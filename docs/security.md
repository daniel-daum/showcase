# Security review

Step 10 of the spec. Each rule and each "done when" item maps to the code that
enforces it and the test that proves it. Run them all with `just test`.

## Spec security rules

| # | Rule | Enforced in | Tested by |
|---|------|-------------|-----------|
| 1 | Generator uses no token, so it can only see public data | `gitea.Client` never sets credentials and refuses redirects. `gitx` runs git with no prompt, no credential helper, no user config and http(s) only. A repo the API marks private is refused anyway. | `fakeGitea` fails any request that carries `Authorization` or `Cookie`; `TestOutputIsSafe` |
| 2 | One allowlist (`config.json`) | Builder iterates `cfg.Repos`. Proxy checks `HasRepo` / `HasImage`. Packages are filtered to allowlisted repos and images. | `TestSiteGolden` (missing and private repos absent), `TestProxyRoutes` (unlisted repo and image) |
| 3 | Router default is 404, with only the listed routes | `server.route` and `proxyRoute`. Dot segments, `//`, `\` and NUL are refused before any file lookup. | `TestRoutes`, `TestProxyRoutes` |
| 4 | Realm rewrite, strip `Set-Cookie` and Gitea headers, no client `Authorization`/`Cookie` on git | `newProxy` Rewrite and ModifyResponse. Non-Bearer challenges are dropped too, so git never prompts. | `TestProxyRoutes`, `TestProxyResponses` |
| 5 | Safe rendering | goldmark; raw HTML is never passed through, only rebuilt from an allowlist of tags and attributes (`htmlRenderer`), with links and images resolved like Markdown's and only http(s)/mailto hrefs; dangerous URLs blanked, `html/template` everywhere, highlighter escapes every token, 512 KB / binary limits, README images relative or https only | `TestSiteGolden` (forbidden strings), `TestOutputIsSafe` (no `<script`, inline `style=` or `on*=` in any page), `TestReadmeHTML` (script, handlers, styles, `javascript:`, stray closes) |
| 6 | Security headers; raw never rendered as HTML | `route` sets the CSP, `nosniff` and `no-referrer` on every response. `raw` serves allowed images, `text/plain` or an attachment. Release assets are always attachments. | `TestRoutes` (headers on every case, raw SVG and HTML are `text/plain`), `TestProxyResponses` |
| 7 | Container: non-root, read-only, no caps, no-new-privileges, only `/data` writable | `Dockerfile` (`USER 10001`, `HOME=/data`), `compose.yaml` | `hadolint`; container not yet run (see open items) |
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

## Accepted risks

- PR and release bodies can embed https images (the CSP allows `img-src
  https:`). Those are tracking-pixel capable, but it's your own repos'
  content.
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
