# daum.dev — read-only code site

A minimal, public, read-only view of Daniel's public Gitea repos, served at
the root of `daum.dev`. One Go binary in one Docker image: it rebuilds static
HTML from Gitea on a timer, serves it, and passes `git clone` and
`docker pull` through to Gitea. Nothing on the public side can write, log in,
or reach Gitea's UI or API.

## Goals

- GitHub-profile-style home page: avatar, name, bio, links, activity heatmap, chosen repos.
- Per-repo pages: README, file tree, file viewer with syntax highlighting,
  commit log, commit diffs, releases, pull requests (read-only), packages.
- Public clone: `git clone https://daum.dev/<repo>.git`
- Public image pull: `docker pull daum.dev/daniel/<image>:<tag>`
- No JavaScript. Matches danieldaum.net (saturn): dark only, saturn's Flexoki-based
  palettes (moss by default), Geist Pixel for all text, Geist Mono for code. See `docs/theme.md`.

## Non-goals

- Login, issues, comments, search, forks, stars, blame, editing anything.
- Private repos. They must never appear, even by mistake.
- Showing Tangled or GitHub data. Gitea is the only source.

## Constraints for the implementer (Claude Code)

- End state is a single Docker image running a single process. No Caddy, cron,
  or process supervisor inside it.
- Keep it small. Target under ~1,800 lines of Go total, excluding templates and CSS.
- Small commits, one feature each. No commit over ~400 changed lines.
- Go, standard library first. Allowed dependencies: `github.com/yuin/goldmark`
  (Markdown) and `github.com/alecthomas/chroma/v2` (highlighting). Ask before adding others.
- Shell out to the `git` CLI for repo data (`os/exec`), no go-git.
- Config is JSON (stdlib), not TOML/YAML.
- Templates and CSS embedded with `embed`.
- Don't copy knotless markup or CSS: its license is unknown. Use its layout as
  inspiration only (header → action row → clone box → latest commit → tree → README).

## Environment

- Gitea: Docker on the home server, user `daniel`. Moving to Tailscale-only (no public HTTPS).
  Must be Gitea ≥ 28.3.0 (heatmap API leaked private activity before that).
- The container runs on a home server/VM and listens on plain HTTP (`:8080`).
  Pangolin terminates TLS and proxies `daum.dev` to it. Cloudflare sits in front of Pangolin.
- atproto handle `@daum.dev` is verified by DNS TXT, so the site doesn't need
  `/.well-known/atproto-did`.
- This replaces the current `daum.dev` → `danieldaum.net` redirect. Link to danieldaum.net from the profile.
- Everything here goes down with the home server. Acceptable for now.

## Architecture

```
  Cloudflare → Pangolin (TLS) → daumdev container :8080  (one process)
                                  ├─ static files   (/data/site/current)
                                  ├─ /<repo>.git/…   → Gitea (fetch only)
                                  ├─ /v2/…           → Gitea (GET/HEAD only)
                                  └─ builder goroutine, every 10 min
                                       └─ anonymous HTTP over Tailscale → Gitea API + git
```

- `net/http` serves files and routes; `httputil.ReverseProxy` handles the
  pass-through (`FlushInterval: -1` so clones stream).
- The builder runs once at startup, then on a `time.Ticker`. It holds a mutex so
  builds never overlap. The server keeps serving the previous build while a new one runs.
- The allowlist is read from `config.json` in memory, so there's nothing to
  generate or reload. Config changes take effect on container restart.
- Server timeouts: `ReadHeaderTimeout` 10s, `IdleTimeout` 60s. No `WriteTimeout`
  on proxied routes (large clones and layers); 30s on page routes.
- `GET /healthz` returns 200 once the first build exists.

### Security rules

1. **The generator uses no token.** Every API call and clone is anonymous, so it
   can only ever see public data. A private repo listed in config just fails to load and is skipped with a warning.
2. **One allowlist:** repo and image names come from `config.json`. The builder
   only renders those, and the proxy only forwards those.
3. **The router's default is 404.** Only these routes exist:
    - `GET` static files.
    - `GET /<repo>.git/info/refs` only when the query is exactly `service=git-upload-pack`.
    - `POST /<repo>.git/git-upload-pack`.
    - Anything with `git-receive-pack` → 403.
    - `GET|HEAD /v2/`, `GET /v2/token`, `GET|HEAD /v2/daniel/<image>/(manifests|blobs)/…` for allowlisted images.
    - Clone paths rewrite `/<repo>.git/…` → `/daniel/<repo>.git/…` upstream.
4. **Registry token realm:** Gitea builds the `WWW-Authenticate` realm from its
   `ROOT_URL`, which will be a tailnet address. The proxy's `ModifyResponse`
   rewrites the realm to `https://daum.dev/v2/token`.
   Also strip any `Set-Cookie` and Gitea-specific headers from proxied responses,
   and never forward the client's `Authorization` or `Cookie` headers on git routes.
5. **Rendering:** goldmark with raw HTML disabled (default). Escape everything
   else through `html/template`. Files over 512 KB or binary: show size and a raw link, no render.
   README images: only relative paths inside the repo or https URLs.
6. **Headers:** `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self' https:; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`,
   plus `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. Raw files served as `text/plain` or with `Content-Disposition: attachment`.
7. **Container:** runs as a non-root user with a read-only root filesystem,
   `cap_drop: [ALL]` and `no-new-privileges`. The only writable path is the
   `/data` volume (site builds + bare git mirrors). Config is mounted read-only at `/config.json`.
8. Trust `X-Forwarded-*` only from Pangolin's address (used for logs only).
9. **Request limits:** cap request bodies on `git-upload-pack` (e.g. 10 MB) and
   reject any other method with a body.

## Config (`config.json`)

```json
{
    "gitea_url": "http://gitea.tailnet.example:3000",
    "user": "daniel",
    "site_url": "https://daum.dev",
    "links": {
        "website": "https://danieldaum.net",
        "atproto": "https://bsky.app/profile/daum.dev",
        "tangled": "https://tangled.org/daum.dev",
        "github": "https://github.com/..."
    },
    "repos": ["repo-a", "repo-b"],
    "images": ["repo-a"],
    "max_commits": 50
}
```

## Data sources

| Page piece                                               | Source                                                                                                           |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Profile (avatar, name, bio)                              | `GET /api/v1/users/{user}`                                                                                       |
| Heatmap                                                  | `GET /api/v1/users/{user}/heatmap` (rendered as server-side SVG)                                                 |
| Repo meta (description, topics, default branch, website) | `GET /api/v1/repos/{user}/{repo}`                                                                                |
| Language bar                                             | `GET /api/v1/repos/{user}/{repo}/languages`                                                                      |
| Releases                                                 | `GET /api/v1/repos/{user}/{repo}/releases`                                                                       |
| Pull requests                                            | `GET /api/v1/repos/{user}/{repo}/pulls?state=all`                                                                |
| Packages                                                 | `GET /api/v1/packages/{user}` (filter to repo-linked)                                                            |
| Tree, files, log, diffs, README                          | local bare mirror: `git clone --mirror` / `git fetch`, then `git ls-tree`, `git cat-file`, `git log`, `git show` |

## Pages (URL → content)

- `/` — profile card, links, heatmap, repo list (name, description, language, updated).
- `/<repo>/` — header (name, description, topics, latest release), action row
  (Code · Commits · Releases · Pulls · Packages), clone box (HTTPS + Tangled/GitHub links),
  latest commit line, root tree with last-commit message and age per entry, rendered README.
- `/<repo>/tree/<path>/` — directory listing.
- `/<repo>/blob/<path>` — highlighted file with line anchors (`#L12`).
- `/<repo>/raw/<path>` — raw bytes.
- `/<repo>/log/` — last `max_commits` commits on the default branch.
- `/<repo>/commit/<sha>/` — message, stats, unified diff (for those commits only).
- `/<repo>/releases/` — tag, title, date, notes (Markdown), asset links.
- `/<repo>/pulls/` — title, state, author, date, body. No diffs.
- `/<repo>/packages/` — name, version, type, pull/install command.
- Go repos: add `<meta name="go-import" content="daum.dev/<repo> git https://daum.dev/<repo>.git">` so `go get daum.dev/<repo>` works.

Only the default branch is browsable. Tags show on the releases page.

## Build loop

1. For each repo: fetch mirror. If HEAD and API `updated_at` are unchanged since last run, reuse the old output.
2. Render into `/data/site/.tmp-<ts>/`, then swap a `current` symlink atomically. Keep one previous build.
3. On error: log it, keep serving the last good build, retry next tick. Never publish a partial build.
4. If there's no good build yet, the server returns 503 for pages (proxy routes still work).

## Repo layout

```
cmd/daumdev/main.go    entrypoint: load config, start builder + server
internal/gitea/        API client (only the calls above)
internal/gitx/         git CLI wrappers
internal/build/        build loop, change detection, atomic swap
internal/render/       templates, markdown, highlight, heatmap SVG
internal/server/       router, static files, git + registry proxy, headers
web/templates/*.html   embedded
web/static/style.css   embedded, saturn tokens + components
Dockerfile             multi-stage: golang → alpine + git, non-root
compose.yaml           one service, example for deployment
testdata/              sample Gitea JSON + a tiny test repo
```

### Image

- Stage 1: `golang:<pinned>` builds a static binary (`CGO_ENABLED=0`, `-trimpath`).
- Stage 2: `alpine:<pinned>` with `git` and `ca-certificates` only (git is needed
  for mirrors; that's why it isn't distroless).
- `USER 10001`, `EXPOSE 8080`, `VOLUME /data`, `HEALTHCHECK` hitting `/healthz`.
- Pin base images by digest. Mirrors are owned by the runtime user, so don't
  add `safe.directory '*'`.
- One `compose.yaml` service:

```yaml
services:
    daumdev:
        image: daumdev:latest
        read_only: true
        cap_drop: [ALL]
        security_opt: ["no-new-privileges:true"]
        volumes:
            - daumdev-data:/data
            - ./config.json:/config.json:ro
        ports: ["127.0.0.1:8080:8080"]
        restart: unless-stopped
volumes:
    daumdev-data:
```

## Build order (one commit each)

1. Skeleton, config loading, Gitea client, profile page with heatmap. Golden test from `testdata`.
2. Git mirror wrapper; repo page with README, latest commit, root tree.
3. Tree, blob (chroma), raw pages; size/binary limits.
4. Log and commit diff pages.
5. Releases, pulls, packages pages.
6. Builder loop: ticker, mutex, atomic swap, change detection.
7. Server: static files, security headers, `/healthz`, 404 default.
8. Git and registry proxy with route allowlist and realm rewrite.
9. Dockerfile + compose.yaml.
10. Security pass: Go tests for the router proving push, API, UI and unlisted paths return 403/404.

## Done when

- `git clone https://daum.dev/<repo>.git` works; `git push` to the same URL fails.
- `docker pull daum.dev/daniel/<image>` works anonymously; `docker push` fails.
- `/api/v1/…`, `/user/login`, `/daniel/<repo>` and any unlisted repo return 404.
- A private repo added to config never appears anywhere.
- Pages pass the CSP with zero console errors and render without JavaScript.
