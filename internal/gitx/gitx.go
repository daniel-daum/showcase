// Package gitx wraps the git CLI for reading bare mirrors.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
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

// Tree lists every entry at rev, recursively, with full paths as names.
func (r Repo) Tree(ctx context.Context, rev string) ([]Entry, error) {
	out, err := r.run(ctx, "ls-tree", "-r", "-t", "-z", "-l", "--end-of-options", rev+"^{tree}")
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

// Blob returns the contents of the blob at hash.
func (r Repo) Blob(ctx context.Context, hash string) ([]byte, error) {
	return r.run(ctx, "cat-file", "blob", hash)
}

type Commit struct {
	Hash, Author, Subject, Body string
	When                        time.Time
	Parents                     []string
}

func (c Commit) Short() string { return c.Hash[:min(len(c.Hash), 10)] }

// Log returns up to n commits reachable from rev, optionally touching path.
func (r Repo) Log(ctx context.Context, rev string, n int, path string) ([]Commit, error) {
	args := []string{"log", "-n", strconv.Itoa(n), "--format=%H%x1f%an%x1f%at%x1f%s%x1f%b%x1f%P%x1e", "--end-of-options", rev}
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
		if len(f) != 6 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		cs = append(cs, Commit{Hash: f[0], Author: f[1], When: time.Unix(at, 0).UTC(), Subject: f[3],
			Body: strings.TrimSpace(f[4]), Parents: strings.Fields(f[5])})
	}
	return cs, nil
}

// CommitCount is the number of commits reachable from rev.
func (r Repo) CommitCount(ctx context.Context, rev string) (int, error) {
	out, err := r.run(ctx, "rev-list", "--count", "--end-of-options", rev)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// Ref is a branch or tag and the commit it points at.
type Ref struct {
	Name, Hash, Subject string
	When                time.Time
}

// Refs lists "heads" or "tags", newest first. Annotated tags resolve to
// their commit.
func (r Repo) Refs(ctx context.Context, kind string) ([]Ref, error) {
	out, err := r.run(ctx, "for-each-ref", "--sort=-creatordate",
		"--format=%(refname:lstrip=2)%1f%(if)%(*objectname)%(then)%(*objectname)%(else)%(objectname)%(end)%1f%(creatordate:unix)%1f%(contents:subject)%1e",
		"refs/"+kind)
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) != 4 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		refs = append(refs, Ref{Name: f[0], Hash: f[1], When: time.Unix(at, 0).UTC(), Subject: f[3]})
	}
	return refs, nil
}

// LastChanges maps every path (files and directories) to the newest commit
// reachable from rev that touched it, in one pass over history.
func (r Repo) LastChanges(ctx context.Context, rev string) (map[string]Commit, error) {
	out, err := r.run(ctx, "log", "-z", "--name-only", "--format=%x1e%H%x1f%an%x1f%at%x1f%s", "--end-of-options", rev)
	if err != nil {
		return nil, err
	}
	last := map[string]Commit{}
	for _, rec := range strings.Split(string(out), "\x1e")[1:] {
		head, files, _ := strings.Cut(rec, "\x00")
		f := strings.Split(head, "\x1f")
		if len(f) != 4 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		c := Commit{Hash: f[0], Author: f[1], When: time.Unix(at, 0).UTC(), Subject: f[3]}
		for _, p := range strings.Split(strings.TrimLeft(files, "\n"), "\x00") {
			for ; p != "" && p != "."; p = path.Dir(p) {
				if _, ok := last[p]; ok {
					break // set by a newer commit, and so are its parents
				}
				last[p] = c
			}
		}
	}
	return last, nil
}

// Stat is a commit's line counts against its first parent.
type Stat struct{ Added, Deleted int }

var (
	insRe = regexp.MustCompile(`(\d+) insertion`)
	delRe = regexp.MustCompile(`(\d+) deletion`)
)

// ShortStats returns line counts for the newest n commits on rev, measured
// against the first parent like the commit pages.
func (r Repo) ShortStats(ctx context.Context, rev string, n int) (map[string]Stat, error) {
	out, err := r.run(ctx, "log", "-n", strconv.Itoa(n), "--format=%x1e%H", "--shortstat", "--diff-merges=first-parent", "--end-of-options", rev)
	if err != nil {
		return nil, err
	}
	stats := map[string]Stat{}
	for _, rec := range strings.Split(string(out), "\x1e")[1:] {
		hash, rest, _ := strings.Cut(rec, "\n")
		var s Stat
		if m := insRe.FindStringSubmatch(rest); m != nil {
			s.Added, _ = strconv.Atoi(m[1])
		}
		if m := delRe.FindStringSubmatch(rest); m != nil {
			s.Deleted, _ = strconv.Atoi(m[1])
		}
		stats[strings.TrimSpace(hash)] = s
	}
	return stats, nil
}

// CommitsSince returns the commits on rev since a time, newest first, with
// hash, author, time (UTC) and subject only.
func (r Repo) CommitsSince(ctx context.Context, rev string, since time.Time) ([]Commit, error) {
	out, err := r.run(ctx, "log", "--format=%H%x1f%an%x1f%at%x1f%s%x1e", "--since="+since.UTC().Format(time.RFC3339), "--end-of-options", rev)
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) != 4 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		cs = append(cs, Commit{Hash: f[0], Author: f[1], When: time.Unix(at, 0).UTC(), Subject: f[3]})
	}
	return cs, nil
}
