package build

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Builds live in <data>/site/<timestamp>/; <data>/site/current is a symlink
// to the newest good one. The server only ever reads through current.
type state struct {
	mu   sync.Mutex
	prev string            // last good build dir, "" before the first
	keys map[string]string // repo -> reuse key of the last good build
	next map[string]string // keys of the build in progress
}

func (b *Builder) siteDir() string { return filepath.Join(b.Data, "site") }

// Current is the directory the server serves from.
func (b *Builder) Current() string { return filepath.Join(b.siteDir(), "current") }

// Ready reports whether a good build exists (possibly from an earlier run).
func (b *Builder) Ready() bool {
	_, err := os.Stat(filepath.Join(b.Current(), "index.html"))
	return err == nil
}

// Run builds now and then every interval until ctx ends. Failures are logged
// and the last good build keeps being served.
func (b *Builder) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		start := time.Now()
		if err := b.BuildOnce(ctx); err != nil {
			b.Log.Error("build failed", "err", err, "took", time.Since(start))
		} else {
			b.Log.Info("build done", "took", time.Since(start))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// BuildOnce renders a new build and publishes it atomically. It never runs
// concurrently with itself, and never publishes a partial build.
func (b *Builder) BuildOnce(ctx context.Context) error {
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	if err := os.MkdirAll(b.siteDir(), 0o755); err != nil {
		return err
	}
	if b.st.prev == "" { // after a restart: keep serving the old build until the swap
		if t, err := os.Readlink(b.Current()); err == nil {
			b.st.prev = filepath.Join(b.siteDir(), t)
		}
	}
	name := b.Now().UTC().Format("20060102T150405.000")
	tmp := filepath.Join(b.siteDir(), ".tmp-"+name)
	b.st.next = map[string]string{}
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return err
	}
	if err := b.Render(ctx, tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	final := filepath.Join(b.siteDir(), name)
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	link := b.Current() + ".new"
	os.Remove(link)
	if err := os.Symlink(name, link); err != nil {
		return err
	}
	if err := os.Rename(link, b.Current()); err != nil { // atomic swap
		return err
	}
	b.prune(name, filepath.Base(b.st.prev))
	b.st.prev, b.st.keys = final, b.st.next
	return nil
}

// prune removes every build except keep (and the previous one, so requests
// already reading it can finish) plus leftovers from crashed builds.
func (b *Builder) prune(keep ...string) {
	entries, _ := os.ReadDir(b.siteDir())
	for _, e := range entries {
		n := e.Name()
		if n == "current" || n == keep[0] || n == keep[1] {
			continue
		}
		os.RemoveAll(filepath.Join(b.siteDir(), n))
	}
}

// reuse links r's git-derived pages (dirs) from the previous build when the
// repo's HEAD and header data are unchanged. It reports whether it did.
func (b *Builder) reuse(r *repo, dir string, dirs ...string) bool {
	// everything the shared repo header and tabs show
	h, _ := json.Marshal([]any{r.Head, r.Description, r.Website, r.Topics, r.Archived, r.DefaultBranch, r.LatestTag(),
		len(r.Branches), len(r.Tags), len(r.Releases), len(r.Pulls), len(r.Packages), r.License, r.Avatar})
	key := fmt.Sprintf("%x", sha256.Sum256(h))
	if b.st.next == nil { // -once mode: no previous build
		return false
	}
	b.st.next[r.Name] = key
	if b.st.prev == "" || b.st.keys[r.Name] != key {
		return false
	}
	for _, d := range dirs {
		src := filepath.Join(b.st.prev, r.Name, d)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := linkTree(src, filepath.Join(dir, r.Name, d)); err != nil {
			b.Log.Warn("reuse failed, re-rendering", "repo", r.Name, "err", err)
			for _, d := range dirs { // never write through links into the old build
				os.RemoveAll(filepath.Join(dir, r.Name, d))
			}
			return false
		}
	}
	return true
}

// linkTree hard-links every file under src into dst. Builds are never
// modified once published, so sharing inodes between them is safe.
func linkTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, strings.TrimPrefix(p, src))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return os.Link(p, target)
	})
}
