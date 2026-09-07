# Architecture

## Overview
Part of digikeeper project. An HTTP service for append-based personal records.
It accepts records over a REST API, stores them durably in JSONL files (source of truth), and maintains a SQLite index for fast querying.

The mutex and locks of data changes is based on flock -- prevents concurrency on OS-dir level.

## CQS
Logic of service organically divided into command and query.
- **Command** (`internal/domain/command`): state-changing operations, such as JSONL append + SQLite index update.
- **Query** (`internal/domain/query`): retrieval operations that may reorganize data for response, such as SQLite selecting matching JSONL files → JSONL scan + record-level filtering.

Currently synchronous. The command storage boundary is compatible with future async indexing.

CQS is not a packaging rule; it is a semantic discipline for separating asking from changing.

The real boundary is not GET versus POST, but read view versus write decision.
A query layer should represent what a caller needs to observe; a command layer should own the consistency rules needed to safely change state.

When a "get" participates in command consistency — locks, versions, transactions, invariant checks, idempotency, or state-transition decisions — it is part of the write model, even if it reads.
Separate query/read models only when they change for different reasons than the command model; otherwise the split becomes ceremonial duplication and increases coupling instead of reducing it.

## Layering
```
slog-http middleware              ← RequestID, access logs
internal/httpapi/*                ← parse, validate, respond
internal/domain/{command,query}   ← business logic, owns interfaces
internal/infrastructure/*         ← storage, indexing, and external adapters
```
Dependencies flow inward. Interfaces defined at the usage-model-level.

## Handler Segregation

Handlers under `internal/httpapi/` mirror the CQS split so that read and write paths evolve independently — different validation rules, status codes, and future middleware (e.g. rate limiting writes only).
`httpapi/command` owns mutation concerns; `httpapi/query` owns read concerns; `httpapi/schemaregistry` is stateless schema discovery with no domain coupling.
Shared utilities (`response.go`, `errors.go`, `middleware.go`) are kept at the `httpapi/` root to avoid duplication without blurring the command/query boundary.

## Infrastructure Split

`internal/infrastructure/` contains focused packages rather than one facade:
Each package maps one technical capability to the domain interfaces that use it.

| Package        | Role |
|----------------|------|
| `commandstore` | Write path: JSONL append + index update |
| `querystore`   | Read path: matching file lookup → JSONL scan |
| `index`        | Finds JSONL files that may contain matching records |
| `jsonlstore`   | Raw JSONL file I/O |
| `sourcerepo`   | Source-ID ↔ name resolution |

## Observability
- RequestID in every JSONL record + `X-Request-ID` header
- `slog.JSONHandler` structured logging
- `expvar`: `records_appended`, `sqlite_index_latency_ms` at `/debug/vars`

## API Convention
The API follows the main guidelines of [JSON:API](https://jsonapi.org/) specification.
Before making API design decisions, consult the spec and its addendums first.

## See Also
- [Schema Registry Handlers](SCHEMA_REGISTRY_HANDLERS.md)
- [Candidate Compaction](CANDIDATE_COMPACTION.md)

## Trade-offs

| Decision                | Revisit when |
|-------------------------|-------------|
| Synchronous write+index | Write latency matters |
| expvar metrics          | Need Prometheus/OTel |
| No auth                 | Exposed to untrusted network |
| File-level index        | Need sub-file granularity |
