# Schema Registry Handlers

## Description

Schema registry handlers expose supported record schemas over HTTP.
They live in `internal/httpapi/schemaregistry` and are wired from `cmd/server`; the
loading and validation logic lives in `internal/schemaregistry`.

Endpoints:

- `GET /v1/registry` returns schema type summaries with their latest and available versions.
- `GET /v1/registry/{type}` returns the latest schema and instructions for a record type.
- `GET /v1/registry/{type}/{version}` returns the schema and instructions for a specific version.

Schemas are JSONSchema files under the data directory's `schemas/{type}/v{version}/` (see
`storefs.SchemaDir`).
`internal/schemaregistry.Load` reads them at startup into a `Registry`. On first run, `SeedDefaultNote` copies an embedded default
`note/v1` into a newly created schema directory so a new instance has a usable
type; it never overwrites a user-owned file.

Schema and instruction files use this required path format: `{type}/v{version}/<schema.json|instructions.md>`

```text
note/v1/schema.json  → type: note, version: 1, file_type: json_schema
note/v1/instructions.md  → type: note, version: 1, file_type: text, instructions and rules
note/v2/schema.json  → type: note, version: 2, file_type: json_schema
note/v2/instructions.md  → type: note, version: 2, file_type: text, instructions and rules
```


## Versioning Contract

A schema identity is `(type, version)`. Published schema files are immutable: add a new
file for a changed schema instead of modifying an existing version.
A published `(type, version)` includes both files: json schema and instructions, and both are immutable once published.

A record persists its schema version in `m.schm_ver`.
This identifies the exact registry schema needed to interpret that record; it never means "latest".

Record metadata also contains `m.rev`, its logical revision. It does not correlate directly with the schema version.

## Why It Exists

Clients need a stable way to discover supported record types and the schema version used by persisted records.
Serving schemas from the running service keeps clients aligned with the deployed version instead of relying only on external documentation.

## Boundaries

The schema registry is user-owned, per-instance metadata: each data directory has its own
`schemas/` tree, edited directly on disk rather than through the API. The HTTP handlers
are read-only — they discover and serve whatever `internal/schemaregistry.Load` found at
startup — but the underlying files are not fixed at deploy time the way the code is.

The HTTP handlers stays a thin `internal/httpapi` wrapper because it has no business
workflow: loading, validation, and default live in `internal/schemaregistry`.

## Instructions

`instructions.md` describes how agents and clients should create, clarify, and refine records of this type.
It may contain semantic guidance, examples, questions to ask, and Record Compaction rules.

Instructions do not define whether a persisted record is structurally valid; `schema.json` does.
