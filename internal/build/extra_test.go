package build

import (
	"strconv"
	"strings"
	"testing"

	"daum.dev/showcase/internal/gitea"
)

// TestUserTextLimits: a flood of comments or one huge body stays small.
func TestUserTextLimits(t *testing.T) {
	r := &repo{Comments: map[int][]gitea.Comment{}}
	for i := range maxThread + 5 {
		r.Comments[1] = append(r.Comments[1], gitea.Comment{Body: "c" + strconv.Itoa(i)})
	}
	th, hidden, err := thread(r, 1, "/repo-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(th) != maxThread || hidden != 5 || !strings.Contains(string(th[len(th)-1].Body), "c"+strconv.Itoa(maxThread+4)) {
		t.Errorf("thread kept %d, hid %d; want the newest %d and 5 hidden", len(th), hidden, maxThread)
	}

	h, err := userMarkdown(strings.Repeat("é", maxUserText), "/repo-a") // two bytes each: the cut lands mid-rune
	if err != nil {
		t.Fatal(err)
	}
	if len(h) > maxUserText+200 || !strings.Contains(string(h), "cut: too long") {
		t.Errorf("long body rendered to %d bytes", len(h))
	}
}
