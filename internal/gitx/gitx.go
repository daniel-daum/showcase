// Package gitx wraps the git CLI for reading bare mirrors.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// env keeps git from prompting for credentials, reading user config or
// using anything but HTTP(S). A private repo just fails.
var env = append(os.Environ(),
	"GIT_TERMINAL_PROMPT=0",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_ALLOW_PROTOCOL=http:https",
	"GIT_ASKPASS=/bin/false",
)

type Repo struct{ Dir string }

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	args = append([]string{"-c", "credential.helper=", "-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = dir, env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[4], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Mirror clones url into dir as a bare mirror, or fetches if it exists.
func Mirror(ctx context.Context, url, dir string) (Repo, error) {
	if _, err := os.Stat(dir); err == nil {
		_, err := git(ctx, dir, "fetch", "--prune", "--quiet", "origin")
		return Repo{dir}, err
	}
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if _, err := git(ctx, "", "clone", "--mirror", "--quiet", url, tmp); err != nil {
		os.RemoveAll(tmp)
		return Repo{}, err
	}
	return Repo{dir}, os.Rename(tmp, dir)
}

func (r Repo) run(ctx context.Context, args ...string) ([]byte, error) {
	return git(ctx, r.Dir, args...)
}

// Head returns the commit hash of branch.
func (r Repo) Head(ctx context.Context, branch string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch+"^{commit}")
	return strings.TrimSpace(string(out)), err
}

type Entry struct {
	Mode, Type, Hash, Name string
	Size                   int64 // -1 for trees and submodules
}

// Tree lists the entries of dir ("" for the root) at rev.
func (r Repo) Tree(ctx context.Context, rev, dir string) ([]Entry, error) {
	spec := rev + "^{tree}"
	if dir != "" {
		spec = rev + ":" + dir
	}
	out, err := r.run(ctx, "ls-tree", "-z", "-l", "--end-of-options", spec)
	if err != nil {
		return nil, err
	}
	var es []Entry
	for _, rec := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 {
			continue
		}
		size, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil {
			size = -1
		}
		es = append(es, Entry{Mode: f[0], Type: f[1], Hash: f[2], Name: name, Size: size})
	}
	return es, nil
}

// Blob returns the contents of the blob at hash, or nil if it is larger than max.
func (r Repo) Blob(ctx context.Context, hash string, size, max int64) ([]byte, error) {
	if size > max {
		return nil, nil
	}
	return r.run(ctx, "cat-file", "blob", hash)
}

type Commit struct {
	Hash, Author, Subject, Body string
	When                        time.Time
}

func (c Commit) Short() string { return c.Hash[:min(len(c.Hash), 10)] }

// Log returns up to n commits reachable from rev, optionally touching path.
func (r Repo) Log(ctx context.Context, rev string, n int, path string) ([]Commit, error) {
	args := []string{"log", "-n", strconv.Itoa(n), "--format=%H%x1f%an%x1f%at%x1f%s%x1f%b%x1e", "--end-of-options", rev}
	if path != "" {
		args = append(args, "--", ":(literal)"+path)
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) != 5 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		cs = append(cs, Commit{Hash: f[0], Author: f[1], When: time.Unix(at, 0).UTC(), Subject: f[3], Body: strings.TrimSpace(f[4])})
	}
	return cs, nil
}

// CommitDays counts commits reachable from rev per UTC day since since.
func (r Repo) CommitDays(ctx context.Context, rev string, since time.Time, into map[string]int) error {
	out, err := r.run(ctx, "log", "--format=%at", "--since="+since.UTC().Format(time.RFC3339), "--end-of-options", rev)
	if err != nil {
		return err
	}
	for _, line := range strings.Fields(string(out)) {
		if at, err := strconv.ParseInt(line, 10, 64); err == nil {
			into[time.Unix(at, 0).UTC().Format("2006-01-02")]++
		}
	}
	return nil
}
