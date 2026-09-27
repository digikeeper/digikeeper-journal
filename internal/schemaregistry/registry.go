// Package schemaregistry loads and validates user-owned record schemas.
package schemaregistry

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

//go:embed schema_for_schemas.json
var embedded embed.FS

const metaSchemaURL = "https://digikeeper.local/schema-for-schemas.json"

var (
	ErrUnknownSchema = errors.New("schema is not loaded")
	ErrInvalidRecord = errors.New("record does not satisfy schema")
)

// Registry is immutable after construction and safe to share across handlers and services.
type Registry struct {
	schemas map[string]map[int]schemaDoc
	order   []string
}

type schemaDoc struct {
	schema       json.RawMessage
	instructions string
	compiled     *jsonschema.Schema
}

type pendingSchema struct {
	typeName     string
	version      int
	url          string
	schema       json.RawMessage
	instructions string
	document     map[string]any
}

type Entry struct {
	Type         string
	Version      int
	Schema       json.RawMessage
	Instructions string
}

// Load reads schemas/{type}/v{version}/schema.json and instructions.md from fsys.
func Load(fsys fs.FS) (*Registry, error) {
	metaSchema, err := loadMetaSchema()
	if err != nil {
		return nil, err
	}
	types, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read schemas: %w", err)
	}

	var pending []pendingSchema
	for _, typeDir := range types {
		if !typeDir.IsDir() {
			return nil, fmt.Errorf("schema entry %q must be a type directory", typeDir.Name())
		}
		typeName := typeDir.Name()
		if err := validTypeName(typeName); err != nil {
			return nil, err
		}
		versions, err := fs.ReadDir(fsys, typeName)
		if err != nil {
			return nil, fmt.Errorf("read schema type %q: %w", typeName, err)
		}
		if len(versions) == 0 {
			return nil, fmt.Errorf("schema type %q has no versions", typeName)
		}
		for _, versionDir := range versions {
			if !versionDir.IsDir() {
				return nil, fmt.Errorf("schema type %q entry %q must be a version directory", typeName, versionDir.Name())
			}
			version, err := parseVersionDir(typeName, versionDir.Name())
			if err != nil {
				return nil, err
			}
			base := path.Join(typeName, versionDir.Name())
			raw, err := fs.ReadFile(fsys, path.Join(base, "schema.json"))
			if err != nil {
				return nil, fmt.Errorf("schema %s: %w", path.Join(base, "schema.json"), err)
			}
			instructions, err := fs.ReadFile(fsys, path.Join(base, "instructions.md"))
			if err != nil {
				return nil, fmt.Errorf("instructions %s: %w", path.Join(base, "instructions.md"), err)
			}
			var document map[string]any
			if err := json.Unmarshal(raw, &document); err != nil {
				return nil, fmt.Errorf("schema %s is not valid JSON: %w", base, err)
			}
			if err := metaSchema.Validate(document); err != nil {
				return nil, fmt.Errorf("schema %s does not satisfy schema-for-schemas: %w", base, err)
			}
			pending = append(pending, pendingSchema{
				typeName: typeName, version: version,
				url:    schemaURL(typeName, versionDir.Name()),
				schema: raw, instructions: string(instructions), document: document,
			})
		}
	}
	if len(pending) == 0 {
		return nil, errors.New("no schema bundles found")
	}

	compiler := newCompiler()
	for _, schema := range pending {
		if err := compiler.AddResource(schema.url, schema.document); err != nil {
			return nil, fmt.Errorf("register schema %s: %w", schema.url, err)
		}
	}

	r := &Registry{schemas: make(map[string]map[int]schemaDoc)}
	for _, schema := range pending {
		compiled, err := compiler.Compile(schema.url)
		if err != nil {
			return nil, fmt.Errorf("compile schema %s: %w", schema.url, err)
		}
		if r.schemas[schema.typeName] == nil {
			r.schemas[schema.typeName] = make(map[int]schemaDoc)
			r.order = append(r.order, schema.typeName)
		}
		r.schemas[schema.typeName][schema.version] = schemaDoc{
			schema: schema.schema, instructions: schema.instructions, compiled: compiled,
		}
	}
	slices.Sort(r.order)
	return r, nil
}

func newCompiler() *jsonschema.Compiler {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	return compiler
}

func loadMetaSchema() (*jsonschema.Schema, error) {
	raw, err := embedded.ReadFile("schema_for_schemas.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded schema-for-schemas: %w", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("parse embedded schema-for-schemas: %w", err)
	}
	compiler := newCompiler()
	if err := compiler.AddResource(metaSchemaURL, document); err != nil {
		return nil, fmt.Errorf("register embedded schema-for-schemas: %w", err)
	}
	compiled, err := compiler.Compile(metaSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile embedded schema-for-schemas: %w", err)
	}
	return compiled, nil
}

func schemaURL(typeName, versionDir string) string {
	return "https://digikeeper.local/schemas/" + path.Join(typeName, versionDir, "schema.json")
}

func validTypeName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return fmt.Errorf("invalid schema type directory %q", name)
	}
	return nil
}

func parseVersionDir(typeName, name string) (int, error) {
	version, err := strconv.Atoi(strings.TrimPrefix(name, "v"))
	if !strings.HasPrefix(name, "v") || err != nil || version < 1 {
		return 0, fmt.Errorf("invalid version directory %q in %q: expected v<positive-integer>", name, typeName)
	}
	return version, nil
}

func (r *Registry) Types() []string { return slices.Clone(r.order) }

func (r *Registry) Versions(typeName string) []int {
	versions := make([]int, 0, len(r.schemas[typeName]))
	for version := range r.schemas[typeName] {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions
}

func (r *Registry) LatestVersion(typeName string) (int, error) {
	versions := r.Versions(typeName)
	if len(versions) == 0 {
		return 0, fmt.Errorf("type %q: %w", typeName, ErrUnknownSchema)
	}
	return versions[len(versions)-1], nil
}

func (r *Registry) Entry(typeName string, version int) (Entry, error) {
	doc, ok := r.schemas[typeName][version]
	if !ok {
		return Entry{}, fmt.Errorf("type %q version %d: %w", typeName, version, ErrUnknownSchema)
	}
	return Entry{Type: typeName, Version: version, Schema: doc.schema, Instructions: doc.instructions}, nil
}

// Validate checks a fully server-populated record against the selected stored schema.
func (r *Registry) Validate(typeName string, version int, record core.Record) error {
	doc, ok := r.schemas[typeName][version]
	if !ok {
		return fmt.Errorf("type %q version %d: %w", typeName, version, ErrUnknownSchema)
	}
	if record.Type != typeName {
		return fmt.Errorf("record type %q does not match selected type %q: %w", record.Type, typeName, ErrInvalidRecord)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode record: %w", err)
	}
	if err := doc.compiled.Validate(value); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}
	return nil
}
