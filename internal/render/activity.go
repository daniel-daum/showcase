package render

import (
	"fmt"
	"html/template"
	"math"
	"strconv"
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

// The bar chart's rows, in viewBox units. Its columns are the heatmap's.
const (
	barsTop, barsBase = 18, 100  // plot area
	barsH, barsLabel  = 118, 114 // "this week" under the baseline
)

// yAxis picks a 1-2-5 step that gives 2-3 grid lines (0 included), with the
// top line at or above m.
func yAxis(m int) (step, top int) {
	for s := 1; ; s *= 10 {
		for _, k := range []int{s, 2 * s, 5 * s} {
			if m <= 2*k {
				return k, k * max(1, (m+k-1)/k)
			}
		}
	}
}

func num(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }

// ActivityBars draws weekly commit counts (oldest first, ending with end's
// week) as a bar chart on the heatmap's columns, so each bar sits above its
// week. Only the last n weeks are drawn; the aria-label sums them all. It
// marks this week and brackets the streak's weeks, counted like Streak.
func ActivityBars(counts []int, end time.Time, streak, n int) template.HTML {
	start := WeekStart(end, len(counts)-1)
	week := func(i int) time.Time { return start.AddDate(0, 0, 7*i) }
	total, peak := 0, 0
	for i, c := range counts {
		if total += c; c >= counts[peak] {
			peak = i
		}
	}
	sum := "No commits in the last year"
	if total > 0 {
		sum = fmt.Sprintf("%s in %d weeks, busiest week %s with %d", commits(total), len(counts), week(peak).Format("Jan 2"), counts[peak])
		if streak > 0 {
			sum += fmt.Sprintf(", %d-week streak", streak)
		}
	}
	last := len(counts) - 1 // the streak's last week
	if counts[last] == 0 {
		last--
	}
	off := len(counts) - n
	counts, start, last = counts[off:], week(off), last-off

	shown, peak := 0, 0
	for i, c := range counts {
		if shown += c; c >= counts[peak] {
			peak = i
		}
	}
	step, ymax := yAxis(counts[peak])
	height := func(c int) float64 { return max(4, float64(c*(barsBase-barsTop))/float64(ymax)) }
	right := float64(colX(n) - gap) // the baseline's end

	var b strings.Builder
	text := func(class string, x float64, y int, anchor, s string) {
		fmt.Fprintf(&b, `<text class="%s" x="%s" y="%d" text-anchor="%s">%s</text>`, class, num(x), y, anchor, s)
	}
	fmt.Fprintf(&b, `<svg class="bars" viewBox="0 0 %d %d" role="img" aria-label="%s">`, colX(n), barsH, sum)
	for v := 0; v <= ymax; v += step {
		y, class := barsBase-float64(v*(barsBase-barsTop))/float64(ymax), "bars-grid"
		if v == 0 {
			class = "bars-base"
		}
		fmt.Fprintf(&b, `<line class="%s" x1="%d" x2="%s" y1="%s" y2="%[4]s"/>`, class, left-2, num(right), num(y))
		text("bars-axis", left-6, int(y)+3, "end", strconv.Itoa(v))
	}
	for _, c := range monthCols(start, n) {
		if x := num(float64(colX(c)) - gap/2.0); c > 0 {
			fmt.Fprintf(&b, `<line class="bars-month" x1="%s" x2="%s" y1="%d" y2="%d"/>`, x, x, barsTop, barsBase)
		}
	}
	for i, c := range counts {
		x, h, r, class := colX(i), 2.0, 1.0, "bars-bar bars-stub"
		if c > 0 {
			h, r, class = height(c), 2, fmt.Sprintf("bars-bar h%d", level(c, counts[peak]))
		}
		if i == n-1 && shown > 0 {
			class += " bars-now"
		}
		fmt.Fprintf(&b, `<rect class="bars-hit" x="%s" y="0" width="%d" height="%d"><title>%s · week of %s – %s</title></rect>`,
			num(float64(x)-gap/2.0), cell+gap, barsBase+2, commits(c), week(i).Format("Jan 2"), week(i).AddDate(0, 0, 6).Format("Jan 2"))
		fmt.Fprintf(&b, `<path class="%s" d="M%d %dV%sa%g %g 0 0 1 %g-%gh%ga%g %g 0 0 1 %g %gV%dz"/>`,
			class, x, barsBase, num(barsBase-h+r), r, r, r, r, cell-2*r, r, r, r, r, barsBase)
	}
	if shown == 0 {
		msg := "No commits in the last year"
		if total > 0 {
			msg = fmt.Sprintf("No commits in the last %d weeks", n)
		}
		text("bars-empty", float64(left+colX(n))/2, (barsTop+barsBase)/2+3, "middle", msg)
		return template.HTML(b.String() + `</svg>`)
	}

	text("bars-now-text", right, barsLabel, "end", "this week")
	if first := max(0, last-streak+1); streak >= 2 && last >= 0 {
		fmt.Fprintf(&b, `<path class="bars-streak" d="M%d %dV%dH%dV%d"/>`, colX(first), barsBase+4, barsBase+6, colX(last)+cell, barsBase+4)
	}
	return template.HTML(b.String() + `</svg>`)
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
