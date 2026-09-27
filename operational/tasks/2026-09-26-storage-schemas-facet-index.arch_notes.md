# Architecture notes — storage schemas and facet index

## Architectural findings
- `cmd/server/main.go` builds storage, index, services, then `schemaregistry.NewHandler()`; the handler currently embeds note/health schemas and checks JSON syntax only (`internal/httpapi/schemaregistry/handler.go`).
- `storefs.Dir` owns data-directory names, a rooted read-only FS, and lock/transaction boundaries (`internal/infrastructure/storefs/storefs.go`, `names.go`); give schema files a storefs-owned tree.
- Append hardcodes schema version 1 (`internal/domain/command/append/append.go`); candidate submission copies the original record then changes type/tags/data (`internal/domain/command/candidate/submit.go`). Neither consults the registry.
- `commandstore.Append` updates SQLite after durable JSONL append. `index.Store` aggregates tags/types/time by file; query searches files then filters actual records. Compaction rewrites JSONL then calls `RebuildPartition` (`internal/domain/command/compaction/compact.go`).
- `docs/SCHEMA_REGISTRY_HANDLERS.md` says published bundles include immutable instructions, though the current loader tolerates missing instructions. The new contract requires both.

## Decisions / ADR candidates
- **Shared read-only registry vs HTTP-owned registry:** startup-load and inject a validated registry into write services and read handlers; avoids domain-to-HTTP coupling, at the cost of a new shared interface.
- **Schema source:** user-managed files + restart vs write API/live reload; simpler consistency and startup errors, no runtime editing.
- **Common index contract:** file-level `type`/`ts`/named `facets` plus record recheck vs indexing `d`; shared keys remain independent of user-defined payload schemas.
- **Clean break:** no legacy JSONL/SQLite compatibility; simpler serializer/index, but requires new data directory and explicit rollout warning.
- **Version immutability:** document it rather than persist hashes; cannot detect edits across restarts.

## Hot spots
- Schema document validation versus compiled record validation; ensure required shared root fields and restrict type-specific fields to `d`.
- Huma HTTP query parsing for dynamic bracketed `facet[key]` parameters; test repeat values, empty keys/values, and error responses.
- Candidate type/version changes must validate replacement without disturbing partition lock ordering or compaction revision CAS.
- File-level facet aggregation must never exclude a true match; AND across keys may yield false positives, removed by record filtering.
- Index migrations are deliberately omitted: old `file_index.tags` rows and old JSONL must not be reused.
