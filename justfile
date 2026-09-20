bin := "./bin"
cmd := "./cmd"

build:
    go build -o {{ bin }}/server {{ cmd }}/server
    go build -o {{ bin }}/dkbackup {{ cmd }}/dkbackup

# dkbackup reads JOURNAL_STORAGE_PATH and BACKUP_* straight from the environment
backup *args:
    go run {{ cmd }}/dkbackup {{ args }}

run:
    DIGIKEEPER_LOAD_DOTENV=true go run {{ cmd }}/server

lint:
    golangci-lint run ./... && go fix -diff ./...

fmt:
    golangci-lint run --fix ./...

fix-diff:
    go fix -diff ./...

fix:
    go fix ./...

test *args:
    go test {{ args }}

unit *args:
    go test {{ args }} ./...
