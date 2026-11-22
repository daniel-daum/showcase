# Decisions

Choices made while building, where the spec was silent or had to bend.
Newest last.

## Heatmap comes from git, not the Gitea heatmap API

The heatmap API leaked private activity before Gitea 28.3.0, and the server
runs 28.2.x. The builder counts commits on the default branch of the
allowlisted repos from the local mirrors instead. Private activity can't leak
whatever Gitea version runs, but only commits are counted (no PRs, issues and
so on). All authors are counted, since the anonymous API hides the user's
email, so merged commits from others show up too.

## Packages are filtered by the allowlist

Gitea package visibility follows the owner, not the linked repo, so a package
linked to a private repo is public. Only packages linked to an allowlisted
repo are listed.

## Change detection: reuse git pages when nothing they show changed

Repo `updated_at` doesn't reliably change when PRs or releases change, so it
isn't used. Every build re-fetches the API and re-renders the profile, each
repo's root page, the log page and the releases, pulls and packages pages.
File, folder, raw and commit pages are hard-linked from the previous build
when the reuse key is unchanged. The key is HEAD plus the repo header data
(description, website, topics, archived, default branch, latest tag), because
every page shows that header. Keys live in memory, so a restart (which is
also how config or template changes ship) re-renders everything.

Builds are written to `site/.tmp-<ts>`, renamed to `site/<ts>`, and published
by renaming a fresh `current.new` symlink over `current`. The newest and the
previous build are kept, and anything else (including temp dirs left by a
crash) is removed after each swap.

## Tailnet URLs never reach the page

Gitea builds URLs from its `ROOT_URL`, a tailnet address. The avatar is
downloaded into the build. Release assets go through one more allowlisted
proxy route, `/<repo>/releases/download/<tag>/<file>`. The registry proxy
refuses upstream redirects so a storage backend URL is never handed out.

## Raw files: images keep their type, everything else is text

README images resolve to `/<repo>/raw/...`, so png, jpg, gif and webp are
served with their real types. SVG and everything else is served as
`text/plain` or as an attachment. `nosniff` and the CSP stay on.

## Smaller choices

- `go get` on subpackages: any `/<repo>/...?go-get=1` gets the go-import meta.
- The git proxy forwards `Git-Protocol` so protocol v2 works.
- Git runs with `GIT_TERMINAL_PROMPT=0` and an empty credential helper, so a
  private repo in config fails fast instead of hanging on a prompt.
- Config rejects repo names that clash with routes (`static`, `v2`, `healthz`,
  `api`, `user`).
- Dates are bucketed in UTC.
- README links resolve to `blob/` pages, or to `tree/` pages when they end in
  `/`. Images that aren't relative or https are dropped and their alt text
  kept. Goldmark blanks dangerous link schemes such as `javascript:`.
- The go-import meta is added when the repo root has a `go.mod`.
- The clone box shows only the HTTPS URL for now. Tangled and GitHub links
  need per-repo config, because not every repo is mirrored there.
- Tooling: Go is pinned in `mise.toml`, and commands live in the `justfile`.

## Theme follows danieldaum.net (saturn)

The site's look matches saturn instead of a generic Flexoki light/dark theme.
It is dark only and uses saturn's tokens, palettes, fonts and components. The
details are in `docs/theme.md`. This replaces the spec's original
"light and dark via `prefers-color-scheme`".

## File pages

- A blob page for `<path>` is stored as `<repo>/blob/<path>.html`, and the
  server maps `/blob/<path>` to it. Raw bytes are stored at `<repo>/raw/<path>`.
- Files over 50 MB get no raw copy, only a note to clone. Files over 512 KB,
  or binary ones (a NUL byte in the first 8 KB, or not valid UTF-8), get no
  highlighted view. png, jpg, gif and webp are shown as images.
- The last commit per entry comes from one `git log --name-only` pass over
  history, not one `git log` per path.
- Folder pages show their own README, if they have one.
- Highlighting: chroma tokens map to saturn's `hl-*` roles in
  `internal/render/highlight.go`. README code blocks use the same mapping.

## Log and commit pages

- Only the commits on the log page (the last `max_commits` on the default
  branch) get commit pages. Parent links show only when the parent has a page.
- Diffs are against the first parent (`--diff-merges=first-parent`), with
  rename detection. At most 5,000 diff lines are kept per commit. The rest are
  counted in the stats but not shown.

## Releases, pulls and packages

- If Gitea returns 404 for releases, pulls or packages (for example, the
  feature is turned off for that repo), the page is empty. Any other API error
  fails the build.
- Draft releases are never shown. The repo header shows the newest
  non-prerelease tag.
- Release assets link to `/<repo>/releases/download/<tag>/<file>`, which the
  proxy (step 8) forwards.
- PR bodies sit in `<details>`, so they open without JS.
- Packages: container images are listed only when the image is in `images`
  (so every `docker pull` shown actually works), and untagged `sha256:`
  versions are hidden. Other package types are listed with no install command,
  because their registries aren't proxied.

## Server

- Routes: `/`, `/healthz`, `/static/…` and `/<repo>/…` for allowlisted repos.
  Inside a repo, `raw/` serves files, `blob/<p>` serves `<p>.html`, paths
  ending in `/` serve `index.html`, and a folder without the slash redirects.
  Everything else, including any path with dot segments, `//`, `\` or NUL, is
  a 404 using the built `404.html`. `/index.html` itself is a 404 too.
- Methods other than GET and HEAD get a 404, and a GET or HEAD with a body
  gets a 400. Step 8 adds the git `POST` route.
- Raw files: png, jpg, gif and webp keep their image types. Files with a NUL
  in the first 512 bytes are `application/octet-stream` with
  `Content-Disposition: attachment`. Everything else, SVG and HTML included,
  is `text/plain`.
- Page responses get a 30s write deadline through `http.ResponseController`.
  The server itself has no `WriteTimeout`, so proxied clones can stream.
- Logs: for peers in `trusted_proxies`, the client IP comes from
  `CF-Connecting-IP`, or else the last `X-Forwarded-For` entry. Otherwise it's
  the TCP peer.
- `-serve-dir DIR` (dev only) serves a finished build without building; `just
  serve` and `just demo` use it.

## Git and registry proxy

- Proxy routes are matched before anything else and work before the first
  build. Within the `.git`, `/v2` and `releases/download` prefixes anything not
  explicitly allowed is a 404. Any path or query containing
  `git-receive-pack` is a 403.
- Requests to Gitea never carry `Cookie` or `X-Forwarded-*`. `Authorization`
  passes only when it is a `Bearer` token on a registry manifest or blob
  route, which is the anonymous token Gitea just issued. Basic credentials
  are never forwarded.
- `/v2/token` is forwarded only when every `scope` is a pull of an
  allowlisted image. `_catalog` and `tags/list` are 404.
- Any 3xx from Gitea becomes a 502, so storage or tailnet URLs never reach
  clients. With Gitea's local storage, blobs and assets are served directly,
  so this doesn't happen in practice.
- Release assets are always `application/octet-stream` with
  `Content-Disposition: attachment`.
- The `git-upload-pack` body is capped at 10 MB and must have the git
  request content type.
- Gitea must allow anonymous HTTP git and registry reads
  (`REQUIRE_SIGNIN_VIEW = false`, HTTP git not disabled), or public clones and
  pulls fail.
