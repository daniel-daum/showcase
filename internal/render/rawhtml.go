package render

import (
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// htmlRenderer stands in for goldmark's raw HTML output. Raw HTML is never
// passed through: each tag is parsed, and only an allowlist of tags and
// attributes is written back, built from scratch and escaped. Everything
// else (script, style, iframe, on*, style=, unknown tags) is dropped; the
// contents of script-like tags go too, other text stays as escaped text.
// Open tags are tracked over the whole document, so <div align="center">
// … </div> around Markdown works, and close() ends whatever is left open.
type htmlRenderer struct {
	links linkFixer
	open  []string
	skip  int // depth inside a dropped element whose text is dropped too
}

var (
	tagRe  = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)|<[!?][^>]*>|<(/?)([A-Za-z][A-Za-z0-9-]*)((?:\s+[^\s"'>/=]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>` + "`" + `]+))?)*)\s*/?>`)
	attrRe = regexp.MustCompile(`([^\s"'>/=]+)(?:\s*=\s*("[^"]*"|'[^']*'|[^\s"'=<>` + "`" + `]+))?`)
	sizeRe = regexp.MustCompile(`^[0-9]{1,4}%?$`)

	// tag → the attributes it may keep
	allowed = map[string][]string{
		"p": {"align"}, "div": {"align"}, "h1": {"align"}, "h2": {"align"}, "h3": {"align"},
		"h4": {"align"}, "h5": {"align"}, "h6": {"align"},
		"img": {"src", "alt", "title", "width", "height", "align"}, "a": {"href", "title"},
		"br": nil, "hr": nil, "b": nil, "i": nil, "em": nil, "strong": nil, "code": nil, "kbd": nil,
		"sub": nil, "sup": nil, "s": nil, "del": nil, "pre": nil, "blockquote": nil,
		"ul": nil, "ol": nil, "li": nil, "details": {"open"}, "summary": nil,
	}
	voids   = []string{"br", "hr", "img"}
	dropAll = []string{"script", "style", "template", "textarea", "iframe", "object", "noscript", "svg", "math", "title", "select"}
)

func (h *htmlRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			b := n.(*ast.HTMLBlock)
			var s strings.Builder
			for i := 0; i < b.Lines().Len(); i++ {
				seg := b.Lines().At(i)
				s.Write(seg.Value(src))
			}
			if b.HasClosure() {
				cl := b.ClosureLine
				s.Write(cl.Value(src))
			}
			h.write(w, s.String())
		}
		return ast.WalkSkipChildren, nil
	})
	reg.Register(ast.KindRawHTML, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			var s strings.Builder
			segs := n.(*ast.RawHTML).Segments
			for i := 0; i < segs.Len(); i++ {
				seg := segs.At(i)
				s.Write(seg.Value(src))
			}
			h.write(w, s.String())
		}
		return ast.WalkSkipChildren, nil
	})
}

func (h *htmlRenderer) write(w util.BufWriter, s string) {
	for s != "" {
		loc := tagRe.FindStringSubmatchIndex(s)
		if loc == nil {
			h.text(w, s)
			return
		}
		h.text(w, s[:loc[0]])
		if loc[4] >= 0 { // a tag, not a comment or declaration
			name := strings.ToLower(s[loc[4]:loc[5]])
			if loc[3] > loc[2] {
				h.end(w, name)
			} else {
				h.start(w, name, s[loc[6]:loc[7]])
			}
		}
		s = s[loc[1]:]
	}
}

func (h *htmlRenderer) text(w util.BufWriter, s string) {
	if h.skip == 0 {
		w.WriteString(html.EscapeString(html.UnescapeString(s)))
	}
}

func (h *htmlRenderer) start(w util.BufWriter, name, attrs string) {
	if slices.Contains(dropAll, name) {
		h.skip++
	}
	keep, ok := allowed[name]
	if !ok || h.skip > 0 {
		return
	}
	var out strings.Builder
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		k, v := strings.ToLower(m[1]), html.UnescapeString(strings.Trim(m[2], `"'`))
		if !slices.Contains(keep, k) {
			continue
		}
		switch k {
		case "src":
			d, ok := h.links.resolve(v, "raw", true)
			if !ok {
				continue
			}
			v = d
		case "href":
			d, ok := h.links.resolve(v, "blob", false)
			if u, err := url.Parse(d); !ok || err != nil || !slices.Contains([]string{"", "http", "https", "mailto"}, strings.ToLower(u.Scheme)) {
				continue
			}
			v = d
		case "width", "height":
			if !sizeRe.MatchString(v) {
				continue
			}
		case "align":
			if v = strings.ToLower(v); !slices.Contains([]string{"left", "center", "right"}, v) {
				continue
			}
		}
		out.WriteString(" " + k)
		if k != "open" {
			out.WriteString(`="` + html.EscapeString(v) + `"`)
		}
	}
	if name == "img" && !strings.Contains(out.String(), " src=") {
		h.text(w, attrValue(attrs, "alt")) // like a dropped Markdown image: keep the alt text
		return
	}
	w.WriteString("<" + name + out.String() + ">")
	if !slices.Contains(voids, name) {
		h.open = append(h.open, name)
	}
}

func (h *htmlRenderer) end(w util.BufWriter, name string) {
	if slices.Contains(dropAll, name) {
		h.skip = max(0, h.skip-1)
		return
	}
	i := -1
	for j := len(h.open) - 1; j >= 0 && i < 0; j-- {
		if h.open[j] == name {
			i = j
		}
	}
	if h.skip > 0 || i < 0 {
		return // never opened here: a stray close can't end the page's own elements
	}
	for len(h.open) > i {
		w.WriteString("</" + h.open[len(h.open)-1] + ">")
		h.open = h.open[:len(h.open)-1]
	}
}

// close returns the end tags for everything still open after the document.
func (h *htmlRenderer) close() string {
	var s strings.Builder
	for i := len(h.open) - 1; i >= 0; i-- {
		s.WriteString("</" + h.open[i] + ">")
	}
	h.open = nil
	return s.String()
}

func attrValue(attrs, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		if strings.EqualFold(m[1], name) {
			return html.UnescapeString(strings.Trim(m[2], `"'`))
		}
	}
	return ""
}
