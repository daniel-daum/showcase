package build

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"daum.dev/showcase/internal/gitea"
)

func inode(t *testing.T, p string) uint64 {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t).Ino
}

func builds(t *testing.T, b *Builder) []string {
	t.Helper()
	es, _ := os.ReadDir(b.siteDir())
	var names []string
	for _, e := range es {
		if e.Name() != "current" {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestBuildLoop(t *testing.T) {
	b := testBuilder(t)
	ctx := context.Background()
	if b.Ready() {
		t.Fatal("ready before the first build")
	}
	tick := 0
	now := b.Now()
	b.Now = func() time.Time { tick++; return now.Add(time.Duration(tick) * time.Second) }

	if err := b.BuildOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !b.Ready() {
		t.Fatal("not ready after a build")
	}
	blob := func() string { return filepath.Join(b.Current(), "repo-a/blob/go.mod.html") }
	first := inode(t, blob())

	// unchanged HEAD and header: git pages are hard-linked, not re-rendered
	if err := b.BuildOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if inode(t, blob()) != first {
		t.Error("blob page was re-rendered although nothing changed")
	}

	// a changed key re-renders; only the current and previous builds remain
	b.st.keys["repo-a"] = "changed"
	if err := b.BuildOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if inode(t, blob()) == first {
		t.Error("blob page was reused although the repo changed")
	}
	if n := builds(t, b); len(n) != 2 {
		t.Errorf("want 2 builds kept, got %v", n)
	}

	// a failing build leaves the current one in place and no temp dir behind
	cur, _ := os.Readlink(b.Current())
	b.Gitea = gitea.New("http://127.0.0.1:1", "daniel")
	if err := b.BuildOnce(ctx); err == nil {
		t.Fatal("build against a dead Gitea succeeded")
	}
	if now, _ := os.Readlink(b.Current()); now != cur {
		t.Error("current changed after a failed build")
	}
	if n := builds(t, b); len(n) != 2 {
		t.Errorf("failed build left files behind: %v", n)
	}
}
