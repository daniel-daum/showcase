package render

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

const (
	cell  = 10
	gap   = 3
	left  = 28 // room for weekday labels
	top   = 16 // room for month labels
	weeks = 53
)

// Heatmap draws a year of daily counts (keyed "2006-01-02", UTC) ending on
// the day of end as an inline SVG. Colours come from CSS classes, since the
// CSP forbids inline styles.
func Heatmap(counts map[string]int, end time.Time) (template.HTML, int) {
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -int(end.Weekday())-7*(weeks-1))
	max, total := 0, 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		n := counts[d.Format("2006-01-02")]
		total += n
		if n > max {
			max = n
		}
	}

	w, h := left+weeks*(cell+gap), top+7*(cell+gap)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="heatmap" viewBox="0 0 %d %d" role="img" aria-label="%d commits in the last year">`, w, h, total)
	for i, label := range []string{"Mon", "Wed", "Fri"} {
		fmt.Fprintf(&b, `<text x="0" y="%d">%s</text>`, top+(2*i+1)*(cell+gap)+cell-1, label)
	}
	lastMonth := time.Month(0)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		col, row := int(d.Sub(start).Hours()/24)/7, int(d.Weekday())
		x, y := left+col*(cell+gap), top+row*(cell+gap)
		if row == 0 && d.Month() != lastMonth && col < weeks-2 {
			lastMonth = d.Month()
			fmt.Fprintf(&b, `<text x="%d" y="%d">%s</text>`, x, top-5, d.Format("Jan"))
		}
		n := counts[d.Format("2006-01-02")]
		fmt.Fprintf(&b, `<rect class="h%d" x="%d" y="%d" width="%d" height="%d" rx="2"><title>%s on %s</title></rect>`,
			level(n, max), x, y, cell, cell, commits(n), d.Format("Jan 2, 2006"))
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String()), total
}

func level(n, max int) int {
	if n <= 0 || max <= 0 {
		return 0
	}
	return (4*n + max - 1) / max // ceil(4n/max), 1..4
}

func commits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}
