# Security review

Step 10 of the spec. Each rule and each "done when" item maps to the code that
enforces it and the test that proves it. Run them all with `just test`.

## Spec security rules

| # | Rule | Enforced in | Tested by |
|---|------|-------------|-----------|
| 1 | Generator uses no token, so it can only see public data (one exception: an optional token for the package listing) | `gitea.Client` sets credentials only on the package listing (`tokenPath`), refuses redirects, and the builder keeps only packages linked to repos that loaded anonymously. `gitx` runs git with no prompt, no credential helper, no user config and http(s) only. A repo the API marks private is refused anyway. | `fakeGitea` fails any request that carries `Authorization` or `Cookie`; `TestOutputIsSafe`; `TestTokenOnlyForPackages`, `TestTokenNotFollowedOnRedirect` |
| 2 | One allowlist (`config.json`) | Builder iterates `cfg.Repos`. Proxy checks `HasRepo` / `HasImage`. Packages are filtered to allowlisted repos and images. | `TestSiteGolden` (missing and private repos absent), `TestProxyRoutes` (unlisted repo and image) |
| 3 | Router default is 404, with only the listed routes | `server.route` and `proxyRoute`. Dot segments, `//`, `\` and NUL are refused before any file lookup. | `TestRoutes`, `TestProxyRoutes` |
| 4 | Realm rewrite, strip `Set-Cookie` and Gitea headers, no client `Authorization`/`Cookie` on git | `newProxy` Rewrite and ModifyResponse. Only `forwardHeaders` reach Gitea, so headers it might trust (`X-WEBAUTH-USER`) can't be injected. Non-Bearer challenges are dropped too, so git never prompts. | `TestProxyRoutes`, `TestProxyResponses`, `TestProxyDropsHeaders` |
| 5 | Safe rendering | goldmark; raw HTML is never passed through, only rebuilt from an allowlist of tags and attributes (`htmlRenderer`), with links and images resolved like Markdown's and only http(s)/mailto hrefs; dangerous URLs blanked, `html/template` everywhere, highlighter escapes every token, 512 KB / binary limits, README images relative or https only | `TestSiteGolden` (forbidden strings), `TestOutputIsSafe` (no `<script`, inline `style=` or `on*=` in any page), `TestReadmeHTML` (script, handlers, styles, `javascript:`, stray closes) |
| 6 | Security headers; raw never rendered as HTML | `route` sets the CSP, `nosniff` and `no-referrer` on every response. `raw` serves allowed images, `text/plain` or an attachment. Release assets are always attachments. | `TestRoutes` (headers on every case, raw SVG and HTML are `text/plain`), `TestProxyResponses` |
| 7 | Container: non-root, read-only, no caps, no-new-privileges, only `/data` writable | `Dockerfile` (`USER 10001`, `HOME=/data`), `deploy/compose.yaml` | `hadolint`; container not yet run (see open items) |
| 8 | Trust `X-Forwarded-*` only from Pangolin | `clientIP` uses `trusted_proxies`. The proxy strips `X-Forwarded-*` and `Forwarded` toward Gitea. | `TestClientIP`, `TestProxyRoutes` |
| 9 | Cap `git-upload-pack` bodies, reject other bodies | 10 MB `MaxBytesReader`. A GET or HEAD with a body gets a 400. Proxy GET routes require no body. | `TestProxyResponses`, `TestBodyRejected` |

## "Done when"

| Item | Status |
|------|--------|
| `git clone https://daum.dev/<repo>.git` works and `git push` fails | Proven with real git through the server: `TestCloneAndPush`. The fake Gitea there accepts pushes, so the refusal is ours. |
| `docker pull` works anonymously and `docker push` fails | Push methods and push scopes are refused (`TestProxyRoutes`). The pull path is unit-tested but **not yet run against real Gitea**. |
| `/api/v1/…`, `/user/login`, `/daniel/<repo>` and unlisted repos are 404 | `TestRoutes`, `TestProxyRoutes` |
| A private repo in config never appears anywhere | `TestOutputIsSafe` scans every output file, raw files included. A private, missing or renamed repo is skipped. Any other error fails the whole build, so a flaky API can't publish a site with repos missing. |
| Pages pass the CSP with no console errors and need no JS | No scripts anywhere. `TestOutputIsSafe` rules out inline styles and handlers; it caught goldmark's table-alignment `style=`, which is now an `align` attribute. Fonts, CSS and avatar are same-origin. **Not yet checked in a real browser.** |

## Fixed during this review

- GFM table alignment rendered `style="text-align:…"`, which the CSP blocks.
  It is now an `align` attribute.
- Registry routes: blob refs must be `sha256:<64 hex>`, and manifest refs a
  valid tag or digest, so paths like `blobs/uploads` never reach Gitea.
- `WWW-Authenticate` from Gitea is passed only when it is the registry's
  Bearer challenge (with the realm rewritten). Basic challenges are dropped, so
  git and docker never prompt for credentials that would go nowhere.
- A repo is skipped only when Gitea says it's not found or private. Any other
  error fails the build and the previous one stays live.

## Changes after the review

- The CSP now has `script-src 'self'` for `/static/copy.js` (copy buttons).
  Inline scripts and handlers are still impossible, and `TestOutputIsSafe`
  allows exactly that one script tag and nothing else.
- Repo SVGs on `raw/` stay `text/plain` even though `/static` serves our own
  icons as `image/svg+xml` (covered by `TestRoutes`).
- The CSP now has `connect-src 'self'` (2026-11-24), so `copy.js` can fetch
  the raw file a file page's copy button copies. Requests go to this origin
  only; `img-src https:` was already a wider way out. See decisions, "Copy
  a file".

## Second review (2026-11-23)

After issues, comments and the package token. Read through the server,
proxy, rendering, git wrappers, builder, registry client, config, scripts,
Dockerfile and compose file.

Fixed:

- **Request headers to Gitea are an allowlist** (`forwardHeaders`). The
  proxy used to pass everything except `Cookie` and some `Authorization`.
  If Gitea ever had reverse-proxy login on (`X-WEBAUTH-USER`), a visitor
  could have claimed to be you. `TestProxyDropsHeaders`.
- **At most 8 concurrent git requests** go through the proxy; more get a
  503 with `Retry-After`. Upload-pack is the expensive call for Gitea, and
  anyone can start one. `TestProxyGitSlots`.
- **Registry blobs are served as downloads** (`application/octet-stream`,
  attachment), like release assets, so no layer can render as a page on
  this origin.
- **Text from strangers is capped.** Comments mean anyone with a Gitea
  account can put text on the site. Issue, pull and comment bodies are cut
  at 32 KB and a thread shows its newest 100 comments, saying how many it
  left out. `TestUserTextLimits`.

Checked and fine: path handling (dot segments, `//` and NUL refused; tree
entries with `.`/`..`/empty segments skipped; only regular files served; no
open redirect through `base_path`), the HTML allowlist (`javascript:`
through entities, control characters or leading spaces fails URL parsing
and is dropped; the CSP blocks inline script anyway), git invocations
(`--end-of-options`, literal pathspecs, no prompts or helpers, http(s) only,
bare mirrors so no submodule or checkout hooks), registry layer reading
(64 MB compressed, 16 KB per file, streamed, 30 s timeout), the token (never
logged, never in config.json, never sent elsewhere), and the scripts
(`theme.js` only accepts `[a-z]+` from the cookie).

## Accepted risks

- Issue, pull, comment and release bodies can embed https images (the CSP
  allows `img-src https:`). With comments, that includes strangers'
  tracking pixels; `no-referrer` hides the page, not the visitor's IP.
- Anyone with a Gitea account can write issues and comments that show up
  here (sanitized and capped). Gitea is tailnet-only with sign-ups off, so
  that's only accounts you create.
- README headings get automatic `id`s, so a heading could share an id with
  a page element. The page's own elements come first, so `copy.js` still
  finds them; the worst case is a mis-scrolled anchor.
- A repo with a file `x` and a directory `x.html` side by side makes its blob
  pages collide, and the build fails. It only affects your own repos and
  shows up as a build error, never as wrong content.
- Builds load whole blobs (up to 50 MB) and patches into memory. That's fine
  for your own repos, but it isn't a defence against hostile repos.

## Open items (need the real environment)

1. Run the image on the host: read-only root, uid 10001, healthcheck.
2. `docker pull daum.dev/daniel/<image>` through Pangolin and Cloudflare.
3. Open pages in a browser and confirm there are no CSP console errors.
4. Check Gitea's settings allow anonymous reads (`REQUIRE_SIGNIN_VIEW = false`).
5. `govulncheck` v1.10.0 on 2026-11-23 (Go 1.27.4, goldmark 1.8.7, chroma
   2.27.0): no vulnerabilities found. Run it again before each release
   (`go run golang.org/x/vuln/cmd/govulncheck@latest ./...`; older
   releases crash on Go 1.27's standard library), and rebuild the image
   for new Go, Alpine and git releases (`just digests`).
6. Compose now sets `mem_limit`/`memswap_limit` 512m, `cpus` 1.0 and
   `pids_limit` 256. Watch the first builds for an OOM kill.
