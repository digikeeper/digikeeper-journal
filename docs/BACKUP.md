# Backup Strategy

## Short description

`cmd/dkbackup` dumps the journal to a remote, and restores it (with index preps).
The backup is incremental,so it stays cheap to keep running over years.

It is a crucial part of this repository: providers disappear, prices change, and data deserves more than one copy.
This system is long-lived and runs with no SRE involved.


## Backup API

```bash
dkbackup push      # upload everything changed since the remote's last dump
dkbackup restore   # materialize the remote's last dump into the data directory
dkbackup verify    # parse the local JSONL tree; -repair drops a torn trailing line
dkbackup reindex   # rebuild index.db from the JSONL files (server stopped)
```

## What is dumped, and what is not

The JSONL trees are the source of truth, so they are dumped.

But:
- `index.db` is **not**, for two reasons:
  - It is derived: `reindex` rebuilds it from the files, so dumping it stores nothing new.
  - It is a single binary file with no delta, and a live WAL-mode database cannot be captured by copying the file anyway.
- Lock files and `*.compact.tmp` temporaries are skipped as well.

## Incremental against the remote

The backup is incremental. Technically, this means the remote holds a `manifest.json` describing the last dump. `push` reads it, compares each local file by size and modification time, and uploads only what differs — the same cheap test that rsync makes.
Keeping the manifest on the remote rather than in local state means incremental pushes survive a lost machine: a fresh install pushes correctly against an existing dump.

On the remote, file contents sit under their own prefix, away from control files:

```
<remote>/manifest.json
<remote>/data/dk_journal/2026/2026-09-12_journal.jsonl
```

Each manifest records the prefix it used, so old dumps stay restorable if that prefix ever changes.

## Consistency

`push` takes the data directory's **exclusive** lock once, reads the changed set, releases it, and only then uploads. Two consequences:

- The dump is internally consistent. It cannot straddle a compaction, capturing a journal partition from before it alongside candidates from after.
- Writes pause for the local read, not for the network transfer.

## Restore

Restore checks each file against the hash in the manifest and installs it atomically. Files already present with that hash are skipped, so an interrupted restore resumes cheaply. The names in the manifest come from the remote, so they are validated before use — see STOREFS.md.

The full recovery path:

```bash
dkbackup restore -data <dir> -remote <remote>
dkbackup verify  -data <dir> -repair     # drop a torn trailing line, if any
dkbackup reindex -data <dir>             # rebuild index.db
```

A crash mid-append leaves a partial trailing line. Because the format is line-oriented and the record was never acknowledged, truncating it is safe — `verify -repair` does exactly that and nothing else. Any other malformed line is reported, never rewritten.

## Not covered

The remote mirrors the latest state; it keeps no history of earlier ones. There is no retention or pruning, and no encryption at rest beyond whatever the remote provides. If you need snapshots, push into a versioned remote, or back the data directory up with `restic` instead.
