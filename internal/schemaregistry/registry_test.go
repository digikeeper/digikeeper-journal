package schemaregistry

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
)

func TestLoadCompilesDraft2020SchemaAndValidatesRecord(t *testing.T) {
	t.Parallel()

	registry, err := Load(testSchemaFS(t, draft2020Schema))
	require.NoError(t, err)

	err = registry.Validate("note", 1, validRecord(map[string]any{
		"note":     "journal",
		"kind":     "note",
		"occurred": "2026-03-08T10:00:00Z",
	}))
	require.NoError(t, err)

	err = registry.Validate("note", 1, validRecord(map[string]any{
		"note":     "Not-lowercase",
		"kind":     "other",
		"occurred": "not-a-timestamp",
	}))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidRecord))
}

func TestLoadRejectsSchemaThatCannotCompile(t *testing.T) {
	t.Parallel()

	_, err := Load(testSchemaFS(t, invalidPatternSchema))
	require.Error(t, err)
	require.ErrorContains(t, err, "compile schema")
}

func TestLoadRejectsNonDraft2020Schema(t *testing.T) {
	t.Parallel()

	schema := strings.Replace(draft2020Schema, "draft/2020-12/schema", "draft-07/schema", 1)
	_, err := Load(testSchemaFS(t, schema))
	require.Error(t, err)
	require.ErrorContains(t, err, "does not satisfy schema-for-schemas")
}

func testSchemaFS(t *testing.T, schema string) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"note/v1/schema.json":     &fstest.MapFile{Data: []byte(schema)},
		"note/v1/instructions.md": &fstest.MapFile{Data: []byte("# Note\n")},
	}
}

func validRecord(data map[string]any) core.Record {
	return core.Record{
		ID: "record-1", RequestID: "request-1", CreatedAt: time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
		Type: "note", Timestamp: time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
		Facets: map[string][]string{"topic": {"work"}}, Data: data,
		Meta: core.RecordMeta{SchemaVersion: 1, Revision: 1, Source: 1},
	}
}

const draft2020Schema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$defs": {"lowercase": {"type": "string", "pattern": "^[a-z]+$"}},
  "type": "object",
  "additionalProperties": true,
  "required": ["type", "ts", "facets", "d"],
  "properties": {
    "type": {"const": "note"},
    "ts": {"type": "string", "format": "date-time"},
    "facets": {"type": "object", "additionalProperties": {"type": "array", "items": {"type": "string"}}},
    "d": {"type": "object", "additionalProperties": false, "required": ["note", "kind", "occurred"], "properties": {"note": {"$ref": "#/$defs/lowercase"}, "kind": {"const": "note"}, "occurred": {"type": "string", "format": "date-time"}}}
  }
}`

const invalidPatternSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": true,
  "required": ["type", "ts", "facets", "d"],
  "properties": {
    "type": {"type": "string", "const": "note"}, "ts": {"type": "string", "format": "date-time"},
    "facets": {"type": "object", "additionalProperties": {"type": "array", "items": {"type": "string"}}},
    "d": {"type": "object", "properties": {"note": {"type": "string", "pattern": "["}}}
  }
}`
