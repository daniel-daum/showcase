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
	Theme string    // saturn palette name
	Links []Link    // footer links
	pages map[string]*template.Template
	syms  map[string][]byte // sprite symbols by id: f-<glyph> file icons, i-<name> UI icons
}

type Link struct{ Name, URL string }

// Page is what every template receives.
type Page struct {
	Title    string
	GoImport string // optional go-import meta content
	Wide     bool   // code layout (page--code)
	D        any

	Theme string // filled in by Write
	Links []Link
	CSS   string
}

func New(now time.Time) (*Renderer, error) {
	css, err := web.FS.ReadFile("static/style.css")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(css)
	r := &Renderer{Now: now, css: hex.EncodeToString(sum[:5]), pages: map[string]*template.Template{}}
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
	p.Theme, p.Links, p.CSS = r.Theme, r.Links, r.css
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", p); err != nil {
		return fmt.Errorf("render %s: %w", rel, err)
	}
	return WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), r.fillSprite(buf.Bytes()))
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

// fileType is a file icon: a Phosphor glyph and a colour role (CSS r-*).
type fileType struct{ glyph, role string }

// fileIcon picks a Phosphor Colored icon (see scripts/icons.py) the way Zed
// does: the whole name as a file name, then as a suffix, then each part after
// a dot ("a.tar.gz" tries "tar.gz", then "gz"). A lowercase retry catches
// names like README.MD. Folders and submodules get the closed folder, and
// "tree-open" (the current folder in breadcrumbs) the open one.
func fileIcon(name, typ string) template.HTML {
	switch typ {
	case "tree", "commit":
		return iconSVG(fileType{"folder-closed", "folder"})
	case "tree-open":
		return iconSVG(fileType{"folder-open", "folder"})
	}
	return iconSVG(cmp.Or(lookupIcon(name), lookupIcon(strings.ToLower(name)), iconDefault))
}

func lookupIcon(name string) fileType {
	for rest, ok := name, true; ok; _, rest, ok = strings.Cut(rest, ".") {
		if t, found := iconStems[rest]; found {
			return t
		}
		if t, found := iconSuffixes[rest]; found {
			return t
		}
	}
	return fileType{}
}

func iconSVG(t fileType) template.HTML {
	return template.HTML(`<svg class="ficon r-` + t.role + `" width="16" height="16" aria-hidden="true" focusable="false"><use href="#f-` + t.glyph + `"></use></svg>`)
}

// The page sprite: base.html emits it empty, and Write fills in the symbols
// the page refers to, so each page carries only the icons it shows. Symbols
// are inline because the CSP blocks data: URLs and external sprites.
const spriteTag = `<svg class="sprite" width="0" height="0" aria-hidden="true" focusable="false">`

var (
	useRe     = regexp.MustCompile(`href="#([fi]-[a-z0-9-]+)"`)
	viewBoxRe = regexp.MustCompile(`viewBox="([0-9 .]+)"`)
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

// loadSymbols turns the embedded Phosphor SVGs into <symbol>s.
func loadSymbols() (map[string][]byte, error) {
	syms := map[string][]byte{}
	for prefix, dir := range map[string]string{"f-": "static/icons/phosphor", "i-": "static/icons/phosphor/ui"} {
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
			syms[id] = fmt.Appendf(nil, `<symbol id="%s" viewBox="%s">%s</symbol>`, id, vb[1], b[start+1:end])
		}
	}
	return syms, nil
}
