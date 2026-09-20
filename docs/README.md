# Docs

## Short description

Design decisions for `digikeeper-journal`, one document per concern.

These mostly record ADRs and describe how decisions were made.

Start with [ARCHITECTURE.md](ARCHITECTURE.md); the rest go deeper on one area each.

## Catalog

| Document | Answers |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | How the service is shaped: CQS split, layering, locking, observability, API conventions, and the standing trade-offs |
| [STOREFS.md](STOREFS.md) | Why one package owns the data directory's names, lock, and addressing — and what drifts if it does not |
| [BACKUP.md](BACKUP.md) | How `dkbackup` dumps, restores, and rebuilds; what is deliberately not dumped, and why push stays incremental |
| [CANDIDATE_COMPACTION.md](CANDIDATE_COMPACTION.md) | How a proposed record becomes a canonical one: candidate states, resolution, compaction, invariants, failure rules |
| [SCHEMA_REGISTRY_HANDLERS.md](SCHEMA_REGISTRY_HANDLERS.md) | How record schemas are published and versioned, and why the registry is stateless |


## Conventions

Prefer recording the reason over the mechanism: a renamed function invalidates a description, but rarely invalidates the reason it exists.
When a document and the code disagree, usually the code wins and the document is the bug.
