# digikeeper-journal

`digikeeper-journal` is the storage layer for the Digikeeper personal records app, responsible for persisting and indexing journal records.

Tech stack: HTTP service + JSONL storage + SQLite index.

## API
[https://huma.rocks/features/openapi-generation/](https://huma.rocks/features/openapi-generation/)

## Contributors guide

Requires Go 1.27+,
[just](https://github.com/casey/just).
Copy `.env.example` → `.env`.

```bash
just build   # → ./bin/server
just run     # go run; loads local .env
just lint    # golangci-lint
just fmt     # golangci-lint --fix
```

See [docs/README.md](docs/README.md) for the full catalog of design decisions.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the main architectural decisions.
