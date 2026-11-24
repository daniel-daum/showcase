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

## Image

- Base images are `golang:1.27.4-alpine3.24` and `alpine:3.24.4`, pinned by
  digest. `just digests` prints the current upstream digests for bumping. Go
  is the same version as `mise.toml`.
- apk packages (`git`, `ca-certificates`) aren't version-pinned, since
  Alpine drops old package versions. The base image digest bounds them.
- `HOME=/data`, so nothing tries to write elsewhere on the read-only root.
  Builds only write under `/data`.
- `.dockerignore` is an allowlist: only `go.mod`, `go.sum`, `cmd/`,
  `internal/` and `web/` enter the build context, and tests are excluded.
- `crane` and `hadolint` are project tools in `mise.toml`.

## Reaching Gitea

- Today Gitea's HTTPS is public at `git.danieldaum.net`, and only SSH is
  tailnet-only. `gitea_url` is `https://git.danieldaum.net` for now. SSH isn't
  an option, because it needs a key and the builder must stay anonymous.
- Once Gitea goes private, the best option is a shared Docker network on the
  same host (`gitea_url: http://gitea:3000`), so no tailnet hop is needed.
  Otherwise expose Gitea's HTTP port on the tailnet only.
- Gitea 28's packages API requires a token even for public packages, so it
  answers anonymous calls with 401. 401 and 403 now count as "not publicly
  visible", the same as 404. The build logs a warning, and the packages page
  stays empty.

## Polish pass: design, icons, one small script

- **Design:** saturn's palettes and tokens stay, but daum.dev has its own type
  and component system, because saturn's all-pixel, all-caps style reads
  poorly in dense listings. Text is Geist, code and hashes are Geist Mono, and
  headings and the brand are Geist Pixel. Labels are sentence case.
  Everything sits in one bordered "box" component. Repo pages are up to 68rem
  wide.
- **File icons:** replaced in polish pass 3 (Phase 3) by Phosphor icons.
- **Copy buttons need JS**, which departs from the spec's "No JavaScript".
  `/static/copy.js` is about 15 lines, same-origin and progressive: without
  it, the buttons stay hidden and the read-only fields can still be copied by
  hand. The CSP gains `script-src 'self'`, and nothing inline is allowed. To
  drop it, delete `copy.js`, its `<script>` tag and the CSP entry.
- **Branches and tags pages** (`/<repo>/branches/`, `/<repo>/tags/`) list refs
  with their last commit. Only the default branch is browsable. Tags link to
  their release.
- **CI checks:** the status of HEAD comes from Gitea's commit status API. The
  check badge links to the commit page's `#checks` list. Gitea's own run
  pages aren't linked, since the Gitea UI is never exposed.
- **Freshness:** ages within a day, week or month get warmer colours.
- **Stylesheet cache-busting:** the CSS (and `copy.js`) URLs carry a hash of
  `style.css`, so caches never serve a stale stylesheet.

## Polish pass 3

### Phase 1: contrast and type

All changes are in part 2 of `style.css`. Saturn's tokens in part 1 are
untouched, and every new value is a `color-mix()` of them, so ink, wine,
plum, teal and flexoki get the same steps as moss.

- **Layers follow the Paint themes for Gitea** (`gitea/theme/css/`). The
  first try mixed saturn's `--surface` and `--borders` into boxes. They are
  strongly saturated (moss surface is an olive at chroma 0.056), so boxes
  looked off-colour against a near-black page. Paint uses one lightness
  ladder in every palette, tinted only slightly toward the palette's hue: in
  OKLab, page L 0.205, box 0.222, box header 0.245, hover 0.267, border 0.36,
  all at chroma ~0.015. Here each layer is
  `color-mix(in oklab, var(--background-dark) 55%, <neutral grey>)`, which
  lands within 1–2 RGB units of every Paint theme and extends to flexoki,
  which has no Paint theme:

  | token        | grey      | moss result | Paint moss |
  | ------------ | --------- | ----------- | ---------- |
  | `--page`     | `#141414` | `#171a10`   | `#16180e`  |
  | `--box`      | `#1c1c1c` | `#1b1d14`   | `#1a1c12`  |
  | `--box-head` | `#282828` | `#202319`   | `#1f2217`  |
  | `--hover`    | `#343434` | `#25281e`   | `#25271d`  |
  | `--rule`     | `#4e4e4e` | dividers inside boxes | |
  | `--edge`     | `#6d6d6d` | `#3d4035`   | `#3c3f33`  |
  | `--line`     | `#808080` | controls    |            |

  Nothing in part 2 uses `--surface`, `--borders` or `--accent-muted` any
  more. Chips, buttons, the clone menu, code areas, inline code, `hr`, the
  count pills and topic pills now sit on the ladder or on a light mix of the
  accent. Hover borders use `--accent-edge` (the accent mixed into `--edge`).
- **Text:** `--body-text` (45% body, 55% headings) for body copy,
  descriptions and the tree's commit column. File names, repo names, commit
  subjects and counts use `--text-headings` at weight 500–600, and
  `--text-secondary` is kept for metadata. Ages, sizes, counts and stats use
  tabular figures. "No description." on repo cards stays dim
  (`repo-card-desc--none`).
- **Accent:** `--accent-bright` (accent 82%, white 18%) is used for hovers,
  active tabs, breadcrumb links, the profile name, the 404 and one shared
  `:focus-visible` ring (`a`, `button`, `summary`, `input`). `--accent` stays
  for fills: the primary button, the language bar, the top heatmap step and
  the `:target` line edge. Heatmap steps `--heat-1..3` mix the accent into
  `--box` (30/52/76%), like Paint's primary ramp, instead of starting from
  the saturated `--accent-muted`.
- **Depth:** Paint's card shadow (`0 1px 1px` at 25% black) plus a very faint
  top highlight, on boxes, repo cards and box heads. An accent glow behind
  the avatar was tried and dropped: the avatar has a plain 1px ring.
- **Contrast:** `web/contrast_test.go` reads the tokens from `style.css`,
  evaluates the `color-mix()` chains in OKLab and fails below 4.5:1. Run
  `go test -v -run Contrast ./web` to see the ratios. At the time of writing:

  | text on              | moss  | ink   | wine  | plum  | teal  | flexoki |
  | -------------------- | ----- | ----- | ----- | ----- | ----- | ------- |
  | headings / page      | 15.46 | 15.71 | 15.78 | 15.80 | 15.46 | 16.47   |
  | headings / box-head  | 14.01 | 14.30 | 14.38 | 14.40 | 14.01 | 15.23   |
  | body / box           | 13.49 | 13.74 | 13.80 | 13.82 | 13.49 | 14.49   |
  | body / hover         | 11.89 | 12.17 | 12.24 | 12.26 | 11.89 | 13.06   |
  | secondary / page     | 6.51  | 6.62  | 6.65  | 6.65  | 6.51  | 6.94    |
  | secondary / box      | 6.27  | 6.39  | 6.42  | 6.42  | 6.27  | 6.74    |
  | secondary / box-head | 5.90  | 6.02  | 6.06  | 6.07  | 5.90  | 6.41    |
  | secondary / hover    | 5.53  | 5.66  | 5.69  | 5.70  | 5.53  | 6.07    |
  | accent-bright / box  | 8.45  | 7.57  | 7.89  | 8.13  | 8.86  | 7.55    |

  Line numbers keep saturn's dimmed look (opacity 0.55) and are exempt, as
  decoration next to the code.

### Phase 2: language bar above the file tree

- The language legend (dot, name, percent) is a compact row between the tabs
  and the code box. The bar is a 4px SVG strip, the first child of the code
  box. The box's `overflow: hidden` clips it to the rounded top corners and
  keeps it inside the border. The segments are the same as before: adjacent
  rects with no extra gap, the six largest languages plus "other". The old
  `.langs` section under the box is gone.
- Empty repos show neither, as before: there's no code box and Gitea reports
  no languages.

### Phase 3: Phosphor icons

> Replaced by Gitea's icons, see "Gitea's icons again" at the end. The
> per-page sprite described here stays.

- **One icon family.** File icons come from the Phosphor Colored set in
  Daniel's dotfiles (`.config/zed/phosphor-colored-icons`). UI icons (tabs,
  stats, clone, copy, CI, people) are Phosphor Regular from
  `@phosphor-icons/core` 2.1.1. Both are MIT. The 47 file glyphs and the set's
  `LICENSE` live in `web/static/icons/phosphor/`, and the 17 UI glyphs in use
  plus core's `LICENSE` live in `ui/` beneath it. The material icons and
  their licence are gone.
- **Mapping:** `scripts/icons.py` (`just icons <colored> <core>`) reads the
  `ICONS` table from the set's `scripts/build.py` with `ast.literal_eval`,
  without running it, and generates `internal/render/icons_gen.go`: exact
  names (`iconStems`) and suffixes (`iconSuffixes`) mapped to (glyph, role).
  When two keys claim the same suffix, the later one wins, as in build.py.
  The UI name map (`book` → `book-bookmark` and so on) is in the script.
  Zed's built-in mappings (`BUILTIN`) have no suffixes of their own in
  build.py, so they aren't used.
- **Lookup order** is Zed's, which build.py follows: the whole file name as
  a stem, then as a suffix, then each part after a dot from the left
  (`a.tar.gz` tries `tar.gz`, then `gz`), then `default` (the neutral
  document). One addition: if nothing matches, the same lookup runs again on
  the lowercased name, so `README.MD` still gets the markdown icon. Folders
  and submodules use `folder-closed`, and the current folder in tree-page
  breadcrumbs uses `folder-open`. Some common names fall through to the
  default icon, because the table doesn't cover them: `go.mod`, `justfile`
  and `.github`.
- **Colour by role, not hex.** `.r-<role>` classes map roles to saturn's
  syntax tokens, the same pairs as `palettes.json`: red → `--syn-variable`,
  orange → `--syn-constant`, yellow → `--syn-type`, green → `--syn-string`,
  cyan → `--syn-builtin`, blue → `--syn-function`, purple → `--syn-keyword`,
  magenta → a mix of variable and keyword, neutral and folder →
  `--text-secondary`. Icons follow `data-theme`.
- **Per-page sprite.** Every icon is `<svg class="ficon r-ROLE"><use
  href="#f-GLYPH">` or `<svg class="icon"><use href="#i-NAME">`. `base.html`
  emits an empty sprite. `render.Write` scans the finished page for `#f-` and
  `#i-` references and fills in only those `<symbol>`s, built once from the
  embedded SVGs. Pages carry just their own icons. Nothing is fetched, there
  are no `data:` URLs, and the CSP is unchanged. Phosphor paths are longer
  than the old hand-drawn strokes, so the repo home went from 16.7 KB to
  21.4 KB before compression, while the profile shrank. `static/icons/` isn't
  copied into the output, since nothing fetches it. The safety test fails if
  a page uses an icon its sprite lacks.
- UI icons are filled paths now, so `.icon` uses `fill: currentColor` and
  the stroke rules are gone. CI pending is `dot-outline`.

### Extra: document tabs in the README box

- On the repo root, the README box gets a tab bar when the repo has more than
  the README. In tab order: README, License (named when recognised, such as
  "MIT license"), Security, Contributing, Code of conduct, Support,
  Governance, Funding, Citation, Authors (also `CONTRIBUTORS`,
  `MAINTAINERS`), Notice and Changelog (also `CHANGES`, `HISTORY`, `NEWS`,
  `RELEASES`). Only the ones the repo has are shown. Each is the first file
  matching the stem, in any case (`LICENSE`/`LICENCE`/`COPYING` by prefix),
  with no extension or one of md, markdown, txt, rst, adoc, yml, yaml, cff
  or bib, in the root, then `.github/`, then `docs/`. The table is
  `docKinds` in `internal/build/repo.go`, and the CSS has panel rules for up
  to 13 tabs. Files
  over 512 KB are skipped. Markdown renders like the README, with relative
  links resolved from the file's own folder. Plain text, such as most
  LICENSE files, is a wrapping monospace block. Folder pages keep their
  single README box.
- **No JS:** the tabs are radio inputs (visually hidden) with labels. CSS
  shows the panel whose radio is checked, using `:has()` inside
  `@supports selector(:has(a))`. Without `:has` only the first document
  shows, which is the old README box. Radios also give keyboard switching
  (arrow keys) and a focus ring for free. The CSP is unchanged.
- Known limits: heading anchors inside a hidden tab don't scroll there, and
  two documents may produce the same heading id.
- New UI icons: `shield` (shield-check), `users`, `heart`, `lifebuoy`,
  `bank`, `coins`, `quotes` and `info`.
- The repo-a fixture gains `SECURITY.md`, `CONTRIBUTING.md`,
  `.github/CODE_OF_CONDUCT.md` and `.github/FUNDING.yml` (plain text).

### Extra: container images read from the registry

- Gitea 28's packages API wants a token, so anonymously the packages page
  was empty. Container images now come from Gitea's registry itself, which
  serves public images anonymously: `/v2/` answers with a bearer challenge,
  and `/v2/token` hands out a pull token without credentials. That token can
  only do what any `docker pull` can, so the builder stays anonymous (rule
  1). The token is requested from the configured `gitea_url`, never from the
  challenge's realm, kept in memory and never rendered. The test fake Gitea
  allows exactly that one `Authorization` header.
- **Which images:** allowlisted images (`images` in config) named like the
  repo, plus container packages the API links to the repo, when the API
  answers. Images outside `images` never appear. Other package types (npm
  and so on) still come from the API, listed under "Other packages".
- **What's shown:** per image, the `docker pull` of the newest tag. Then up
  to 8 versions, newest first, one per distinct manifest digest, with every
  tag that points at it (the first 30 tags are read). Each version is a
  `<details>`, and the newest is open. It shows the full digest, a
  pull-by-digest command, the created time, the manifest type, and each
  platform (attestation manifests skipped) with its full digest, compressed
  size, layer count and distro.
- **Distro:** read from `etc/os-release` (or `usr/lib/os-release`, which is
  where Alpine's symlink points) in the first three layers. Only gzip or
  plain tar layers up to 64 MB are read, and the stream stops as soon as the
  file is found. Otherwise the page says "No os-release (scratch or
  distroless)".
- **Metadata:** OCI labels for version, revision (linked to its commit page
  when it has one), base image, licenses, authors, vendor, description,
  source, URL and documentation, plus entrypoint, command, ports and user.
  The environment is never even parsed, since it can hold secrets. Gitea's
  hosts are private: the configured host and the challenge realm's host,
  which is `ROOT_URL` and may be a tailnet name. A source or URL on those
  hosts that points at an allowlisted repo becomes a link to its page here.
  Any other value naming them, or a `.ts.net` name, is dropped. Only https
  URLs are linked.
- **Integrity and cost:** manifests fetched by digest and config blobs must
  hash to their digest. Per-platform results are cached in memory by digest,
  since they're content-addressed, so a rebuild only refetches the tag list
  and the tag manifests.
- **Failure:** registry trouble never fails the build. The image is still
  listed with its pull command and a note that details aren't available.
  This differs from the API rule, because a stale packages page is worse
  than a short one.

### Phase 4: activity on the profile page

All of it is server-side HTML and SVG, built from data the build already
has: the last year's commits from the mirrors of the allowlisted repos, plus
releases, pulls and repo metadata from API calls already made. The one new
field read is the repo's `created_at` (and `merged_at` on pulls), from the
same responses. No new endpoints are called. Only repos in `config.json`
are in the data, so nothing else can appear.

- **Weekly bars** (replaced by the activity bar chart, see below) sit in the
  heatmap box, above the heatmap. The box head shows "active in N of the
  last 52 weeks" and the current streak: consecutive weeks with commits,
  ending this week, or last week while this week is still empty.
- **Layout (revised after review):** activity graph at the top, then the
  featured repos, then the activity feed. The separate "Recently active"
  row is gone.
- **Featured repos:** up to six cards in the middle, from `featured` in
  config (in order, each must be in `repos`), or the six most recently
  updated. "All N repositories →" links to `/repos/`, a new top-level page
  that lists every allowlisted repo. The server has one extra route for it,
  and `repos` is now a reserved repo name. A repo with commits in the last
  week gets a "N commits this week" pill and an accent edge (see Phase 5). Cards also carry
  the 12-week sparkline and the latest release tag.
- **Activity feed:** a text timeline, modelled on Tangled's and styled
  after the comment threads on danieldaum.net: no boxes, a dashed rule
  above, and a time column (UTC) that shows the time for today's events and
  the date ("Nov 19") otherwise, with the full time on hover. Day headers
  were tried and dropped to keep it short. Each event has an avatar (yours for your own events, a person icon
  for others) with a small badge icon, a one-line sentence, and nested
  lines on a 1px left rule, like saturn's comment replies. The feed covers
  the last 90 days, at most 6 events, in a compact type size (shortened twice after review):
  - "pushed N commits to `main` on repo": git doesn't record pushes, so
    commits are grouped per repo, author and day. Each shows the newest
    two as short hash (linked when it has a page) plus subject, then a
    "+N more" chip and "Commits →" to the log.
  - "created repository X", "published release `vX` on repo", and
    "opened" or "merged pull <title> on repo", one line each.
  The year's commits come from one `git log` pass per repo (hash, author,
  time, subject), which also feeds the heatmap and the bars. On phones the
  time moves above the sentence.
- **Counting rules match the heatmap's:** every commit on the default
  branch counts, whoever authored it, because the anonymous API hides
  emails. Pushes are approximated by commits, since Gitea's activity feed
  isn't used: it would need a new endpoint and covers private repos.
- The repo-a fixture gains 25 dated commits across five months (on
  `docs/notes.md`, four of them on one day), repo `created_at` dates, and a
  merged pull (#5), so the goldens and `just demo` show a real shape.

### Phase 5: more life on every page

No JS was added for any of it.

- **Freshness:** a still green dot marks a commit in the last 24 hours:
  on the "updated" pill in the repo header and on tree rows. A pulsing
  version was tried and dropped, since motion doesn't fit the static look.
  Repo cards with commits in the last 7 days show a plain "N commits this
  week" pill and an accent edge. An earlier "active"/"live" label said too
  little. Releases and commits under 7 days old get an accent "new" badge.
  The warm age colours stay, and they are mixes of `--syn-constant` and
  `--text-secondary`, both well over 4.5:1 on boxes and hovers.
- **Repo header:** stats are compact pills on every repo page: the latest
  release (an accent badge), commits, branches, tags, "updated … ago" in its
  freshness colour (or the live dot), and the CI result of HEAD linked to
  its checks. The code box no longer repeats branches, tags and commits.
  Its top bar keeps the branch chip and the clone menu. Website, licence
  and packages stay on the meta line, which is left out when empty.
- **Reuse key:** pages reused across builds (tree, blob, commit) show the
  header too, so the reuse key now includes the CI status and the newest
  commit's age bucket (days for the first week, then weeks, months,
  years). Those pages are rebuilt when the pill's text or the live dot
  would change. Within the first day the hours can lag on reused pages; the
  repo home and log are rebuilt every time.
- **Commit log:** commits are grouped by day (UTC) under sticky headers
  ("Today", "Yesterday", "Nov 14", with the year when it differs), one box
  per day. Each row has a coloured `+N −M` from one `git log --shortstat
  --diff-merges=first-parent` pass, matching the commit pages.
- **Hover and focus:** cards lift onto a lighter background with an accent
  edge, and asset rows highlight like other rows. Focus rings inside boxes
  sit inside the edge so they aren't clipped. The ring itself is the shared
  `--accent-bright` one from Phase 1.
- **Empty states** (no releases, pulls, packages or commits, an empty repo,
  no branches or tags) have a round icon, one line and a muted hint.
- **Motion:** hover transitions are 120–180 ms, and the clone menu chevron
  rotates. Saturn's reduced-motion rule turns all of it off.
- **404:** the pixel "404" in `--accent-bright`, links to the featured
  repos, and buttons back home and to all repositories.
- **Clone menu:** the code box no longer clips the menu (`overflow:
  visible`). Its first child and last file row take the rounded corners
  instead.
- The fixture's newest commit moves to the morning of the fixed build day,
  so the goldens and `just demo` show live dots, "Today" and "new".

## Activity bar chart

`render.ActivityBars` replaces the plain weekly bars above the heatmap. It
is one inline SVG (`class="bars"`, `role="img"`), with no JS and no inline
styles: every part has its own class and its colour comes from tokens.

- **Shared x axis.** The chart uses the heatmap's grid from `heatmap.go`
  (`left`, `cell`, `gap`, 53 columns, `colX`, `monthCols`) and the same first
  week, `WeekStart(end, 52)`. Both viewBoxes are `left + n*(cell+gap)` wide
  and drawn at `width: 100%; height: auto`, so each bar sits over its week
  at any width and the text isn't stretched. The bars are bucketed with
  `Weeks(times, now, 53)`. "Active in N of the last 52 weeks" and the streak
  still use the last 52.
- **Y axis.** The step is the smallest of 1, 2, 5, 10, 20, 50 … that gives
  2–3 lines (0 included) with the top line at or above the busiest week,
  for example 0/5/10 for a max of 6 and 0/10/20 for 15. Bars are linear on
  that scale, at least 4 units tall. Empty weeks are a 2-unit stub. Colour
  uses the heatmap's `level()` steps.
- **Markers, kept few:** this week (an `--accent-bright` outline and "this
  week" under the right end of the baseline) and the streak (a bracket in
  `--syn-constant` under exactly the weeks `Streak` counts, so it ends last
  week while this week is still empty). Month rules sit in the gap before
  each heatmap month label. The streak has no label of its own, because the
  box head already says "6-week streak". A "busiest · Oct 18 · 15" dot and
  label were dropped after review for the same reason (busy). The busiest
  week stays in the aria-label.
- **Releases are not marked.** A first version drew a dashed line, a
  diamond and the tag for each release, with labels staggered over two
  rows. It was dropped after review: the tags were detail nobody needed,
  and the label placement cost code. With one label left under the
  baseline, the chart is 118 units high.
- **Hover** detail is SVG `<title>`s: a full-height hit rect per column
  holds "12 commits · week of Oct 18 – Oct 24", and the bar after it has
  `pointer-events: none`, so `.bars-hit:hover + .bars-bar` brightens it and
  the column gets a faint background. 120ms fill transitions, none under
  reduced motion.
- **Empty year:** grid, month rules and stubs only, with "No commits in the
  last year" in the middle and no markers.
- **Small screens: two renders, toggled by CSS.** Below 600px the profile
  shows a second chart and heatmap drawn for the last 26 weeks with the same
  column logic, and hides the full-year pair (`.heat-year` / `.heat-half`).
  The aria-labels keep the full-year summary. A sideways scroller that
  starts at the right (an `rtl` container) was the other option. It keeps
  one copy, but scrolls the y-axis labels and weekday labels out of view,
  which is the part that makes the chart readable. Each pair sits in one
  container with equal viewBox widths, so the chart and heatmap can't fall
  out of line. The cost is HTML: the profile went from 58.9 KB to 92.1 KB
  (7.1 KB to 10.1 KB gzipped), most of it the second heatmap.

## Served at code.danieldaum.net

The site moves from `daum.dev` to `code.danieldaum.net`. Everything
host-specific already came from `site_url` (clone URLs, `go get` paths, image
refs, the registry token realm) except the brand in the header, which now
shows the host of `site_url` too. Go module paths follow the host:
`go get code.danieldaum.net/<repo>`. The fixtures and tests use the new host.
The spec still says `daum.dev` throughout; read it as `site_url`.

## Palettes shared with danieldaum.net, and a font review toggle

- **Why a cookie:** danieldaum.net keeps the palette in `sessionStorage` and
  `localStorage` under `theme`. Those are per origin, so
  `code.danieldaum.net` can't read them. A cookie set for
  `Domain=danieldaum.net` is visible to both. `/static/theme.js` reads a
  `theme` cookie first, then this site's own `sessionStorage`, and otherwise
  moves to the next palette after the last visit, like saturn's
  `theme.js`. It writes all three whenever it picks one. The cookie is a
  session cookie with `SameSite=Lax`, and `Secure` on https. On an IP or
  `localhost` it's host-only. The server never reads it.
- **Saturn's side** needs the same cookie read and write (about 10 lines in
  its `theme.js`). Until that ships, each site rotates palettes on its own.
- **Footer:** a `theme` button cycles moss → ink → wine → plum → teal, as on
  danieldaum.net (flexoki stays out of the cycle there too). The buttons
  have `hidden` and `theme.js` reveals them, since `[data-theme]` is always
  set here from config. Saturn shows its button on that attribute instead.
- **No flash:** `theme.js` loads in `<head>` without `defer`, so the palette
  is set before the first paint. It's same-origin, so the CSP is unchanged
  (`script-src 'self'`, nothing inline). Without JS, the page keeps the
  palette from config. The cache-busting `?v=` hash now covers `style.css`,
  `theme.js` and `copy.js`.
- **Font: Geist Pixel for all text,** as on danieldaum.net, chosen after a
  short review with a footer toggle (since removed). Code and hashes stay
  Geist Mono, because pixel type is proportional and code columns would
  drift. Geist Pixel is declared for every weight, as in saturn, so bold
  text isn't faked and emphasis comes from colour. The Geist font is gone.
  This reverses "Text is Geist" from the polish pass.
- **Width:** repo pages (`page--code`) are up to 60rem wide, down from
  68rem, and every other page (profile, repos, 404) is up to 50rem. Saturn's
  78ch is gone here: `ch` follows the font, and with Geist Pixel it grew to
  about 73rem.

## Gitea's icons again

Phosphor read worse than the Material icons the site had before, so the
icons are Gitea's own again: material-icon-theme for files and folders,
Octicons for the UI.

- **Files:** the same 153-icon subset of material-icon-theme as before
  Phase 3, plus `folder-open` for the current folder in breadcrumbs. It is
  npm `material-icon-theme@5.40.0`, which reproduces the old files and
  tables byte for byte. Lookup is the old one: the lowercased name, then
  extensions longest first (`a.test.ts` tries `test.ts`, then `ts`), and
  folders by name (`docs`, `cmd`, `.github` get their own folder icons).
  The icons keep their own colours, as on Gitea, so they don't follow
  `data-theme`. The `.r-*` colour roles are gone.
- **UI:** Octicons 16px from `@primer/octicons@19.15.0`, GitHub's set, which
  Gitea uses too. Templates keep their own names (`pull`, `fire`, `user`…),
  and `scripts/icons.py` maps them to Octicons (`git-pull-request`, `flame`,
  `person`…). Document tabs use GitHub's glyphs where it has one: Code of
  conduct `code-of-conduct`, Funding `heart`, Governance `organization`,
  Support `comment-discussion`, Citation `quote`. CI pending is `dot-fill`.
- **Still a per-page sprite**, not `<img>` as before Phase 3: icons are
  `<symbol>`s inlined into the pages that use them, so nothing is fetched
  and the CSP is unchanged. Material icons reuse ids (`a`, `folder`,
  `motive`) and refer to them (`url(#a)`, `xlink:href="#a"`). The loader
  prefixes every id inside an icon with its symbol id (`xf-kotlin-a`), so
  they can't clash on one page. A test checks there are no bare ids and no
  inline styles.
- **Root attributes:** six icons (document, readme, changelog, log,
  settings, just) set `fill="none"` on their `<svg>` to hide a 24×24
  background path, and editorconfig sets `clip-rule` there. The loader
  copies the root's presentation attributes onto the `<symbol>`. Without
  that, the background drew as a black square. The editorconfig logo
  itself is black, as on Gitea.
- `just icons <material-icon-theme dir> <octicons dir>` regenerates both
  sets from extracted npm packages. The repo home grew from 29.0 KB to 35.1 KB
  before compression, since Material paths are longer.

## User name case and avatar caching

- Gitea user names keep their case for display ("Daniel") but match in any
  case, in the API and in git URLs. Image refs must be lowercase, so
  `user` from config is lowercased when it loads. Clone paths, registry
  routes and `docker pull` refs use the lowercase name. The profile shows
  the login and full name as Gitea returns them.
- The avatar is downloaded on every build and saved as
  `/static/avatar-<hash>.<ext>`, a hash of its bytes. `/static/` is cached
  for a day, so a fixed name would keep showing the old avatar after a
  change.

## Responsive pass after Geist Pixel

Checked in headless Chromium at 1280px and 390px on the profile, repos,
repo home, file, log, commit, releases and packages pages. No page scrolls
sideways at either width.

- **Repo cards:** the name keeps its width and the "N commits this week"
  pill and release badge wrap under it, instead of the name shrinking to
  "repo…". The pill doesn't wrap inside itself.
- **Tabs:** saturn's `nav { flex-wrap: wrap }` made the repo tabs wrap into
  ragged rows on phones. They now scroll sideways, like Gitea's and GitHub's
  on mobile. Below 600px the repo tabs and the README box's document tabs
  fade out at the right edge to show there's more, with room to scroll the
  last tab clear of the fade.
- **Clone menu:** stays anchored to the right of the Clone button at every
  width, so it opens leftward and fits. The old small-screen rule anchored
  it left and pushed it off screen.
- **Commit rows:** below 600px the `+N −M`, CI and hash move to a second
  line under the subject, so subjects aren't cut to a few letters.
- **Avatar:** 160px, a rounded square (`--radius`) as on Gitea instead of a
  circle. Small avatars stay round.
- `just demo` used to 404 on the newest commit page: the golden test moved
  that page to `commit-latest.html`. It copies it now.

## The chrome is danieldaum.net's

The site is served at code.danieldaum.net and will move to
danieldaum.net/projects, so it must read as part of that site. The header,
footer and favicon are now exactly saturn's, hard-coded in `base.html`:

- **Header:** the name is "Daniel Daum", linking to danieldaum.net, and the
  nav is about, blog, projects, garden. The three main-site links are
  absolute (a relative `/about/` would 404 on code.danieldaum.net); the
  "projects" link points at `/` (this site's home) and carries
  `aria-current="page"`. When the site moves under danieldaum.net/projects,
  the absolute links stay right, but the internal ones need a base path.
- **Footer:** saturn's, verbatim: email, git (Tangled, new tab), PGP,
  changelog, privacy, feed, theme button. All absolute, since the pages live
  on danieldaum.net. The mobile hide-rules match the path
  (`a[href$="/pgp.asc"]`) because the urls aren't bare paths, as in saturn.
- **Profile page:** the profile card (avatar, name, bio) is gone. The home
  page opens with saturn's /projects page header (`/projects` and
  "Things I've built or projects in development"), the same meta description
  and the title "daniel daum - projects". The avatar is still downloaded: the
  activity feed uses it for own events.
- **Favicon:** saturn's favicon pngs and webmanifest (icon paths rewritten to
  `/static/favicon/`) live in `web/static/favicon/`. The server serves
  `.webmanifest` as `application/manifest`.
- **Config:** the `links` map is gone. The chrome is fixed, not configured.
  `site_url` still drives clone urls, `go get` paths and image refs.
- **Width and background:** `body` is saturn's again: `78ch` wide (about
  73rem in Geist Pixel at 16px), 16px, `--background-dark`. With the old
  50rem and 60rem widths the header nav and footer jumped sideways when
  coming from danieldaum.net, and the darker grey page background showed
  too. The code browser's own 15px type now starts at `main`. Every page is
  the same width, so `Page.Wide` and `page--code` are gone. This replaces
  the "Width" bullet under "Palettes shared with danieldaum.net".
- **Layers:** `--page` is `--background-dark`. The other layers mix it with
  white (box 2%, box head 5%, hover 8%, rule 14%, edge 20%, line 24%), close
  to the old Paint lightness steps above the page, keeping the palette's
  hue. The contrast test passes in every palette (lowest: secondary text on
  hover, 5.21:1).
- **Titles and head:** every title is `daniel daum - <page>`, as on saturn
  ("daniel daum - repo-a", "daniel daum - README.md · repo-a"). Every page
  links saturn's Atom feed, and pages with a description (the home page)
  carry saturn's Open Graph and Twitter card tags.
- **Breadcrumbs:** repo pages and `/repos/` open with saturn's breadcrumb
  (`projects -> repo-a`), like saturn's project and tracker pages. It keeps
  saturn's capitals. The repo title is just the name: the owner, avatar and
  slash are gone. `/repos/` uses saturn's page header (`/repositories`).
  The in-repo path crumbs stay as they were.
- **404:** saturn's wording, "this page doesn't exist", and its title.
- **Activity feed:** one rule only. The line through the avatars and the
  commit list's left rule drew two parallel lines. The commit rule stays,
  as on saturn's comment replies.
- **Palette:** saturn's `theme.js` still needs the cookie (see "Palettes
  shared with danieldaum.net"). Until then the two sites can show
  different palettes, which is the most visible seam left.

## README HTML: an allowlist

GitHub-style READMEs open with raw HTML (`<p align="center"><img …></p>`,
`<h1 align="center">`). With goldmark's raw HTML switched off, rhea's
screenshot, title and tagline were dropped. This bends spec rule 5: raw HTML
is still never passed through, but `internal/render/rawhtml.go` rebuilds an
allowlist of it.

- **Tags:** p, div, h1–h6, img, a, br, hr, b, i, em, strong, code, kbd, sub,
  sup, s, del, pre, blockquote, ul, ol, li, details, summary. Others are
  dropped and their text kept, except script-like tags (script, style,
  iframe, svg, textarea …), whose contents go too. Comments are dropped.
- **Attributes:** `align` (left, center, right) on blocks and img; img
  `src`, `alt`, `title`, `width`, `height` (digits, optional `%`); a `href`,
  `title`; details `open`. Everything else goes, so no `style`, `class`,
  `id` or `on*`, and the CSP is unchanged.
- **URLs:** `src` and `href` go through the same resolver as Markdown
  images and links (relative paths to `raw/` and `blob/`, images relative
  or https only). An href must be relative, http(s) or mailto. An image
  without a usable src becomes its alt text, like a dropped Markdown image.
- **Text** is unescaped and re-escaped, so nothing in it can form a tag.
- **Balance:** open tags are tracked over the whole README, so a
  `<div align="center">` … `</div>` around Markdown works. A close tag that
  was never opened is ignored, so README HTML can't end the page's own
  elements, and whatever is still open is closed at the end.
- No new dependency: the tags are read with two regular expressions, which
  is enough because the output is built from scratch, never copied.

## Release 0.1.0: name, deploy folder, base path

- **Name:** the project is "showcase". The binary, `cmd/showcase`, the
  compose service and the image (`git.danieldaum.net/daniel/showcase`) use
  it. The Go module path stays `daum.dev/showcase`, since nothing imports
  it from outside.
- **Deploy folder:** `deploy/` holds the production `config.json` (no
  secrets: the builder is anonymous), `compose.yaml`, the saturn patch and
  the runbook. The root `config.json` stays gitignored, for `just run`.
- **Images:** `just release <version>` builds linux/amd64 and linux/arm64
  with buildx and pushes `<version>` and `latest`. It refuses a dirty tree
  or a HEAD not tagged `v<version>`. The Dockerfile's build stage runs on
  `$BUILDPLATFORM` and cross-compiles, so only the small runtime stage is
  emulated. Both base digests are multi-arch indexes.
- **Mirrors follow `gitea_url`:** `gitx.Mirror` resets `origin` to the
  configured URL before each fetch. Before, a mirror kept fetching the
  address it was cloned from, so moving Gitea to the tailnet would have
  broken every repo until the volume was wiped.
- **`base_path`** (`/projects`, or empty for the root) serves the pages under
  one path segment, for danieldaum.net/projects through saturn's Pages
  Function.
  - Links: templates and the builder still write root-relative links. One
    pass in `render.Write` prefixes every root-relative `href` and `src`
    (`/repo-a/` → `/projects/repo-a/`), README links included, so no link
    can be missed. html/template always quotes attributes with `"` and
    escapes quotes in text and values, so only real attributes match.
    `//host` links are left alone. `TestBasePathLinks` fails if any page
    links outside the base.
  - Server: paths under the base are served with it stripped. Anything
    else redirects into the base (`/repo-a/` → `/projects/repo-a/`), so
    old links keep working. Clone, `docker pull`, release downloads, `go get`
    (`?go-get=1`) and `/healthz` stay at the root: module paths and image
    refs can't move, and Pages Functions only forward GET and HEAD.
  - Release downloads link to `site_url` when a base is set, since
    danieldaum.net doesn't forward them.
  - The webmanifest's paths are relative, so they work under either.
  - Validation: one lowercase segment, not a reserved name or a repo name.
- **Saturn side** (`deploy/saturn/saturn.patch`): `theme.js` shares the
  palette through the `theme` cookie, `social/functions/projects/` redirects
  `/projects/*` to code.danieldaum.net (`MODE = "redirect"`) or proxies it
  (`"proxy"`), `_routes.json` adds `/projects`, and `isLikeable` drops
  `/projects/`. The proxy forwards GET and HEAD only, to a fixed origin,
  without cookies, and returns upstream redirects as they are. Showcase's
  security headers pass through; saturn's `_headers` don't apply to
  Function responses. Tested with Node in front of a `base_path` build:
  all 66 pages and their links work through it.

## Language bar, tree rules and `.env` pages

- **Language bar:** the bar shows Gitea's numbers as they are. Gitea reads
  linguist attributes, so wrong stats get fixed in the repo being shown,
  not in showcase. This repo's `.gitattributes` marks `gitea/` (copied
  Gitea themes) and `testdata/` (goldens) as vendored and `icons_gen.go`
  as generated. Before that, the themes made showcase read as mostly CSS.
  Gitea recounts on the next push.
- **File tree rules:** the commit message fade (`mask-image`) sits on the
  link inside `.f-msg`, not on the cell. A mask on the cell faded its
  top border too, so each row's rule broke off under the message column.
- **`.env` pages:** `/<repo>/blob/.env` renders and is served like any
  other file. In production, the reverse proxy in front of the container
  answers every path ending in `/.env` with an empty 403 (no showcase
  headers, missing files too), and browsers show that as a blank page. It
  looks like a WAF rule against `.env` leaks (for example CrowdSec's
  `vpatch-env-access`). Fix it there with an exception for this host,
  not by renaming routes in showcase.

## Issues and comments

The spec lists issues and comments as non-goals. Daniel asked for both, so
they're in, read-only like pulls.

- **Issues tab** (`/<repo>/issues/`), between Releases and Pulls: the 50
  newest issues (`/issues?state=all&type=issues`; the issues API has no sort
  option). Gitea's issues API returns pulls too, so anything with
  `pull_request` set is dropped. The heading counts open and closed issues;
  labels are plain pills, since the CSP allows no inline colors.
- **Comments** on issues and pulls, oldest first, from
  `/issues/{n}/comments`, fetched only when the item's `comments` count is
  above zero. That endpoint only returns real comments, not timeline
  events. Bodies come from anyone, so they go through the same Markdown
  allowlist as READMEs; the fixture's comment carries a `<script>` and a
  `javascript:` link to prove it.
- **Public only:** the calls are anonymous, so only issues of public repos
  are seen. Gitea has no confidential issues within a public repo.
- **Read-only:** the page says issues can't be opened or answered here.
- **Activity:** opened issues join the profile feed.
- Icons: Octicons `issue-opened`, `issue-closed` and `comment` (19.15.0),
  added to `scripts/icons.py`.

## A Gitea token, for the packages API only

Security rule 1 says the generator uses no token. Gitea 28 wants one to
list packages, and non-container packages are coming, so there's now one
narrow exception. Nothing uses it until `gitea_token_file` is set.

- **Config:** `gitea_token_file` names a file holding the token (a Docker
  secret or a read-only mount), so config.json stays free of secrets. The
  token can't be set in config.json itself, and it's never marshalled.
- **One endpoint:** `gitea.Client` sends the token only when a call asks for
  it (only `Packages` does) and the path is the user's package listing
  (`/api/v1/packages/<user>?…`). Any other path asking for it is refused
  before a request goes out. Repos, issues, pulls, comments, avatars,
  mirrors and the git and registry proxies stay anonymous: they never see
  the token. Add more endpoints to `tokenPath` one at a time.
- **No redirects:** the client already refuses them, so the token is never
  replayed elsewhere.
- **Filter:** with a token, the listing can include private packages. Only
  packages linked to a repo that loaded anonymously, and not private, are
  kept. The builder passes those repo names, not the config list, so a
  private repo in config never lets its packages through.
- **Not yet:** packages linked to no repo are still dropped, and downloads
  still need a route. Both come with the packages work.
- Tests: `internal/gitea` proves the header only reaches the package
  listing, never follows a redirect, and is absent without a token;
  `internal/config` covers the file handling.

## Tab bars that fit

With issues added, a repo using everything overflowed both tab rows on
desktop. The content column is 631px (78ch of Geist Pixel at 16px, less
padding). Measured from the font's glyph advances: the repo tabs needed
772px (808px with four-digit counts), the README box's tabs 716px for
repo-a's six documents and 1,202px for all eleven kinds.

- **Commits tab, without its count.** The header's "N commits" pill
  already shows the number. Tabs are a little tighter (0.85rem, 0.45rem
  side padding, no gap, smaller count pills): 592px for repo-a, 610px with
  large counts (issues and pulls are capped at 50). Only the current tab
  is bold, which adds a few pixels at most.
- **Compact document tabs.** Up to four documents keep their labels (the
  four longest take 516px). From five on, only the open tab shows its
  label; the others are icons with a `title`, and the label stays in the
  page for screen readers. All eleven take 496px, so they never scroll on
  desktop. On hover a tab's icon lights up and a faint underline shows, so
  icon-only tabs read as clickable. Phones still scroll both rows with the
  edge fade.

## Second security review

Details in `docs/security.md`. The decisions: the proxy forwards an
allowlist of request headers instead of stripping a denylist; git proxying
is capped at 8 concurrent requests (503 beyond); registry blobs are always
downloads; issue, pull and comment bodies are cut at 32 KB and threads show
their newest 100 comments.

## A quieter code bar

On an iPhone the repo tabs overlapped: Safari shrank the scrolling tabs
below their content, so each icon sat on the previous word. Even where they
fit, six icons, six labels and four count pills under a wrapping language
legend made the top of the code box busy. This replaces "Tab bars that fit"
for the repo tabs; the README box's document tabs are unchanged.

- **Plain words.** The repo tabs drop their icons and read like the site
  nav above them: secondary text, the open tab in `--accent-bright` with an
  underline as wide as the word, 1.5rem apart (1.25rem on phones). Counts
  are small secondary numerals, not pills. `flex: none` on each tab stops
  Safari's squeeze.
- **Only tabs with something in them.** Code and Commits always show.
  Releases, Issues, Pulls and Packages show when they have at least one
  item, or when they're the open page (an old link to an empty page still
  says where you are). The pages themselves still exist.
- **Three languages and "other".** `langBar` keeps the three largest
  languages instead of six; the rest join "other", in the legend and the
  strip. On phones the legend is 0.75rem with 0.75rem gaps.
- **Widths** (Geist Pixel advances, as before): all six tabs with counts
  take 448px of the 631px column. On a 390px phone (358px of content) the
  showcase repo's three tabs take 202px; all six take 428px and scroll with
  the edge fade. The legend for Go, CSS, HTML and other takes 335px on a
  phone, so it stays on one line down to 375px screens.
- Not checked in a real browser in this pass: the numbers come from the
  font's metrics, not a render.

## Images built by Gitea Actions

`.gitea/workflows/release.yml` builds and pushes the image when a tag
`v*.*.*` is pushed, then creates the Gitea release. Tagging and deploying
stay manual: the server runs the digest pinned in `deploy/compose.yaml`,
never `latest`, so nothing reaches it that Daniel didn't pin.

- **amd64 only.** The server is amd64. The image used to be built for
  arm64 too, because `just release` ran on the Mac (Apple silicon). That
  needed QEMU: Go cross-compiles natively, but the runtime stage's
  `apk add` runs as the target arch. Nothing runs the arm64 image, so it
  and QEMU are gone, here and in `just release`. Locally there's
  `just image` and `just demo`.
- **Plain docker commands**, no actions beyond checkout (pinned to a
  commit). The newest Docker actions need Node 24, and the runner was only
  shown to run Node 20 (`actions/checkout@v4`). The digest comes from
  `docker push`'s output, which every image store prints, rather than
  `docker inspect`.
- **Tokens:** only `REGISTRY_TOKEN` is made by hand (a Gitea token with
  `write:package`), since Gitea's run token isn't known to push packages.
  The release is written with the run token, `GITEA_TOKEN`. Both reach
  the scripts as environment variables, and the tag name is read from
  `$GITHUB_REF_NAME`, so nothing is interpolated into a script.
- **Registry host:** Gitea moved to the tailnet
  (`code.aardvark-halibut.ts.net`), and `git.danieldaum.net/v2/` now
  answers 404. The workflow, the justfile, compose.yaml and the docs name
  the tailnet host. The pinned 0.1.0 digest is the same there (checked
  with crane).
  - Not `$GITHUB_SERVER_URL`: that's the runner's address for Gitea,
    `http://gitea:3000` (a Docker network name), which works inside the
    job container but not for the push. `docker push` is done by the
    Docker daemon on the runner's host, which resolves names like the
    host does. So that host must resolve and reach the tailnet name.
    The Gitea API call (the release) does run in the job container and
    keeps using `$GITHUB_SERVER_URL`.
- **Manual runs:** `workflow_dispatch` takes an existing tag. The
  workflow comes from the branch it runs on and checks out
  `refs/tags/<tag>`, so a fixed workflow can release a tag whose own run
  failed. The tag must look like `v1.2.3`; anything else stops the run.
- **Release notes:** the image with its digest (to pin in compose.yaml)
  and the commit subjects since the previous tag. A rerun rebuilds the
  image with a new digest, so an existing release is updated (PATCH)
  instead of created.
- **Not covered:** tests (`just test` runs before committing) and the
  tag's signature. A signature check was tried and dropped: the workflow
  comes from the tagged commit, so it only stops tags made in Gitea's UI,
  and the digest pin already guards the server.

## Signed commits

Signed commits get Gitea's lock badge: the short hash gets a green edge and
a segment with a lock and the signer's avatar when Gitea verified the
signature, or an open lock when it's signed but not verified. Unsigned
commits look as before.

- **Gitea's verdict, not git's.** One more anonymous call per repo,
  `/commits?sha=<HEAD>&limit=50&stat=false&files=false&verification=true`,
  paged up to `max_commits`. Gitea checks the signature against the keys
  of the user whose verified email committed it, so the badge matches
  Gitea's own. Checking locally would need the keys in an allowed-signers
  file, `ssh-keygen` and `gpg` in the image, and Gitea's email matching
  rules again.
- **What's kept:** verified or not, the signer's Gitea name and the key
  (SSH fingerprint or GPG key ID, read from the `reason`, and only when it
  has that exact form). The signer's email, the signature and the payload
  are never decoded. The avatar shows only when the signer is the profile's
  user; other signers get the lock alone. The API has no trust status, so
  Gitea's yellow "untrusted" and red "warning" states aren't shown.
- **Where:** every short hash of a commit on the log page (log rows, the
  repo's latest commit bar, the commit page, branches and tags). The commit
  page adds Gitea's signature bar: "Verified · signed by daniel with SSH key
  `SHA256:…`", or "Signed · Gitea couldn't verify the signature". The
  activity feed shows a small lock after verified hashes. Parent hashes
  stay plain, as on Gitea. Branches and tags pointing at older commits show
  no badge, since only the log page's commits are looked up.
- **Colour:** `--add`, the CI pass green. `--sig` (12% of it over `--box`)
  is the badge segment and the bar; `--sig-edge` the border. The contrast
  test covers text on `--sig` (lowest: secondary text, 4.94:1).
- **Failure:** 404/401/403 means no badges. Any other error fails the
  build, like the CI status call. Empty repos skip the call (Gitea answers
  409).
- **Reuse key:** includes the verdicts, so commit pages are rebuilt when a
  key is added in Gitea and old commits turn verified.
- Icons: Octicons `lock` and `unlock` (19.15.0), added to `scripts/icons.py`.

## Copy a file

File pages get a copy button joined to Raw (`[Raw | ⧉]`), like GitHub's.

- **It copies the raw file.** `copy.js` fetches the Raw link's URL and puts
  the bytes on the clipboard, so CRLF, mixed line endings and files too
  big to show (over 512 KB) all copy exactly. That needs `connect-src
  'self'` in the CSP: the third change to spec rule 6, after `script-src
  'self'`. It only lets this site's own scripts (`theme.js`, `copy.js`)
  request this origin. It adds no way out: an injected script could already
  send data anywhere through `img-src https:`, and the site holds nothing
  private. Under `base_path` the origin is danieldaum.net and the Raw link
  is already rebased, so it works there too.
- **Safari** only allows clipboard writes during the click, so the
  download is handed to `ClipboardItem` as a promise. Browsers without
  `ClipboardItem` get `writeText` after the download.
- **Which files:** text with a raw copy, up to 5 MB (`maxCopy`). Images,
  binary files and files over 5 MB keep Raw alone.
- **Trailing blank lines:** chroma drops the empty line after a final
  newline, so a file ending in blank lines showed one line too few.
  `Highlight` adds it back, and `TestHighlightKeepsText` checks the lines
  give back the source.
- **Hidden without JS, for real now.** `.copy` and `.button` set `display`,
  which beat the `hidden` attribute, so the clone box's copy button showed
  without JS too, contrary to "Copy buttons need JS". `[hidden]` rules for
  both fix it.

## This week on the bar chart, clearer

Production showed "1-week streak" with no bracket under the bars, and this
week's bar looked like any other busy week. Both replace parts of
"Markers, kept few" under "Activity bar chart".

- **The streak bracket is drawn from one week,** not two, so it always
  matches the streak in the box head. A one-week bracket is a short
  underline under that week's bar.
- **This week's outline is `--text-headings`,** not `--accent-bright`. The
  busiest weeks are filled with `--accent`, and `--accent-bright` (the
  accent with 18% white) barely showed on them.
