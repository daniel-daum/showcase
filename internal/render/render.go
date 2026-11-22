// Package render turns data into static HTML files.
package render

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"daum.dev/showcase/web"
)

type Renderer struct {
	Now   time.Time // fixed per build so output is deterministic
	pages map[string]*template.Template
}

// Page is what every template receives.
type Page struct {
	Title    string
	GoImport string // optional go-import meta content
	D        any
}

func New(now time.Time) (*Renderer, error) {
	r := &Renderer{Now: now, pages: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"ago":  r.ago,
		"date": func(t time.Time) string { return t.UTC().Format("2006-01-02") },
		"lang": langClass,
		"size": humanSize,
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
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", p); err != nil {
		return fmt.Errorf("render %s: %w", rel, err)
	}
	return WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), buf.Bytes())
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
		b, err := web.FS.ReadFile(p)
		if err != nil {
			return err
		}
		return WriteFile(filepath.Join(dir, filepath.FromSlash(p)), b)
	})
}

func (r *Renderer) ago(t time.Time) string {
	d := r.Now.Sub(t)
	switch {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month")
	}
	return plural(int(d.Hours()/24/365), "year")
}

func plural(n int, unit string) string {
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d %s ago", n, unit)
}

// langClass maps a language name to one of eight accent colour classes.
func langClass(name string) string {
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
