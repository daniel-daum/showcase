package render

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// The heatmap's grid. The activity bars use the same columns.
const (
	cell  = 10
	gap   = 3
	left  = 28 // room for weekday labels
	top   = 16 // room for month labels
	weeks = 53
)

// colX is the left edge of week column col.
func colX(col int) int { return left + col*(cell+gap) }

// monthCols lists the columns that get a month label: the first Sunday of
// each month, leaving out the last two columns.
func monthCols(start time.Time, n int) []int {
	var out []int
	last := time.Month(0)
	for col := 0; col < n-2; col++ {
		if m := start.AddDate(0, 0, 7*col).Month(); m != last {
			last = m
			out = append(out, col)
		}
	}
	return out
}

// Heatmap draws daily counts (keyed "2006-01-02", UTC) ending on the day of
// end as an inline SVG of the last n week columns. The total and the
// aria-label cover the whole year. Colours come from CSS classes, since the
// CSP forbids inline styles.
func Heatmap(counts map[string]int, end time.Time, n int) (template.HTML, int) {
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	start := WeekStart(end, n-1)
	max, total := 0, 0
	for d := WeekStart(end, weeks-1); !d.After(end); d = d.AddDate(0, 0, 1) {
		c := counts[d.Format("2006-01-02")]
		total += c
		if c > max && !d.Before(start) {
			max = c
		}
	}

	w, h := colX(n), top+7*(cell+gap)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="heatmap" viewBox="0 0 %d %d" role="img" aria-label="%d commits in the last year">`, w, h, total)
	for i, label := range []string{"Mon", "Wed", "Fri"} {
		fmt.Fprintf(&b, `<text x="0" y="%d">%s</text>`, top+(2*i+1)*(cell+gap)+cell-1, label)
	}
	months := monthCols(start, n)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		col, row := int(d.Sub(start).Hours()/24)/7, int(d.Weekday())
		x, y := colX(col), top+row*(cell+gap)
		if row == 0 && len(months) > 0 && months[0] == col {
			months = months[1:]
			fmt.Fprintf(&b, `<text x="%d" y="%d">%s</text>`, x, top-5, d.Format("Jan"))
		}
		c := counts[d.Format("2006-01-02")]
		fmt.Fprintf(&b, `<rect class="h%d" x="%d" y="%d" width="%d" height="%d" rx="2"><title>%s on %s</title></rect>`,
			level(c, max), x, y, cell, cell, commits(c), d.Format("Jan 2, 2006"))
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
