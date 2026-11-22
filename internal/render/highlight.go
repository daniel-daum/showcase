package render

import (
	"html/template"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Highlight tokenises src with chroma and returns one HTML string per line.
// Tokens get saturn's hl-<role> classes, so the palette CSS applies and no
// inline styles are needed. name picks the lexer; lang, if set, wins.
func Highlight(name, lang, src string) []template.HTML {
	var lx chroma.Lexer
	if lang != "" {
		lx = lexers.Get(lang)
	}
	if lx == nil {
		lx = lexers.Match(name)
	}
	if lx == nil && name != "" {
		lx = lexers.Analyse(src)
	}
	if lx == nil {
		lx = lexers.Fallback
	}
	src = strings.TrimSuffix(src, "\n")
	it, err := chroma.Coalesce(lx).Tokenise(nil, src)
	if err != nil {
		it = chroma.Literator(chroma.Token{Type: chroma.Text, Value: src})
	}
	var lines []template.HTML
	for _, line := range chroma.SplitTokensIntoLines(it.Tokens()) {
		var b strings.Builder
		for _, t := range line {
			v := template.HTMLEscapeString(strings.TrimSuffix(t.Value, "\n"))
			if r := role(t.Type); r != "" && v != "" {
				b.WriteString(`<span class="hl-` + r + `">` + v + `</span>`)
			} else {
				b.WriteString(v)
			}
		}
		lines = append(lines, template.HTML(b.String()))
	}
	return lines
}

// role maps a chroma token type to one of saturn's syntax roles, following
// the scopes in saturn's scripts/highlight.mjs.
func role(t chroma.TokenType) string {
	switch t {
	case chroma.KeywordConstant, chroma.NameConstant, chroma.NameDecorator, chroma.NameEntity:
		return "constant"
	case chroma.KeywordType, chroma.NameClass, chroma.NameException, chroma.NameNamespace:
		return "type"
	case chroma.NameBuiltinPseudo:
		return "self"
	case chroma.NameBuiltin:
		return "builtin"
	case chroma.NameFunction, chroma.NameFunctionMagic:
		return "function"
	case chroma.NameTag, chroma.NameAttribute, chroma.NameProperty:
		return "variable"
	case chroma.LiteralStringEscape, chroma.LiteralStringRegex, chroma.LiteralStringInterpol:
		return "escape"
	case chroma.GenericInserted:
		return "added"
	case chroma.GenericDeleted:
		return "removed"
	case chroma.GenericHeading, chroma.GenericSubheading:
		return "heading"
	}
	switch {
	case t.InCategory(chroma.Comment):
		return "comment"
	case t.InCategory(chroma.Keyword):
		return "keyword"
	case t.InSubCategory(chroma.NameVariable):
		return "variable"
	case t.InSubCategory(chroma.LiteralString):
		return "string"
	case t.InSubCategory(chroma.LiteralNumber):
		return "constant"
	case t.InCategory(chroma.Operator), t.InCategory(chroma.Punctuation):
		return "punct"
	}
	return ""
}
