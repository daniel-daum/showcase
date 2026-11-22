package build

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Things that must never appear in any rendered page: they would break the
// CSP (inline style or script, event handlers) or leak private data.
var (
	inlineStyle  = regexp.MustCompile(`<[a-zA-Z][^>]*\sstyle=`)
	eventHandler = regexp.MustCompile(`<[a-zA-Z][^>]*\son[a-z]+=`)
	scriptTag    = regexp.MustCompile(`(?i)<script`)
	// the one allowed script: same-origin copy buttons, no inline code
	ownScripts = regexp.MustCompile(`<script src="/static/(theme\.js\?v=[0-9a-f]+"|copy\.js\?v=[0-9a-f]+" defer)></script>`)
	iconUse    = regexp.MustCompile(`<use href="#([fi]-[a-z0-9-]+)">`)
)

func TestOutputIsSafe(t *testing.T) {
	dir := t.TempDir()
	if err := testBuilder(t).Render(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	pages := 0
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, _ := os.ReadFile(p)
		s := string(b)
		// private repo and data from it, anywhere, including raw files
		for _, bad := range []string{"leaky", "should never render", "should-never-render", "tailnet.example"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s contains %q", rel, bad)
			}
		}
		if !strings.HasSuffix(p, ".html") || strings.Contains(rel, "/raw/") {
			return nil
		}
		pages++
		s = ownScripts.ReplaceAllString(s, "")
		for name, re := range map[string]*regexp.Regexp{"inline style": inlineStyle, "event handler": eventHandler, "script": scriptTag} {
			if m := re.FindString(s); m != "" {
				t.Errorf("%s has an %s: %s", rel, name, m)
			}
		}
		// every icon a page draws has its symbol in that page's sprite
		for _, m := range iconUse.FindAllStringSubmatch(s, -1) {
			if !strings.Contains(s, `<symbol id="`+m[1]+`"`) {
				t.Errorf("%s uses icon %s but its sprite has no such symbol", rel, m[1])
			}
		}
		return nil
	})
	if pages < 20 {
		t.Errorf("only %d pages checked", pages)
	}
	if _, err := os.Stat(filepath.Join(dir, "leaky")); err == nil {
		t.Error("private repo directory exists")
	}
}

// Under a base_path every page link stays inside it: anything root-relative
// outside /projects/ would 404 on danieldaum.net. Downloads go to site_url.
func TestBasePathLinks(t *testing.T) {
	dir := t.TempDir()
	b := testBuilder(t)
	b.Cfg.BasePath = "/projects"
	if err := b.Render(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(`\s(?:href|src)="(/[^"]*)"`)
	pages, downloads := 0, 0
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") || strings.Contains(p, "/raw/") {
			return err
		}
		pages++
		s, _ := os.ReadFile(p)
		for _, m := range link.FindAllStringSubmatch(string(s), -1) {
			if !strings.HasPrefix(m[1], "/projects/") {
				t.Errorf("%s links outside the base: %s", p, m[1])
			}
		}
		downloads += strings.Count(string(s), `href="https://code.danieldaum.net/repo-a/releases/download/`)
		return nil
	})
	if pages < 50 || downloads == 0 {
		t.Errorf("checked %d pages and %d download links", pages, downloads)
	}
}
