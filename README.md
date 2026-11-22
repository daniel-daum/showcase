# daum.dev

A small, read-only public view of selected Gitea repos. One Go binary
rebuilds static HTML from Gitea on a timer, serves it, and passes `git clone`
and `docker pull` through to Gitea. The public side can't write, log in, or
reach Gitea's UI or API.

It talks to Gitea anonymously, so it can only ever see public data. Only the
repos and images listed in `config.json` are rendered or proxied.

## Develop

Needs [mise](https://mise.jdx.dev) and [just](https://just.systems).

```sh
mise install     # Go version pinned in mise.toml
just             # list commands
just test        # run tests
just lint        # gofmt + go vet
just golden      # rewrite golden files after an intended output change
just render      # render the site once into ./out from ./config.json
just demo        # render the test fixtures and serve them on localhost:8000
just run         # build from real Gitea every 10 min and serve on localhost:8000
```

## Configure

`config.json` (mounted read-only at `/config.json` in the container):

```json
{
    "gitea_url": "http://gitea.tailnet.example:3000",
    "user": "daniel",
    "site_url": "https://daum.dev",
    "links": { "website": "https://danieldaum.net" },
    "repos": ["repo-a", "repo-b"],
    "images": ["repo-a"],
    "max_commits": 50,
    "theme": "moss",
    "trusted_proxies": ["100.64.0.10"]
}
```

## Deploy

```sh
just image                       # docker build -t daumdev:latest . (needs Docker)
docker compose up -d             # uses compose.yaml and ./config.json
```

The container runs as uid 10001 with a read-only root filesystem, no
capabilities and `no-new-privileges`. Only `/data` is writable: git mirrors
and site builds. It listens on plain HTTP `:8080`. Pangolin terminates TLS.

Gitea must allow anonymous reads (`REQUIRE_SIGNIN_VIEW = false`, HTTP git
not disabled) and has to be reachable from the container at `gitea_url`.
Put Pangolin's address in `trusted_proxies` so logs show client IPs.

## Layout

```
cmd/daumdev/       entrypoint
internal/config/   config.json loading and validation
internal/gitea/    anonymous Gitea API client
internal/gitx/     git CLI wrappers
internal/build/    build loop and rendering pipeline
internal/render/   templates, markdown, highlighting, heatmap SVG
internal/server/   router, static files, git + registry proxy
web/               embedded templates and static assets
testdata/          sample Gitea JSON and golden output
docs/              spec and design decisions
Dockerfile         golang build stage, alpine + git runtime, pinned by digest
compose.yaml       example deployment
```

## Docs

- [docs/spec.md](docs/spec.md): what this is and isn't, the security rules
  and the build order.
- [docs/decisions.md](docs/decisions.md): choices made while building.
- [docs/theme.md](docs/theme.md): how the look follows danieldaum.net.
- [docs/security.md](docs/security.md): each security rule mapped to code and tests.

## Licences

Styles and palettes come from [saturn](https://tangled.org/danieldaum.net/saturn)
(danieldaum.net, MIT), which builds on Flexoki (MIT,
[stephango.com/flexoki](https://stephango.com/flexoki)). Geist, Geist Mono and
Geist Pixel are under the SIL Open Font License, and file icons come from
[material-icon-theme](https://github.com/material-extensions/vscode-material-icon-theme)
(MIT). The licence files are in `web/static/fonts/` and `web/static/icons/`.
