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
- **Font review (temporary):** a `font` button sets `data-font="pixel"` on
  `<html>`, which points `--ui` at Geist Pixel. It's remembered in
  `localStorage`. Code stays Geist Mono. Remove the button, its CSS and the
  font part of `theme.js` once a font is chosen.
- **Width:** repo pages (`page--code`) are up to 60rem wide, down from
  68rem. The profile keeps saturn's 78ch.

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
- `just icons <material-icon-theme dir> <octicons dir>` regenerates both
  sets from extracted npm packages. The repo home grew from 29.0 KB to 35.1 KB
  before compression, since Material paths are longer.
