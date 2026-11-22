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
    go run ./cmd/daumdev -config {{config}} -data .data -once -out out

# Serve ./out on localhost:8000 (stand-in until the real server exists; maps /blob/<path> to <path>.html)
serve:
    #!/usr/bin/env python3
    import http.server, os
    class H(http.server.SimpleHTTPRequestHandler):
        def translate_path(self, path):
            p = super().translate_path(path)
            return p + ".html" if "/blob/" in path and os.path.isfile(p + ".html") else p
    os.chdir("out")
    http.server.ThreadingHTTPServer(("127.0.0.1", 8000), H).serve_forever()

# Render from real Gitea (needs ./config.json) and serve it
preview config="config.json": (render config) serve

# Render the test fixtures (no Gitea needed) and serve them
demo:
    go test ./internal/build -run Golden -count=1 -keep "$PWD/out"
    just serve

# Count Go lines (budget: ~1,800, excluding tests, templates and CSS)
loc:
    find . -name '*.go' ! -name '*_test.go' -print0 | xargs -0 cat | wc -l
