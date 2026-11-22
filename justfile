set shell := ["bash", "-euo", "pipefail", "-c"]

default:
    @just --list

# Run all tests
test:
    go test ./...

# Rewrite golden files after an intended output change
golden:
    go test ./internal/build -run Golden -update

# gofmt check + go vet
lint:
    test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
    go vet ./...

# Build the binary into ./bin
build:
    CGO_ENABLED=0 go build -trimpath -o bin/daumdev ./cmd/daumdev

# Render the site once into ./out (needs a config.json and access to Gitea)
render config="config.json":
    rm -rf out
    go run ./cmd/daumdev -config {{config}} -once -out out

# Count Go lines (budget: ~1,800, excluding tests, templates and CSS)
loc:
    find . -name '*.go' ! -name '*_test.go' -print0 | xargs -0 cat | wc -l
