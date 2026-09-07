package schemaregistry

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

//go:embed all:*/v*/schema.json all:*/v*/instructions.md
var schemasFS embed.FS

// schemaDoc is one {type}/v{version} directory: the JSON Schema plus its
// human-readable instructions.
type schemaDoc struct {
	schema       json.RawMessage
	instructions string
}

type SchemaEntry struct {
	Type         string          `json:"type"`
	Version      int             `json:"version"`
	JSONSchema   json.RawMessage `json:"schema"`
	Instructions string          `json:"instructions,omitempty"`
}

type SchemaSummary struct {
	Type          string `json:"type"`
	LatestVersion int    `json:"latest_version"`
	Versions      []int  `json:"versions"`
}

type Handler struct {
	schemas map[string]map[int]schemaDoc
	order   []string
}

// NewHandler loads every embedded schema. Layout is {type}/v{version}/, so the
// type and version come from the path rather than being encoded in filenames.
func NewHandler() (*Handler, error) {
	types, err := schemasFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("schemaregistry: read embedded schemas: %w", err)
	}

	schemas := make(map[string]map[int]schemaDoc, len(types))
	for _, typeDir := range types {
		if !typeDir.IsDir() {
			continue
		}
		typeName := typeDir.Name()

		versionDirs, err := schemasFS.ReadDir(typeName)
		if err != nil {
			return nil, fmt.Errorf("schemaregistry: read %s: %w", typeName, err)
		}

		for _, versionDir := range versionDirs {
			if !versionDir.IsDir() {
				continue
			}
			version, err := parseVersionDir(typeName, versionDir.Name())
			if err != nil {
				return nil, err
			}

			dir := typeName + "/" + versionDir.Name()
			schema, err := schemasFS.ReadFile(dir + "/schema.json")
			if err != nil {
				return nil, fmt.Errorf("schemaregistry: read %s/schema.json: %w", dir, err)
			}
			if !json.Valid(schema) {
				return nil, fmt.Errorf("schemaregistry: %s/schema.json is not valid JSON", dir)
			}
			// Instructions are optional; a schema without prose still serves.
			instructions, err := schemasFS.ReadFile(dir + "/instructions.md")
			if err != nil {
				instructions = nil
			}

			versions := schemas[typeName]
			if versions == nil {
				versions = make(map[int]schemaDoc)
				schemas[typeName] = versions
			}
			if _, exists := versions[version]; exists {
				return nil, fmt.Errorf("schemaregistry: duplicate schema %s version %d", typeName, version)
			}
			versions[version] = schemaDoc{
				schema:       json.RawMessage(schema),
				instructions: string(instructions),
			}
		}
	}

	order := make([]string, 0, len(schemas))
	for typeName := range schemas {
		order = append(order, typeName)
	}
	slices.Sort(order)

	return &Handler{schemas: schemas, order: order}, nil
}

// parseVersionDir turns a "v3" directory name into 3.
func parseVersionDir(typeName, name string) (int, error) {
	version, err := strconv.Atoi(strings.TrimPrefix(name, "v"))
	if !strings.HasPrefix(name, "v") || err != nil || version < 1 {
		return 0, fmt.Errorf(
			"schemaregistry: invalid version directory %q in %q: expected v<positive-integer>",
			name, typeName,
		)
	}
	return version, nil
}

type ListOutput struct {
	Body struct {
		Schemas []SchemaSummary `json:"schemas"`
	}
}

func (h *Handler) ListSchemas(_ context.Context, _ *struct{}) (*ListOutput, error) {
	out := &ListOutput{}
	out.Body.Schemas = make([]SchemaSummary, 0, len(h.schemas))
	for _, typeName := range h.order {
		versions := h.versions(typeName)
		out.Body.Schemas = append(out.Body.Schemas, SchemaSummary{
			Type:          typeName,
			LatestVersion: versions[len(versions)-1],
			Versions:      versions,
		})
	}
	return out, nil
}

type GetInput struct {
	Type string `path:"type" doc:"Schema type name"`
}

type GetVersionInput struct {
	Type    string `path:"type" doc:"Schema type name"`
	Version int    `path:"version" minimum:"1" doc:"Immutable schema version"`
}

type GetOutput struct {
	Body SchemaEntry
}

func (h *Handler) GetSchema(_ context.Context, input *GetInput) (*GetOutput, error) {
	versions := h.versions(input.Type)
	if len(versions) == 0 {
		return nil, huma.Error404NotFound("schema type not found: " + input.Type)
	}
	return h.getSchema(input.Type, versions[len(versions)-1])
}

func (h *Handler) GetSchemaVersion(_ context.Context, input *GetVersionInput) (*GetOutput, error) {
	return h.getSchema(input.Type, input.Version)
}

func (h *Handler) getSchema(typeName string, version int) (*GetOutput, error) {
	doc, ok := h.schemas[typeName][version]
	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("schema not found: %s version %d", typeName, version))
	}

	out := &GetOutput{}
	out.Body = SchemaEntry{
		Type:         typeName,
		Version:      version,
		JSONSchema:   doc.schema,
		Instructions: doc.instructions,
	}
	return out, nil
}

func (h *Handler) versions(typeName string) []int {
	versions := make([]int, 0, len(h.schemas[typeName]))
	for version := range h.schemas[typeName] {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions
}
