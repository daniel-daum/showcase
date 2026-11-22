package web

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestContrast checks that text tokens meet WCAG AA (4.5:1) on the layers
// they sit on, in every palette. It reads the tokens from style.css: saturn's
// moss values from `html {`, each `[data-theme]` override, then the derived
// daum.dev tokens in part 2's `:root {`, and evaluates their color-mix()
// chains in OKLab like a browser does. Run with -v to print the ratios.
func TestContrast(t *testing.T) {
	css, err := FS.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	s := string(css)
	part2 := strings.Index(s, "part 2: daum.dev only")
	if part2 < 0 {
		t.Fatal("no part 2 marker in style.css")
	}
	base := block(t, s, "\nhtml {")
	derived := block(t, s[part2:], ":root {")

	pairs := []struct{ fg, bg string }{
		{"--text-headings", "--page"}, {"--text-headings", "--box"}, {"--text-headings", "--box-head"}, {"--text-headings", "--hover"},
		{"--body-text", "--page"}, {"--body-text", "--box"}, {"--body-text", "--box-head"}, {"--body-text", "--hover"},
		{"--text-secondary", "--page"}, {"--text-secondary", "--box"}, {"--text-secondary", "--box-head"},
		{"--text-secondary", "--hover"},
		{"--accent-bright", "--page"}, {"--accent-bright", "--box"}, {"--accent-bright", "--box-head"}, {"--accent-bright", "--hover"},
		{"--accent", "--page"}, {"--background-dark", "--accent"},
		{"--syn-constant", "--box"}, {"--add", "--box"}, {"--del", "--box"},
	}
	var report strings.Builder
	fmt.Fprintf(&report, "%-18s %-18s", "text", "on")
	themes := []string{"moss", "ink", "wine", "plum", "teal", "flexoki"}
	for _, th := range themes {
		fmt.Fprintf(&report, " %7s", th)
	}
	report.WriteString("\n")
	for _, p := range pairs {
		fmt.Fprintf(&report, "%-18s %-18s", p.fg, p.bg)
		for _, th := range themes {
			vars := map[string]string{}
			for k, v := range base {
				vars[k] = v
			}
			if th != "moss" {
				for k, v := range block(t, s, `[data-theme="`+th+`"] {`) {
					vars[k] = v
				}
			}
			for k, v := range derived {
				vars[k] = v
			}
			r := ratio(eval(t, vars, "var("+p.fg+")"), eval(t, vars, "var("+p.bg+")"))
			fmt.Fprintf(&report, " %7.2f", r)
			if r < 4.5 {
				t.Errorf("%s: %s on %s is %.2f:1, below 4.5:1", th, p.fg, p.bg, r)
			}
		}
		report.WriteString("\n")
	}
	t.Log("contrast ratios\n" + report.String())
}

var declRe = regexp.MustCompile(`(--[\w-]+)\s*:\s*([^;]+);`)

// block returns the custom properties declared in the first rule that
// starts with head.
func block(t *testing.T, s, head string) map[string]string {
	t.Helper()
	i := strings.Index(s, head)
	if i < 0 {
		t.Fatalf("style.css has no %q block", head)
	}
	body := s[i+len(head):]
	body = body[:strings.Index(body, "}")]
	vars := map[string]string{}
	for _, m := range declRe.FindAllStringSubmatch(body, -1) {
		vars[m[1]] = strings.TrimSpace(m[2])
	}
	return vars
}

type rgb [3]float64 // sRGB, 0..1

// eval resolves a colour expression: var(), #hex, black, white and
// color-mix(in oklab, …).
func eval(t *testing.T, vars map[string]string, e string) rgb {
	t.Helper()
	e = strings.TrimSpace(e)
	switch {
	case strings.HasPrefix(e, "var("):
		name := strings.TrimSuffix(strings.TrimPrefix(e, "var("), ")")
		v, ok := vars[name]
		if !ok {
			t.Fatalf("unknown token %s", name)
		}
		return eval(t, vars, v)
	case strings.HasPrefix(e, "#") && len(e) == 7:
		var c rgb
		for i := range c {
			n, _ := strconv.ParseUint(e[1+2*i:3+2*i], 16, 8)
			c[i] = float64(n) / 255
		}
		return c
	case e == "black":
		return rgb{}
	case e == "white":
		return rgb{1, 1, 1}
	case strings.HasPrefix(e, "color-mix(in oklab,"):
		args := split(e[len("color-mix(in oklab,") : len(e)-1])
		if len(args) != 2 {
			t.Fatalf("color-mix needs two colours: %s", e)
		}
		ca, pa := percent(args[0])
		cb, pb := percent(args[1])
		switch {
		case pa < 0 && pb < 0:
			pa, pb = 50, 50
		case pa < 0:
			pa = 100 - pb
		case pb < 0:
			pb = 100 - pa
		}
		if pa+pb != 100 {
			t.Fatalf("color-mix percentages must add to 100 here: %s", e)
		}
		a, b := oklab(eval(t, vars, ca)), oklab(eval(t, vars, cb))
		var m [3]float64
		for i := range m {
			m[i] = a[i]*pa/100 + b[i]*pb/100
		}
		return fromOklab(m)
	}
	t.Fatalf("can't evaluate colour %q", e)
	return rgb{}
}

// split cuts at top-level commas.
func split(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// percent splits "var(--x) 40%" into the colour and 40, or -1 if absent.
func percent(s string) (string, float64) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " "); i > 0 && strings.HasSuffix(s, "%") && !strings.Contains(s[i:], ")") {
		p, err := strconv.ParseFloat(s[i+1:len(s)-1], 64)
		if err == nil {
			return strings.TrimSpace(s[:i]), p
		}
	}
	return s, -1
}

func lin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func gam(c float64) float64 {
	c = math.Max(0, math.Min(1, c))
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// oklab and fromOklab follow Björn Ottosson's reference matrices.
func oklab(c rgb) [3]float64 {
	r, g, b := lin(c[0]), lin(c[1]), lin(c[2])
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return [3]float64{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func fromOklab(c [3]float64) rgb {
	l := math.Pow(c[0]+0.3963377774*c[1]+0.2158037573*c[2], 3)
	m := math.Pow(c[0]-0.1055613458*c[1]-0.0638541728*c[2], 3)
	s := math.Pow(c[0]-0.0894841775*c[1]-1.2914855480*c[2], 3)
	return rgb{
		gam(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		gam(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		gam(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
	}
}

func luminance(c rgb) float64 {
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

func ratio(a, b rgb) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
