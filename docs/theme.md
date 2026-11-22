# Theme

daum.dev should look like part of danieldaum.net (the `saturn` repo). Saturn
is the source of truth. This site copies its tokens and components and adds
only what a code browser needs on top.

## What carries over from saturn

- **Dark only.** `color-scheme: dark`, no light mode, no `prefers-color-scheme`
  switch.
- **Tokens** are copied as-is: `--background-dark`, `--surface`, `--borders`,
  `--text-headings`, `--text-body`, `--text-secondary`, `--accent`,
  `--accent-muted`, and the `--syn-*` syntax roles.
- **Palettes:** moss (default), ink, wine, plum, teal and flexoki, each under
  `[data-theme="…"]`. Saturn picks one per visit with `theme.js`. There's no
  JS here, so the build sets `data-theme` on `<html>` from config (see
  Palette choice below).
- **Fonts:** Geist Pixel Square for all text, and Geist Mono (regular and
  italic) for `pre code` only, because pixel type is proportional and code
  columns would drift. The woff2 files and OFL licence go in
  `web/static/fonts/`.
- **Base:** Josh Comeau reset, 78ch body width, the heading scale, text links
  underlined in `--accent-muted` that turn `--accent` on hover, and the focus
  ring, high-contrast and reduced-motion rules.
- **Chrome:** `.site-header` with `.site-name` (daum.dev) and a nav, and a
  footer nav that starts with `//` and holds links to danieldaum.net, Tangled,
  GitHub and Bluesky. UI labels are lowercase like saturn's.

## Saturn components → daum.dev pages

| Saturn component                      | Used for                                               |
| ------------------------------------- | ------------------------------------------------------ |
| `.page-header`, `.page-header--home`  | profile hero (name in accent), repo header             |
| `.meta`, `.meta-split`, `.meta-sep`   | repo meta line, latest commit, file size and line count |
| `.pills` / `.pill`                    | topics, "archived", release tag, PR state              |
| `.card-grid` / `.card`                | repo list on the profile                               |
| `.section-label`                      | section headings: "repositories", "readme", "files"   |
| `.breadcrumb`                         | `repo / dir / file` on tree and blob pages             |
| `.table` / `.table-row`               | file tree, pulls, packages                             |
| `.commit-log` (+ `-row`, `-hash`, `-msg`, `-date`) | latest commit box, the log page       |
| `.version-list` / `.version-meta`     | releases page                                          |
| `.code-block` + `.code-label`         | file viewer (label: path · size · lines), diff hunks   |
| `.hl-*` classes                       | syntax highlighting (see below)                        |

New for daum.dev, built from the same tokens:

- **Tabs** (code · commits · releases · pulls · packages): styled like saturn's
  header nav, with `aria-current` shown in `--accent`.
- **Clone box:** a read-only input on `--surface` with a `--borders` border.
- **Heatmap:** `h0` uses `--surface`. `h1`–`h4` step from `--accent-muted` to
  `--accent` with `color-mix()`, so they follow the palette.
- **Language dots:** eight classes mapped to the `--syn-*` colours, so they
  follow the palette too.
- **Code pages:** a wider `body.page--code` (about `min(72rem, 94vw)`), like
  saturn's `page--wide`. The 78ch width is too narrow for code and diffs.
- **Line numbers:** links (`#L12`) in `--text-secondary`. The `:target` line
  is highlighted with `--surface` and an `--accent-muted` edge.

## Syntax highlighting

Saturn highlights at build time with Shiki and emits `hl-<role>` classes:
comment, keyword, function/link, builtin/escape, type/self/changed,
variable/heading/removed, constant, string/added and punct. Here chroma does
the tokenising, and a small Go formatter maps chroma token types to the same
`hl-*` roles. Saturn's CSS then applies unchanged, every palette's `--syn-*`
colours work, and there are no inline styles, so the CSP holds. Diffs use
`hl-added`, `hl-removed` and `hl-changed`. README code blocks go through the
same formatter.

## Keeping in sync

`web/static/style.css` holds two sections. The first is copied from saturn's
`global.css` (only the parts used) and marked with its source. The second is
daum.dev-only. When saturn's tokens or components change, re-copy the first
section and leave the second alone.

## Palette choice

Saturn's no-JS default is moss. Options here, all JS-free:

1. Moss everywhere (`"theme": "moss"` in config).
2. Config sets a site default, and each repo may pick its own.
3. Each repo gets a palette from a hash of its name.

For now it's option 1: `"theme"` in config sets the palette for the whole
site (default `moss`). Options 2 and 3 can build on that later.
