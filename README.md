# showcase

A small, read-only public view of selected Gitea repos, served at
[code.danieldaum.net](https://code.danieldaum.net) and styled as part of
danieldaum.net. One Go binary
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
    "gitea_url": "https://git.danieldaum.net",
    "user": "daniel",
    "site_url": "https://code.danieldaum.net",
    "base_path": "",
    "repos": ["repo-a", "repo-b"],
    "images": ["repo-a"],
    "featured": ["repo-a"],
    "max_commits": 50,
    "theme": "moss",
    "trusted_proxies": ["100.64.0.10"]
}
```

`featured` (optional) picks up to six repos for the profile, in order.
Without it, the six most recently updated are shown. Every repo is listed
at `/repos/`. `base_path` (optional, like `/projects`) serves the pages
under one path, for danieldaum.net/projects; clone, `go get` and
`docker pull` stay at the root of `site_url`.

## Deploy

See [deploy/README.md](deploy/README.md): releasing an image, running it at
code.danieldaum.net, moving Gitea to the tailnet, and serving it at
danieldaum.net/projects.

```sh
just image                       # build for this machine (needs Docker)
just release 0.1.0               # push git.danieldaum.net/daniel/showcase:0.1.0
```

The container runs as uid 10001 with a read-only root filesystem, no
capabilities and `no-new-privileges`. Only `/data` is writable: git mirrors
and site builds. It listens on plain HTTP `:8080` behind a reverse proxy
that terminates TLS.

## Layout

```
cmd/showcase/      entrypoint
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
deploy/            compose, production config, saturn patch and runbook
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
[stephango.com/flexoki](https://stephango.com/flexoki)). Geist Mono and Geist
Pixel are under the SIL Open Font License. Icons are the ones Gitea
uses: file icons from
[material-icon-theme](https://github.com/material-extensions/vscode-material-icon-theme)
(MIT) and UI icons from [Octicons](https://github.com/primer/octicons) (MIT,
GitHub). The licence files are in `web/static/fonts/`,
`web/static/icons/material/` and `web/static/icons/octicons/`.
