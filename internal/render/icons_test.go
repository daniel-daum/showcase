package render

import (
	"regexp"
	"strings"
	"testing"
)

// Icons reuse ids ("a", "folder"), so each symbol's ids carry its own prefix.
func TestSymbolIDsArePrefixed(t *testing.T) {
	syms, err := loadSymbols()
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"f-kotlin":     `fill="url(#xf-kotlin-a)"`,
		"f-folder-css": `href="#xf-folder-css-a"`,
	} {
		if s := string(syms[id]); !strings.Contains(s, want) || !strings.Contains(s, `id="xf-`+id[2:]+`-a"`) {
			t.Errorf("%s: ids not prefixed: %.200s", id, s)
		}
	}
	bare := regexp.MustCompile(`id="[^x]|href="#[^x]|url\(#[^x]`)
	for id, s := range syms {
		if m := bare.Find(s[len(`<symbol id="`):]); m != nil {
			t.Errorf("%s has an unprefixed reference %s", id, m)
		}
		if strings.Contains(string(s), "style=") {
			t.Errorf("%s has an inline style, which the CSP blocks", id)
		}
	}
}

func TestFileIcon(t *testing.T) {
	for name, want := range map[string]string{
		"main.go": "go", "go.mod": "go-mod", "README.md": "readme", "README.MD": "readme",
		"Dockerfile": "docker", "justfile": "just", "a.test.weird.ts": "typescript", "noext": "file",
	} {
		if got := string(fileIcon(name, "blob")); !strings.Contains(got, `#f-`+want+`"`) {
			t.Errorf("%s: got %s, want f-%s", name, got, want)
		}
	}
	for typ, want := range map[string]string{"tree": "folder-docs", "tree-open": "folder-open", "commit": "folder-docs"} {
		if got := string(fileIcon("docs", typ)); !strings.Contains(got, `#f-`+want+`"`) {
			t.Errorf("docs as %s: got %s, want f-%s", typ, got, want)
		}
	}
}
