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

# Lint the Dockerfile
lint-docker:
    hadolint Dockerfile

# Print the current digests of the pinned base images, to compare with the Dockerfile
digests:
    @echo "golang:1.27.4-alpine3.24 $(crane digest golang:1.27.4-alpine3.24)"
    @echo "alpine:3.24.4            $(crane digest alpine:3.24.4)"

# Build the image (needs Docker, e.g. on the host)
image:
    docker build -t daumdev:latest .

# Build the binary into ./bin
build:
    CGO_ENABLED=0 go build -trimpath -o bin/daumdev ./cmd/daumdev

# Render the site once into ./out (needs a config.json and access to Gitea)
render config="config.json":
    rm -rf out
    go run ./cmd/daumdev -config {{config}} -data .data -once -out out

# Serve the finished build in ./out on localhost:8000 with the real server
serve config="testdata/demo.json":
    go run ./cmd/daumdev -config {{config}} -serve-dir out -listen 127.0.0.1:8000

# Build from real Gitea every 10 minutes and serve on localhost:8000 (needs ./config.json)
run config="config.json":
    go run ./cmd/daumdev -config {{config}} -data .data -listen 127.0.0.1:8000

# Render from real Gitea (needs ./config.json) and serve it
preview config="config.json": (render config)
    just serve {{config}}

# Render the test fixtures (no Gitea needed) and serve them
demo:
    go test ./internal/build -run Golden -count=1 -keep "$PWD/out"
    just serve

# Count Go lines (budget: ~1,800, excluding tests, templates and CSS)
loc:
    find cmd internal web -name '*.go' ! -name '*_test.go' -print0 | xargs -0 cat | wc -l
