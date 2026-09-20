# Architecture

## Overview
Part of the Digikeeper project. An HTTP service for append-based personal records.
It accepts records over a REST API, stores them durably in JSONL files (source of truth), and maintains a SQLite index for fast querying.

Mutual exclusion for data changes is based on flock, which prevents concurrency at the OS-directory level.

## Locking
One reader/writer lock per data directory (`dk.lock`), owned by `internal/infrastructure/storefs` alongside the on-disk names.

The journal is personal storage: one user's records per instance, scaled by running more instances. Stopping the world therefore costs one person a pause.

- Appends and reads take it **shared** (`WithShared`) and run concurrently.
- Rewrites — compaction, a backup dump, a restore — take it **exclusive** (`WithExclusive`).
- Nothing nests: code inside a transaction must not open another.
- Compaction acquires the journal partition and then the candidate partition — any future code acquiring them in the other order will deadlock.

It is a file lock because it has to hold across processes: `dkbackup` quiesces a running server with it. `server.lock` is separate and marks process ownership of the directory for its lifetime.

### Services open a transaction, stores require one

The lock is taken by the **domain service**, never by the store it calls. `storefs` exposes it the way `bbolt` does:

```go
err := s.lock.WithExclusive(ctx, func(tx storefs.WriteTx) error {   // exclusive
    applied, err := s.candidates.ListApplied(ctx, tx, p)
    ...
    return s.journal.ReplacePartition(ctx, tx, p, rewritten)
})
```

`WithShared` yields a `storefs.Tx`, `WithExclusive` a `storefs.WriteTx`. Both interfaces have an unexported method, so only `storefs` can produce one: a store method that takes a `Tx` **cannot be called without the lock**, and one taking a `WriteTx` cannot be handed what `WithShared` yielded. The requirement is in the signature, and getting it wrong does not compile.

It is a transaction because only the service knows where the span begins.

Why a closure rather than a returned guard: the lock is released on every exit, including an error return or a panic, and a transaction cannot outlive its span — one captured out of the closure is refused by `Dir.Check`.

Two things remain checked at runtime, because the types cannot carry them:

- **A transaction from another data directory** (`ErrForeignTx`). Stores call `dir.Check(tx)` for this; it is the only assertion left.
- **Nesting.** Do not call `WithShared` or `WithExclusive` from inside one. The lock is a file lock re-opened per acquisition, so it does not recurse — the inner call waits on the outer until the context expires.

#### Why not a context value

Keeping the guard in `ctx` would have stores assert it. That is the case the `context` documentation names explicitly — "not for passing optional parameters to functions" — and worse, the guard is *required*, not optional. It is not a common solution among storage layers, so users can be misled, and passing the wrong `ctx` (always in scope) could silently defeat it. `ctx` now carries only cancellation.

#### Why not optimistic concurrency

Optimistic control needs the storage layer to offer a conditional write — S3's `If-Match`, GCS's `ifGenerationMatch`. A local filesystem has no mediator and POSIX has no conditional rename, so there is no way to say "swap this partition only if nobody appended since I read it". The only atomic CAS a filesystem offers is create-exclusive. Checked solutions: Delta Lake, Iceberg, S3.

The record layer *is* optimistic, and stays so: `applyCandidate` compare-and-swaps on `Meta.Revision`, which makes compaction idempotent.


## CQS
The service logic is divided into commands and queries.
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
| `storefs`      | The data directory: on-disk names, its lock, an io/fs view, and the only way to address a file in it |

Domain enums drive storage names:
- `core.CandidateStates()` lists the states a candidate can be in, and `storefs` maps each to a directory.
- `command/model.CandidateResolution.EndState()` says where a resolved candidate comes to rest. A state added to the domain without a directory fails a test instead of silently writing candidates nowhere. The mapping is spelled out rather than derived from the constant's string, so renaming a domain constant cannot silently move data on disk.

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
- [storefs](STOREFS.md) — why one package owns the layout and the lock
- [Backup Strategy](BACKUP.md) — `cmd/dkbackup`, and what is deliberately not dumped

## Trade-offs

| Decision                | Revisit when |
|-------------------------|-------------|
| Synchronous write+index | Write latency matters |
| expvar metrics          | Need Prometheus/OTel |
| No auth                 | Exposed to untrusted network |
| File-level index        | Need sub-file granularity |
| One lock per data dir   | One instance must serve many users |
| Names unexported in `storefs` | Never — a caller that can spell a path can drift from it |
| Pessimistic locking over CAS | Immutable partitions + a log (Delta/Iceberg shape), if writes ever go multi-writer |
| `storefs.Tx` in the domain ports | Never — the alternative is a runtime check, and the handle is what makes the lock unforgettable |
