package render

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// WeekStart is the Sunday (UTC) starting the week n weeks before end's week,
// the same columns the heatmap uses.
func WeekStart(end time.Time, n int) time.Time {
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return end.AddDate(0, 0, -int(end.Weekday())-7*n)
}

// Weeks counts times into n weekly buckets ending with end's week (last).
func Weeks(times []time.Time, end time.Time, n int) []int {
	start, out := WeekStart(end, n-1), make([]int, n)
	for _, t := range times {
		if i := int(t.Sub(start).Hours() / 24 / 7); !t.Before(start) && i < n {
			out[i]++
		}
	}
	return out
}

// Streak counts the weeks in a row with commits, ending this week, or last
// week while this one is still empty.
func Streak(weeks []int) int {
	i := len(weeks) - 1
	if i >= 0 && weeks[i] == 0 {
		i--
	}
	n := 0
	for ; i >= 0 && weeks[i] > 0; i-- {
		n++
	}
	return n
}

// WeeklyBars draws one bar per week, tallest = busiest, coloured with the
// heatmap's steps. Empty weeks keep a thin baseline so the year reads as one
// strip.
func WeeklyBars(weeks []int, end time.Time) template.HTML {
	const bw, gap, h = 10, 3, 44
	top := 0
	for _, n := range weeks {
		top = max(top, n)
	}
	start := WeekStart(end, len(weeks)-1)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="weekly" viewBox="0 0 %d %d" preserveAspectRatio="none" aria-hidden="true">`, len(weeks)*(bw+gap)-gap, h)
	for i, n := range weeks {
		bh := 2
		if n > 0 {
			bh = max(4, n*h/top)
		}
		fmt.Fprintf(&b, `<rect class="h%d" x="%d" y="%d" width="%d" height="%d" rx="1.5"><title>%s, week of %s</title></rect>`,
			level(n, top), i*(bw+gap), h-bh, bw, bh, commits(n), start.AddDate(0, 0, 7*i).Format("Jan 2"))
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// Sparkline draws weekly counts as a small line with a soft area under it.
func Sparkline(weeks []int) template.HTML {
	const w, h = 96, 24
	top := 0
	for _, n := range weeks {
		top = max(top, n)
	}
	var pts strings.Builder
	for i, n := range weeks {
		y := float64(h - 2)
		if top > 0 {
			y = float64(h-2) - float64(n)/float64(top)*float64(h-4)
		}
		fmt.Fprintf(&pts, "%.1f,%.1f ", float64(i)*w/float64(len(weeks)-1), y)
	}
	p := strings.TrimSpace(pts.String())
	class := "spark"
	if top == 0 {
		class += " spark--flat"
	}
	return template.HTML(fmt.Sprintf(`<svg class="%s" viewBox="0 0 %d %d" preserveAspectRatio="none" aria-hidden="true"><polygon class="spark-area" points="0,%d %s %d,%d"></polygon><polyline class="spark-line" points="%s"></polyline></svg>`,
		class, w, h, h, p, w, h, p))
}
