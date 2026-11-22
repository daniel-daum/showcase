# Decisions

Choices made while building, where the spec was silent or had to bend.
Newest last.

## Heatmap comes from git, not the Gitea heatmap API

The heatmap API leaked private activity before Gitea 28.3.0, and the server
runs 28.2.x. The builder counts commits on the default branch of the
allowlisted repos from the local mirrors instead. Private activity can't leak
whatever Gitea version runs, but only commits are counted (no PRs, issues and
so on).

## Packages are filtered by the allowlist

Gitea package visibility follows the owner, not the linked repo, so a package
linked to a private repo is public. Only packages linked to an allowlisted
repo are listed.

## Change detection is keyed on HEAD only

Repo `updated_at` doesn't reliably change when PRs or releases change. API
pages (profile, releases, pulls, packages) are re-fetched and re-rendered
every build. Only git-derived pages (tree, blob, log, commit) are reused when
HEAD is unchanged.

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
- Fonts: Geist Pixel Square from the `geist` npm package (OFL). Body text uses
  system fonts.
- Tooling: Go is pinned in `mise.toml`, and commands live in the `justfile`.
