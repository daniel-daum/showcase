set shell := ["bash", "-euo", "pipefail", "-c"]

default:
    @just --list

# Run all tests
test:
    go test ./cmd/... ./internal/... ./web/...

# Rewrite golden files after an intended output change
golden:
    go test ./internal/build -run Golden -update

# gofmt check + go vet
lint:
    test -z "$(gofmt -l cmd internal web)" || { gofmt -l cmd internal web; exit 1; }
    go vet ./cmd/... ./internal/... ./web/...

# Lint the Dockerfile
lint-docker:
    hadolint Dockerfile

# Print the current digests of the pinned base images, to compare with the Dockerfile
digests:
    @echo "golang:1.27.4-alpine3.24 $(crane digest golang:1.27.4-alpine3.24)"
    @echo "alpine:3.24.4            $(crane digest alpine:3.24.4)"

image_name := "git.danieldaum.net/daniel/showcase"

# Build the image for this machine (needs Docker, e.g. on the host)
image tag="dev":
    docker build -t {{image_name}}:{{tag}} .

# Push a tagged release by hand, as CI does (linux/amd64, for the server): needs Docker, `docker login git.danieldaum.net`, a clean tree and HEAD tagged v<version>
release version:
    git diff --quiet HEAD || { echo "commit first"; exit 1; }
    test "$(git describe --exact-match --tags HEAD 2>/dev/null)" = "v{{version}}" || { echo "tag HEAD as v{{version}} first"; exit 1; }
    docker buildx build --platform linux/amd64 -t {{image_name}}:{{version}} -t {{image_name}}:latest --push .

# Regenerate the icons from extracted npm packages: material-icon-theme@5.40.0 and @primer/octicons@19.15.0
icons material octicons:
    python3 -I scripts/icons.py {{material}} {{octicons}}
    gofmt -w internal/render/icons_gen.go

# Build the binary into ./bin
build:
    CGO_ENABLED=0 go build -trimpath -o bin/showcase ./cmd/showcase

# Render the site once into ./out (needs a config.json and access to Gitea)
render config="config.json":
    rm -rf out
    go run ./cmd/showcase -config {{config}} -data .data -once -out out

# Serve the finished build in ./out on localhost:8000 with the real server
serve config="testdata/demo.json":
    go run ./cmd/showcase -config {{config}} -serve-dir out -listen 127.0.0.1:8000

# Build from real Gitea every 10 minutes and serve on localhost:8000 (needs ./config.json)
run config="config.json":
    go run ./cmd/showcase -config {{config}} -data .data -listen 127.0.0.1:8000

# Render from real Gitea (needs ./config.json) and serve it
preview config="config.json": (render config)
    just serve {{config}}

# Render the test fixtures (no Gitea needed) and serve them
demo:
    go test ./internal/build -run Golden -count=1 -keep "$PWD/out"
    just serve

# Count Go lines (budget: ~1,800, excluding tests, templates and CSS)
loc:
    find cmd internal web -name '*.go' ! -name '*_test.go' ! -name '*_gen.go' -print0 | xargs -0 cat | wc -l
