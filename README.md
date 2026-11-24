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

Running it at code.danieldaum.net, moving Gitea to the tailnet and serving
it at danieldaum.net/projects are in [deploy/README.md](deploy/README.md).
`just image` builds an image for this machine only, to try it locally.

### Release a version

A release is a git tag `v<version>`, an image
`code.aardvark-halibut.ts.net/daniel/showcase:<version>` (also tagged `latest`, in Gitea's
registry on the tailnet) and a Gitea release. Versions follow semver: a fix bumps the patch (0.2.0 →
0.2.1), a feature the minor (0.2.0 → 0.3.0). The examples release 0.2.1.

You commit, tag and deploy. Pushing the tag starts
[`.gitea/workflows/release.yml`](.gitea/workflows/release.yml), which does
the rest.

1. **Check and push the commit.**

   ```sh
   just lint && just test
   git log --oneline v0.2.0..HEAD      # what goes into the release
   git push origin main
   ```

2. **Tag it and push the tag** (signed, since `tag.gpgsign` is on).

   ```sh
   git tag -s v0.2.1 -m v0.2.1
   git push origin v0.2.1              # starts the release workflow
   ```

   Tagged the wrong commit? Before pushing, `git tag -d v0.2.1` and tag
   again. Once pushed, leave it and release 0.2.2.

3. **The workflow** (Gitea → showcase → Actions) builds the image for the
   server (linux/amd64), pushes it as `0.2.1` and `latest`, and creates
   the Gitea release `v0.2.1` with the image digest and the commits since
   `v0.2.0`.

   To run it by hand, for example after a failed run: Actions → release →
   Run workflow, on `main`, with the tag (`v0.2.1`). It builds that tag's
   code with the workflow on `main`, so a fixed workflow works for an
   older tag, and it updates the tag's release with the new digest.

4. **Deploy.** Pin the digest from the release notes in
   `deploy/compose.yaml` (the `image:` line and the version in the comment
   above it) and commit that. Copy `compose.yaml` to the server and, next
   to it and `config.json`:

   ```sh
   docker compose pull && docker compose up -d
   curl -s 127.0.0.1:8080/healthz      # "ok" once a build is live
   ```

**Once, before the first run:** enable Actions for the repo and add a repo
secret `REGISTRY_TOKEN` (Settings → Actions → Secrets): a Gitea access
token with `write:package`. That's the only token to make: `GITEA_TOKEN`,
which writes the release, is made by Gitea for each run. The runner needs
Docker (act_runner gives jobs the host's Docker by default).

**Without CI**, from the Mac, after step 2:

```sh
docker login code.aardvark-halibut.ts.net   # once; a Gitea token with write:package
just release 0.2.1                  # same image and tags; no Gitea release
crane digest code.aardvark-halibut.ts.net/daniel/showcase:0.2.1
```

Use one or the other for a version: a rebuild gets a new digest.

### Container

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
.gitea/workflows/   release: builds and pushes the image on a version tag
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
