package render

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	testEnd   = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) // a Saturday
	reViewBox = regexp.MustCompile(`viewBox="0 0 (\d+) `)
	reText    = regexp.MustCompile(`<text class="([^"]+)" x="([\d.]+)" y="(\d+)" text-anchor="(\w+)">(.*?)</text>`)
)

func atoi(t *testing.T, s string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func find(t *testing.T, re, s string) []string {
	t.Helper()
	m := regexp.MustCompile(re).FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("no match for %s", re)
	}
	return m
}

func TestBarsShareHeatmapColumns(t *testing.T) {
	day := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC) // week of Oct 18
	counts := Weeks([]time.Time{day}, testEnd, weeks)
	for _, n := range []int{weeks, 26} {
		heat, _ := Heatmap(map[string]int{"2026-10-20": 1}, testEnd, n)
		bars := string(ActivityBars(counts, testEnd, 0, n))
		hx := find(t, `<rect class="h\d" x="(\d+)"[^>]*><title>1 commit on Oct 20, 2026`, string(heat))[1]
		bx := find(t, `week of Oct 18 – Oct 24</title></rect><path class="bars-bar h4[^"]*" d="M(\d+) `, bars)[1]
		if hx != bx {
			t.Errorf("n=%d: bar at x=%s, heatmap column at x=%s", n, bx, hx)
		}
		if hw, bw := reViewBox.FindStringSubmatch(string(heat))[1], reViewBox.FindStringSubmatch(bars)[1]; hw != bw {
			t.Errorf("n=%d: viewBox widths %s and %s", n, hw, bw)
		}
	}
}

func TestYAxis(t *testing.T) {
	for m := 0; m <= 2000; m++ {
		step, top := yAxis(m)
		if lines := top/step + 1; lines < 2 || lines > 3 || top < m || top%step != 0 {
			t.Fatalf("max %d: step %d, top %d", m, step, top)
		}
	}
}

func TestStreakBracket(t *testing.T) {
	cases := []struct {
		name       string
		from, to   int // weeks with commits, inclusive
		n          int
		wantStart  int // bracket columns in the drawn chart
		wantEnd    int
		wantStreak int
	}{
		{"this week empty", 45, 51, weeks, 45, 51, 7},
		{"to the right edge", 40, 52, weeks, 40, 52, 13},
		{"whole year", 0, 52, weeks, 1, 52, 52},
		{"clipped on small screens", 10, 52, 26, 0, 25, 43},
	}
	for _, c := range cases {
		counts := make([]int, weeks)
		for i := c.from; i <= c.to; i++ {
			counts[i] = 1
		}
		streak := Streak(counts[1:])
		if streak != c.wantStreak {
			t.Fatalf("%s: Streak = %d", c.name, streak)
		}
		m := find(t, `class="bars-streak" d="M(\d+) \d+V\d+H(\d+)V`, string(ActivityBars(counts, testEnd, streak, c.n)))
		if x0, x1 := int(atoi(t, m[1])), int(atoi(t, m[2])); x0 != colX(c.wantStart) || x1 != colX(c.wantEnd)+cell {
			t.Errorf("%s: bracket %d–%d, want %d–%d", c.name, x0, x1, colX(c.wantStart), colX(c.wantEnd)+cell)
		}
	}
}

// The labels stay inside the viewBox: axis values in the gutter, "this
// week" under the right end of the baseline.
func TestLabelsFit(t *testing.T) {
	for _, m := range []int{0, 1, 9, 15, 99, 1234} {
		counts := make([]int, weeks)
		counts[0], counts[weeks-1] = m, m
		for _, n := range []int{weeks, 26} {
			bars := string(ActivityBars(counts, testEnd, Streak(counts[1:]), n))
			w := atoi(t, reViewBox.FindStringSubmatch(bars)[1])
			for _, x := range reText.FindAllStringSubmatch(bars, -1) {
				width := float64(len([]rune(x[5]))) * 5.4 // overshoots Geist at 9px
				x0 := map[string]float64{"start": 0, "middle": width / 2, "end": width}[x[4]]
				if x0 = atoi(t, x[2]) - x0; x0 < 0 || x0+width > w || atoi(t, x[3]) > barsH-2 {
					t.Errorf("max %d, n=%d: %q at %.1f–%.1f, y=%s, outside 0–%v", m, n, x[5], x0, x0+width, x[3], w)
				}
			}
		}
	}
}

func TestEmptyYear(t *testing.T) {
	bars := string(ActivityBars(make([]int, weeks), testEnd, 0, weeks))
	for _, s := range []string{"bars-streak", "bars-now", "h1", "h4"} {
		if strings.Contains(bars, s) {
			t.Errorf("empty year has %q", s)
		}
	}
	for _, s := range []string{`aria-label="No commits in the last year"`, `class="bars-empty"`, ">No commits in the last year<", "bars-grid", "bars-stub"} {
		if !strings.Contains(bars, s) {
			t.Errorf("empty year lacks %q", s)
		}
	}
}
