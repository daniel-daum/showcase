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
		for name, re := range map[string]*regexp.Regexp{"inline style": inlineStyle, "event handler": eventHandler, "script": scriptTag} {
			if m := re.FindString(s); m != "" {
				t.Errorf("%s has an %s: %s", rel, name, m)
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
