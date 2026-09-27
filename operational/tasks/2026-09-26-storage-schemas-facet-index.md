### Goal with success criteria:
- Let each user supply versioned record schemas under their journal data directory; on startup load and validate every schema bundle into one shared in-memory registry. Fail startup with zero bundles or an invalid/incomplete bundle.
- Validate appends and candidate replacements against the selected loaded schema before any durable write. Schema discovery serves the same loaded versions and instructions.
- Replace tags with named `facets` throughout new records, HTTP, file-level indexing, query, and compaction rebuild. Search matches OR across values of one key, AND across keys, and still rechecks individual records.
- New records use one common root: `type`, `ts`, `facets`, `m`, `d`. Type-specific fields live only under `d`; use `m.schema_version`, `m.source`, and `m.r`, with no `x-alias` mapping.

### Scope and context:
- Current embedded registry lives in `internal/httpapi/schemaregistry/handler.go`; `docs/SCHEMA_REGISTRY_HANDLERS.md` documents `(type, version)` and instructions. Keep existing `/v1/registry` discovery routes from `cmd/server/http.go`, but load user bundles at `schemas/{type}/v{version}/schema.json` plus **required** `instructions.md` from the configured storage directory. Have `storefs` own the storage directory name/access, consistent with `docs/ARCHITECTURE.md`.
- Embed only one schema-for-schemas to validate stored `schema.json` documents and their common-field contract. Compile each accepted record schema for use by both discovery and write services. Reject invalid JSON, invalid JSON Schema, missing shared fields, top-level type-specific fields, invalid version paths, incomplete bundles, and unknown record types/versions; return actionable startup or client errors. Record schemas constrain type-specific `d` and any allowed facets; server fills metadata. Include a copyable example bundle.
- Append flow: `internal/httpapi/command/handler.go` → `internal/domain/command/append` → `internal/infrastructure/commandstore/store.go`. New append chooses the latest loaded version for its type. Candidate flow: `internal/httpapi/command/candidate_handler.go` → `internal/domain/command/candidate/submit.go`; permit optional target version, default to original version for unchanged type and latest for changed type. Validate replacement before storing pending candidate; preserve revision CAS during compaction (`internal/domain/command/compaction/compact.go`).
- Query flow: `internal/httpapi/query/handler.go` → `internal/domain/query/search.go` → `internal/infrastructure/querystore/store.go` → `internal/infrastructure/index/index.go`. Accept repeatable `facet[key]=value` filters (nonblank key/value); index only common searchable fields (`type`, `ts`, `facets`) at file level, then recheck per record. Replace tag aggregation on append and `RebuildPartition` after compaction; retain type/time filters.
- Test-first slices: registry loader/invalid bundles/discovery; append version selection and rejection without writes; candidate version selection/rejection and CAS; facets index insert/search/rebuild and record-level false-positive filtering; HTTP fixtures. Commit each working slice. Update `docs/SCHEMA_REGISTRY_HANDLERS.md`, `docs/ARCHITECTURE.md`, and user-facing setup instructions.

### Constraints and do not do:
- Clean break: require a fresh data directory and derived SQLite index. Do not migrate or support old JSONL (`tags`, `m.s`, `m.sv`, `m.v`) or old index rows; make this explicit in rollout docs.
- No schema-write API, hot reload, auto-installed defaults, SQLite schema source, or persistent schema-version hash ledger. Document that published `(type, version)` schemas **and** instructions must not be edited in place.
- Preserve existing journal source-of-truth, file-level candidate index behavior, lock/transaction boundaries, compaction idempotency, and existing registry routes. Do not index arbitrary `d` fields or couple domain services to HTTP handlers. DRY; YAGNI.

### Result commands and checks:
- Use `just unit` and `just lint`; run `just build` after focused tests. Add/refresh integration tests under `tests/` with explicit on-disk schema fixtures, including empty/invalid startup, version selection, facet queries, and index rebuild after compaction. Inspect `justfile` before using recipes.
- The earlier `just unit` attempt in this sandbox was blocked by Go telemetry/build-cache permissions; rerun in an environment with writable Go cache and temp paths and report results rather than claiming tests passed.
- Verify a fresh configured directory containing a valid example schema starts, publishes that schema, accepts matching records, rejects malformed ones without durable writes, and searches facets with the specified OR/AND rules. Verify a directory with no schemas fails startup.

### Links:
- Plan: `operational/tasks/2026-09-26-storage-schemas-facet-index.plan.md`
- Architecture notes: `operational/tasks/2026-09-26-storage-schemas-facet-index.arch_notes.md`
