# Schema Registry Handlers

## Description

Schema registry handlers expose supported record schemas over HTTP.
They live in `internal/httpapi/schemaregistry` and are wired from `cmd/server`.

Endpoints:

- `GET /v1/registry` returns schema type summaries with their latest and available versions.
- `GET /v1/registry/{type}/schema` returns the latest schema for a record type.
- `GET /v1/registry/{type}/instruction` returns the latest type-instraction for a record type.
- `GET /v1/registry/{type}/{version}/schema` && `GET /v1/registry/{type}/{version}/instruction` returns schema/instruiction by version

Schemas are JSON files in `internal/httpapi/schemaregistry/{type}`. The handler embeds
and loads them at startup, retaining each schema as `json.RawMessage`.

Schema and Instruction files use this required path format: `{type}/v{version}/<schema.json|instructions.md>` 

```text
note/v1/schema.json  → type: note, version: 1, file_type: json_schema
note/v1/instructions.md  → type: note, version: 1, file_type: text, instructions and rules
note/v2/schema.json  → type: note, version: 2, file_type: json_schema
note/v2/instructions.md  → type: note, version: 2, file_type: text, instructions and rules
```


## Versioning Contract

A schema identity is `(type, version)`. Published schema files are immutable == add a new
file for a changed schema instead of modifying an existing version. A published `(type, version)` includes both files: json schema and instructions, and both are immutable once published.

A record persists its schema version in `m.sv`.
This identifies the exact registry schema needed to interpret that record; it never means "latest".

Record metadata also contains `m.r`, its logical revision, it does not correlate directly with schema version.

## Why It Exists

Clients need a stable way to discover supported record types and the schema version used by persisted records.
Serving schemas from the running service keeps clients aligned with the deployed version instead of relying only on external documentation.

## Boundaries

The schema registry is read-only application metadata, not user data. Schema changes go
through code review and deployment.

The handler stays in `internal/httpapi` because it has no business workflow or mutable
storage. If schemas become editable or user-specific, this package should be revised.

## Instructions

`instructions.md` describes how agents and clients should create, clarify, and refine records of this type.
It may contain semantic guidance, examples, questions to ask, and Record Compaction rules.

Instructions do not define whether a persisted record is structurally valid; `schema.json` does.
