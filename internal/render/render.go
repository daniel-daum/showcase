// Package render turns data into static HTML files.
package render

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"daum.dev/showcase/web"
)

type Renderer struct {
	Now   time.Time // fixed per build so output is deterministic
	css   string    // short hash of style.css, so caches never serve a stale one
	Theme string    // saturn palette name, the default without JS
	Base  string    // base_path ("/projects"), put in front of every root-relative link
	pages map[string]*template.Template
	syms  map[string][]byte // sprite symbols by id: f-<glyph> file icons, i-<name> UI icons
}

// Page is what every template receives.
type Page struct {
	Title    string
	Meta     string // optional meta description
	GoImport string // optional go-import meta content
	D        any

	Theme string // filled in by Write
	CSS   string
}

func New(now time.Time) (*Renderer, error) {
	h := sha256.New() // one version for the stylesheet and both scripts
	for _, f := range []string{"static/style.css", "static/theme.js", "static/copy.js"} {
		b, err := web.FS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		h.Write(b)
	}
	r := &Renderer{Now: now, css: hex.EncodeToString(h.Sum(nil)[:5]), pages: map[string]*template.Template{}}
	var err error
	if r.syms, err = loadSymbols(); err != nil {
		return nil, err
	}
	funcs := template.FuncMap{
		"ago":   r.ago,
		"fresh": r.fresh,
		"live":  func(t time.Time) bool { return !t.IsZero() && r.Now.Sub(t) < 24*time.Hour },
		"isnew": func(t time.Time) bool { return !t.IsZero() && r.Now.Sub(t) < 7*24*time.Hour },
		"ficon": fileIcon,
		"dec":   func(i int) int { return i - 1 },
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[kv[i].(string)] = kv[i+1]
			}
			return m
		},
		"trimScheme": func(u string) string {
			return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"), "/")
		},
		"pct":  func(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) },
		"date": func(t time.Time) string { return t.UTC().Format("2006-01-02") },
		"lang": langClass,
		"size": humanSize,
		"inc":  func(i int) int { return i + 1 },
		"day":  func(t time.Time) string { return t.UTC().Format("02 Jan 06") },
		"diff": diffClass,
	}
	names, err := fs.Glob(web.FS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if path.Base(n) == "base.html" || strings.HasPrefix(path.Base(n), "_") {
			continue
		}
		// base.html and _*.html partials are shared by every page.
		t, err := template.New("").Funcs(funcs).ParseFS(web.FS, "templates/base.html", "templates/_*.html", n)
		if err != nil {
			return nil, err
		}
		r.pages[strings.TrimSuffix(path.Base(n), ".html")] = t
	}
	return r, nil
}

// Write renders template name into dir/rel.
func (r *Renderer) Write(dir, rel, name string, p Page) error {
	t, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("render: no template %q", name)
	}
	p.Theme, p.CSS = r.Theme, r.css
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", p); err != nil {
		return fmt.Errorf("render %s: %w", rel, err)
	}
	return WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), r.rebase(r.fillSprite(buf.Bytes())))
}

// rootLink matches a root-relative href or src: html/template always quotes
// attributes with ", and quotes inside text and values are escaped, so this
// only finds real attributes. "//host" links are left alone.
var rootLink = regexp.MustCompile(`(\s(?:href|src)=")/([^/])`)

// rebase moves every root-relative link under r.Base. Templates and the
// builder write links from the site root; this one pass keeps them all,
// README links included, right when the pages live under a path.
func (r *Renderer) rebase(b []byte) []byte {
	if r.Base == "" {
		return b
	}
	return rootLink.ReplaceAll(b, []byte("${1}"+r.Base+"/${2}"))
}

// WriteFile writes b to name, creating parent directories.
func WriteFile(name string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, b, 0o644)
}

// CopyStatic copies the embedded static assets into dir/static.
func CopyStatic(dir string) error {
	return fs.WalkDir(web.FS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(p, "static/icons/") { // inlined into pages, never fetched
			return nil
		}
		b, err := web.FS.ReadFile(p)
		if err != nil {
			return err
		}
		return WriteFile(filepath.Join(dir, filepath.FromSlash(p)), b)
	})
}

func (r *Renderer) ago(t time.Time) string {
	d := r.Now.Sub(t)
	days := int(d.Hours() / 24)
	switch {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case days < 7:
		return plural(days, "day")
	case days < 30:
		return plural(days/7, "week")
	case days < 365:
		return plural(days/30, "month")
	}
	return plural(days/365, "year")
}

// fresh classes an age so recent changes stand out: within a day, a week
// or a month.
func (r *Renderer) fresh(t time.Time) string {
	switch d := r.Now.Sub(t); {
	case t.IsZero():
		return ""
	case d < 24*time.Hour:
		return "fresh-1"
	case d < 7*24*time.Hour:
		return "fresh-2"
	case d < 30*24*time.Hour:
		return "fresh-3"
	}
	return ""
}

func plural(n int, unit string) string {
	switch {
	case n == 1 && (unit == "week" || unit == "month" || unit == "year"):
		return "last " + unit
	case n != 1:
		unit += "s"
	}
	return fmt.Sprintf("%d %s ago", n, unit)
}

// langColors gives common languages fixed accent slots, chosen so languages
// that often share a repo get different colours. Others hash into a slot.
var langColors = map[string]int{
	"CSS": 0, "Kotlin": 0, "Elixir": 0,
	"Go": 1, "TypeScript": 1, "Lua": 1,
	"C": 2, "Nix": 2, "Dockerfile": 2, "Vue": 2,
	"JavaScript": 3, "Python": 3, "Svelte": 3,
	"HTML": 4, "Ruby": 4, "Objective-C": 4,
	"Swift": 5, "Rust": 5, "Java": 5, "C++": 5,
	"Shell": 6, "Templ": 6, "PHP": 6,
	"Makefile": 7, "Just": 7, "Markdown": 7, "SCSS": 7,
}

// langClass maps a language name to one of eight accent colour classes.
func langClass(name string) string {
	if i, ok := langColors[name]; ok {
		return fmt.Sprintf("lang-%d", i)
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("lang-%d", h.Sum32()%8)
}

func humanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

// diffClass maps a diff line kind to a CSS class; +/- reuse saturn's roles.
func diffClass(kind byte) string {
	switch kind {
	case '+':
		return "hl-added"
	case '-':
		return "hl-removed"
	case '@':
		return "hunk"
	}
	return ""
}

// fileIcon picks a material-icon-theme icon (see scripts/icons.py) by file
// or folder name, then by extension, longest first ("test.ts" before "ts").
// The current folder in breadcrumbs ("tree-open") gets the open folder.
func fileIcon(name, typ string) template.HTML {
	n, icon := strings.ToLower(name), "file"
	switch {
	case typ == "tree-open":
		icon = "folder-open"
	case typ == "tree" || typ == "commit":
		icon = cmp.Or(iconFolders[n], "folder")
	case iconNames[n] != "":
		icon = iconNames[n]
	default:
		for _, rest, ok := strings.Cut(n, "."); ok; _, rest, ok = strings.Cut(rest, ".") {
			if i := iconExts[rest]; i != "" {
				icon = i
				break
			}
		}
	}
	return template.HTML(`<svg class="ficon" width="16" height="16" aria-hidden="true" focusable="false"><use href="#f-` + icon + `"></use></svg>`)
}

// The page sprite: base.html emits it empty, and Write fills in the symbols
// the page refers to, so each page carries only the icons it shows. Symbols
// are inline because the CSP blocks data: URLs and external sprites.
const spriteTag = `<svg class="sprite" width="0" height="0" aria-hidden="true" focusable="false">`

var (
	useRe      = regexp.MustCompile(`href="#([fi]-[a-z0-9-]+)"`)
	viewBoxRe  = regexp.MustCompile(`viewBox="([0-9 .-]+)"`)
	localIDRe  = regexp.MustCompile(`(id="|href="#|url\(#)([A-Za-z][\w-]*)`) // ids inside one icon
	rootAttrRe = regexp.MustCompile(`\s([a-z-]+)="([^"]*)"`)                 // fill="none" and such on <svg>
)

func (r *Renderer) fillSprite(page []byte) []byte {
	var syms []byte
	seen := map[string]bool{}
	for _, m := range useRe.FindAllSubmatch(page, -1) {
		if id := string(m[1]); !seen[id] {
			seen[id] = true
			syms = append(syms, r.syms[id]...) // unknown ids (a README's own #f-x link) add nothing
		}
	}
	return bytes.Replace(page, []byte(spriteTag), append([]byte(spriteTag), syms...), 1)
}

// loadSymbols turns the embedded icons into <symbol>s: Material file icons
// (f-) and Octicons (i-). Ids inside an icon get its symbol id as a prefix,
// since many icons reuse the same ids and they all share one page.
func loadSymbols() (map[string][]byte, error) {
	syms := map[string][]byte{}
	for prefix, dir := range map[string]string{"f-": "static/icons/material", "i-": "static/icons/octicons"} {
		files, err := fs.Glob(web.FS, dir+"/*.svg")
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			b, err := web.FS.ReadFile(f)
			if err != nil {
				return nil, err
			}
			vb := viewBoxRe.FindSubmatch(b)
			start, end := bytes.IndexByte(b, '>'), bytes.LastIndex(b, []byte("</svg>"))
			if vb == nil || start < 0 || end < start {
				return nil, fmt.Errorf("render: can't read icon %s", f)
			}
			id := prefix + strings.TrimSuffix(path.Base(f), ".svg")
			// the root's presentation attributes move to the symbol, so its
			// content still inherits them (fill="none" hides a background)
			var attrs []byte
			for _, a := range rootAttrRe.FindAllSubmatch(b[:start], -1) {
				if k := string(a[1]); k != "viewbox" && k != "width" && k != "height" && k != "xmlns" {
					attrs = fmt.Appendf(attrs, ` %s="%s"`, k, a[2])
				}
			}
			body := localIDRe.ReplaceAll(b[start+1:end], []byte("${1}x"+id+"-${2}"))
			syms[id] = fmt.Appendf(nil, `<symbol id="%s" viewBox="%s"%s>%s</symbol>`, id, vb[1], attrs, body)
		}
	}
	return syms, nil
}
