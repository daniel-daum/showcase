package render

import (
	"strings"
	"testing"
)

func TestReadmeHTML(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"centred header",
			"<p align=\"center\">\n  <img src=\"Packaging/screenshots/hero.png\" width=\"800\" alt=\"Rhea — app\">\n</p>\n\n<h1 align=\"center\">Rhea</h1>\n",
			"<p align=\"center\">\n  <img src=\"/rhea/raw/Packaging/screenshots/hero.png\" width=\"800\" alt=\"Rhea — app\">\n</p>\n<h1 align=\"center\">Rhea</h1>"},
		{"div around markdown", "<div align=\"center\">\n\n**hi**\n\n</div>\n", "<div align=\"center\">\n<p><strong>hi</strong></p>\n</div>"},
		{"inline badge", "see <a href=\"https://ci.example\"><img src=\"https://ci.example/b.svg\" alt=\"ci\"></a> now",
			"<p>see <a href=\"https://ci.example\"><img src=\"https://ci.example/b.svg\" alt=\"ci\"></a> now</p>"},
		{"unclosed tags end with the document", "<div>\n<b>x\n", "<div>\n<b>x\n</b></div>"},
		{"stray close", "</div></section>\n\ntext", "<p>text</p>"},
		{"script and its text", "<script>alert(1)</script>\n", ""},
		{"handlers and styles", "<p onclick=\"x()\" style=\"color:red\" class=\"a\">hi</p>\n", "<p>hi</p>"},
		{"javascript link", "<p><a href=\"javascript:alert(1)\">x</a></p>\n", "<p><a>x</a></p>"},
		{"http image keeps alt", "<p><img src=\"http://x/a.png\" alt=\"pic\"></p>\n", "<p>pic</p>"},
		{"unknown tags keep text", "<p><span><iframe src=x></iframe>hi &amp; <marquee>bye</marquee></span></p>\n", "<p>hi &amp; bye</p>"},
		{"bad sizes", "<img src=\"a.png\" width=\"1;x\" height=\"20\">\n", "<img src=\"/rhea/raw/a.png\" height=\"20\">"},
		{"comment", "<!-- <script>x</script> -->\n", ""},
	} {
		got, err := Markdown([]byte(c.in), "/rhea", "")
		if err != nil {
			t.Fatal(err)
		}
		if g := strings.TrimSpace(string(got)); g != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, g, c.want)
		}
	}
}
