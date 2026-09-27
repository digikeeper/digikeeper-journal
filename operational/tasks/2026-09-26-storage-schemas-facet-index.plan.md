# Plan — storage schemas and facet index

## Goal
Load user-owned versioned schemas at startup, validate writes against them, and index/search common record facets without depending on type-specific payload fields.

## Acceptance criteria
- Startup rejects zero, incomplete, or invalid versioned schema bundles; valid bundles are served through existing registry routes and used by write services.
- Append uses latest schema version; candidate may target another version, with documented defaults; invalid writes never reach JSONL/candidate storage.
- New record contract uses `type`, `ts`, `facets`, `m`, `d`; no `x-alias` or legacy tag/metadata support.
- `facet[key]=value` searches OR within a key and AND across keys, with file-level lookup plus record-level filtering; compaction rebuilds facet metadata.
- Clean-break setup and schema authoring are documented; focused, integration, and `just` checks pass in a suitable environment.

## Plan
1. Establish baseline with `just unit`/`just build`; inspect `docs/ARCHITECTURE.md` and `docs/SCHEMA_REGISTRY_HANDLERS.md`. **Done when** baseline results or environmental blockers are recorded.
2. Test then add schema directory access in `internal/infrastructure/storefs`; create a complete example `schema.json` + `instructions.md` and one embedded schema-for-schemas. **Done when** paths stay under the data root and malformed/missing bundles have failing tests.
3. Test then implement a read-only registry that loads, validates, and compiles all bundles at startup; wire it in `cmd/server/main.go` and `internal/httpapi/schemaregistry/handler.go`. **Done when** startup fails on empty/invalid bundles and existing discovery routes return the loaded versions.
4. Test then change `internal/domain/core/record.go` and command/query HTTP resources and DTOs to the new common names and `facets` shape. **Done when** serialization and request/response tests use only the new contract.
5. Test then inject registry validation into `internal/domain/command/append` and `internal/domain/command/candidate`; select latest or optional target version per task. **Done when** invalid or unknown versions/types cause client errors without durable writes and compaction revision CAS remains intact.
6. Test then replace tags in `internal/infrastructure/index`, `commandstore`, `querystore`, and `internal/domain/query/search.go`; parse repeatable `facet[key]` in HTTP. **Done when** OR/AND semantics, type/time combinations, file false positives, and `RebuildPartition` pass tests.
7. Update `tests/` fixtures to write explicit schema bundles; run focused tests, `just unit`, `just lint`, and `just build`; update setup and architecture docs. **Done when** startup, append, candidate, discovery, query, and compaction integration checks pass and the fresh-data-only rollout is documented.

Commit after each tested slice; avoid unrelated refactors.

## Risks / Unknowns
- `just unit` was blocked here by sandbox Go telemetry/cache permissions; rerun with writable paths.
- Choosing/compiling a JSON Schema validator must preserve the embedded document contract and give actionable errors; verify supported dialect before implementation.
- Existing registry tests assume embedded note/health schemas; fixtures must move to storage before test expectations change.
