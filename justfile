bin := "./bin"
cmd := "./cmd"

build:
    go build -o {{ bin }}/server {{ cmd }}/server

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
