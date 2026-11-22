package render

import (
	"bytes"
	"html/template"
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Markdown renders src with raw HTML disabled (goldmark's default). Relative
// links and images are resolved against dir inside the repo at base (e.g.
// "/repo-a"); images that aren't relative or https are dropped.
func Markdown(src []byte, base, dir string) (template.HTML, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(linkFixer{base, dir}, 100)),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

type linkFixer struct{ base, dir string }

func (f linkFixer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	var drop []*ast.Image
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			if d, ok := f.resolve(string(n.Destination), "raw", true); ok {
				n.Destination = []byte(d)
			} else {
				drop = append(drop, n)
			}
		case *ast.Link:
			if d, ok := f.resolve(string(n.Destination), "blob", false); ok {
				n.Destination = []byte(d)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, img := range drop { // keep the alt text, lose the image
		parent := img.Parent()
		for c := img.FirstChild(); c != nil; {
			next := c.NextSibling()
			parent.InsertBefore(parent, img, c)
			c = next
		}
		parent.RemoveChild(parent, img)
	}
}

// resolve maps a Markdown destination to a site URL. Absolute URLs pass
// through for links (goldmark blanks dangerous schemes); images must be https.
func (f linkFixer) resolve(dest, kind string, image bool) (string, bool) {
	u, err := url.Parse(dest)
	if err != nil {
		return "", false
	}
	if u.Scheme != "" || u.Host != "" {
		return dest, !image || u.Scheme == "https"
	}
	if u.Path == "" { // "#anchor" or "?q"
		return dest, !image
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		p = path.Join("/", f.dir, p)
	}
	p = strings.TrimPrefix(path.Clean(p), "/")
	if p == "" || p == "." {
		return f.base + "/", !image
	}
	if !image && strings.HasSuffix(u.Path, "/") {
		kind = "tree"
		p += "/"
	}
	out := f.base + "/" + kind + "/" + EscapePath(p)
	if !image && u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out, true
}

// EscapePath escapes each segment of a slash-separated repo path.
func EscapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
