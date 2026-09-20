# storefs

## Short description

`internal/infrastructure/storefs` owns one data directory: the names of everything in it, the lock guarding it, and the only way to address a file inside it.

Those three were previously spread out. `dk_journal` was spelled in three packages, `index.go` rebuilt the `2026/2026-09-12_journal.jsonl` string that `jsonl.go` also built, and the lock paths were assembled independently by whoever needed one.
Nothing broke while every copy agreed — but a rename in the write path would have quietly made files invisible to the reader, the index, or a backup, and nothing would have failed to compile.

## Main decisions

- The names are **unexported**. Code outside the package cannot spell a path, which is what turns "everyone uses the same layout" from a convention into a property.
- The lock is part of file access
- File access keeps resolution internally tied to domain enums
- The lock is exposed as a **transaction**: services open one with `WithShared`/`WithExclusive`, stores require one as a parameter — see ARCHITECTURE.md § Services open a transaction, stores require one

## What it provides

| | |
|---|---|
| `Open(path)` → `*Dir` | creates the trees, opens an `os.Root` handle; `Close` releases it |
| `WithShared(ctx, fn)` / `WithExclusive(ctx, fn)` | run `fn` under the directory's shared / exclusive lock, yielding a `Tx` / `WriteTx` (see ARCHITECTURE.md § Locking) |
| `Tx` / `WriteTx` | proof the lock is held, and the capability to read / rewrite. Closed interfaces: only this package can produce one, so a store method taking one cannot be called without the lock |
| `Check(tx)` | the residual runtime check — that `tx` is live and belongs to *this* directory |
| `Files()` | every record file, directory-relative and sorted |
| `JournalPath` / `CandidatePath` / `AuditPath` / `IndexPath` | addressing by partition and state |
| `Resolve(rel)` | widens a relative key to an absolute path, refusing anything that escapes |
| `FS()` | read-only `io/fs` view, for `fs.WalkDir` and `fstest` |
| `JournalKey(p)` | the index row key — a naming contract, not a path |

## Technical decisions

- **`os.Root`, not `os.DirFS`.** `DirFS` follows a symlink out of the tree; a `Root` refuses to. `Files()` walks the rooted FS for the same reason.

- **`Resolve` returns an error.** Relative names are not always ours — `dkbackup restore` reads them from the remote's manifest — so it rejects anything failing `fs.ValidPath`.

## Domain enums drive the names

Which states a candidate can be in is a domain question, so `core.CandidateStates()` owns the set and `storefs` maps each to a directory:

```go
var candidateAreaByState = map[core.CandidateState]string{
    core.CandidatePending: "pending",
    core.CandidateApplied: "applied",
    core.CandidateDenied:  "denied",
}
```

The mapping is spelled out rather than derived from the constant's own string, so renaming a domain constant cannot silently move data on disk. `command/model.CandidateResolution.EndState()` says where a resolved candidate comes to rest — before, "applied candidates land in `applied/`" was true only because someone named two variables consistently inside `MoveCandidates`.

`TestCandidateAreas_CoverEveryDomainState` ties the two: a state added to the domain without a directory fails there, instead of silently writing those candidates nowhere.

## Resulting on-disk layout

```
<data dir>/
├── dk.lock                     reader/writer lock
├── server.lock                 process ownership, held for the server's lifetime
├── index.db                    derived; rebuildable from the trees below
├── dk_journal/<YYYY>/<YYYY-MM-DD>_journal.jsonl
└── dk_candidates/
    ├── {pending,applied,denied}/<YYYY>/<YYYY-MM-DD>_candidates.jsonl
    └── candidateaudit/<YYYY>/<YYYY-MM-DD>_candidateaudit.jsonl
```
