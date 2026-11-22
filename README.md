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
    "theme": "moss"
}
```

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
```

## Docs

- [docs/spec.md](docs/spec.md): what this is and isn't, the security rules
  and the build order.
- [docs/decisions.md](docs/decisions.md): choices made while building.
- [docs/theme.md](docs/theme.md): how the look follows danieldaum.net.

## Licences

Styles and palettes come from [saturn](https://tangled.org/danieldaum.net/saturn)
(danieldaum.net, MIT), which builds on Flexoki (MIT,
[stephango.com/flexoki](https://stephango.com/flexoki)). Geist Pixel and Geist
Mono are under the SIL Open Font License; the licence files are in
`web/static/fonts/`.
