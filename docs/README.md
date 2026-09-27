# Docs

Catalog of design decisions for `digikeeper-journal`.

- [Architecture](ARCHITECTURE.md) — layering, CQS split, locking model, record shape, trade-offs.
- [Schema Registry Handlers](SCHEMA_REGISTRY_HANDLERS.md) — how record-type schemas are loaded, versioned, and served over HTTP.
- [Candidate Compaction](CANDIDATE_COMPACTION.md) — the candidate-review-and-compaction RFC for proposed record edits.
- [storefs](STOREFS.md) — why `storefs` owns the on-disk layout and lock.

Not yet merged into this branch: `BACKUP.md` (`cmd/dkbackup` design) exists on the separate `backup` branch and isn't available here yet.
