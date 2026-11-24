package render

import (
	"html"
	"regexp"
	"strings"
	"testing"
)

// TestHighlightKeepsText: the lines' text, joined, gives back the source,
// trailing blank lines included, so line counts are right.
func TestHighlightKeepsText(t *testing.T) {
	tags := regexp.MustCompile(`<[^>]*>`)
	for _, src := range []string{"a\nb", "a\nb\n", "a\n\n", "a\n\n\n", "\n", "\tif x {\n\t\treturn\n\t}\n", "<b>&amp;</b>\n", "x\r\ny\r\n"} {
		var lines []string
		for _, l := range Highlight("f.go", "", src) {
			lines = append(lines, html.UnescapeString(tags.ReplaceAllString(string(l), "")))
		}
		nl := "\n"
		if strings.Contains(src, "\r\n") {
			nl = "\r\n" // chroma drops the \r
		}
		got := strings.Join(lines, nl)
		if strings.HasSuffix(src, "\n") {
			got += nl
		}
		if got != src {
			t.Errorf("%q: %d lines give back %q", src, len(lines), got)
		}
	}
}
