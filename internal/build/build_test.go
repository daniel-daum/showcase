package build

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"daum.dev/showcase/internal/config"
	"daum.dev/showcase/internal/gitea"
)

var (
	statusRe = regexp.MustCompile(`/commits/[^/]+/status$`)
	// paths the fake Gitea answers with 401, like Gitea 28's packages API
	needToken = map[string]bool{}
	update    = flag.Bool("update", false, "rewrite golden files")
	keep      = flag.String("keep", "", "also render the fixture site into this directory")
)

// gitEnv isolates fixture commits from the user's git config (no signing).
var gitEnv = []string{
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	"GIT_AUTHOR_NAME=Daniel", "GIT_AUTHOR_EMAIL=d@example.com",
	"GIT_COMMITTER_NAME=Daniel", "GIT_COMMITTER_EMAIL=d@example.com",
}

func run(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), gitEnv...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// fixtureRepos creates bare repos under root/daniel: repo-a from
// testdata/repo-a in two commits with fixed dates, and an empty repo-b.
func fixtureRepos(t *testing.T) string {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.CopyFS(work, os.DirFS("../../testdata/repo-a")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(work, "data.bin"), []byte("bin\x00ary"), 0o644)
	os.WriteFile(filepath.Join(work, "big.txt"), bytes.Repeat([]byte("0123456789abcde\n"), 40<<10), 0o644)
	run(t, work, "", "init", "-q", "-b", "main")
	run(t, work, "", "add", "-A", ":!CHANGELOG.md")
	run(t, work, "2026-06-01T10:00:00Z", "commit", "-q", "--no-gpg-sign", "-m", "Initial commit")
	// a few months of small commits, so the activity views have a shape
	start := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	subjects := []string{"Tidy the guide", "Document the flags", "Fix a typo in the notes", "Add a section on deploys", "Explain the image tags"}
	var notes strings.Builder
	for i, day := range []int{0, 1, 9, 23, 30, 31, 58, 59, 61, 86, 100, 114, 115, 116, 128, 135, 142, 149, 156, 156, 156, 156, 157, 158, 160} {
		fmt.Fprintf(&notes, "- note %d\n", i+1)
		os.WriteFile(filepath.Join(work, "docs/notes.md"), []byte("# Notes\n\n"+notes.String()), 0o644)
		run(t, work, "", "add", "docs/notes.md")
		when := start.AddDate(0, 0, day).Add(time.Duration(i) * 7 * time.Minute)
		run(t, work, when.Format(time.RFC3339), "commit", "-q", "--no-gpg-sign", "-m", subjects[i%len(subjects)])
	}
	os.WriteFile(filepath.Join(work, "CHANGELOG.md"), []byte("# Changes\n"), 0o644)
	os.WriteFile(filepath.Join(work, "cmd/hello/main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello, world\") }\n"), 0o644)
	os.WriteFile(filepath.Join(work, "data.bin"), []byte("bin\x00ary2"), 0o644)
	run(t, work, "", "add", "-A")
	run(t, work, "2026-11-21T08:30:00Z", "commit", "-q", "--no-gpg-sign", "-m", "Add changelog", "-m", "With a body.")
	run(t, work, "", "tag", "v0.1.0", "HEAD~26")
	run(t, work, "2026-11-21T09:00:00Z", "tag", "-a", "-m", "v0.2.0", "v0.2.0")
	run(t, work, "", "branch", "feature/flag")
	run(t, root, "", "clone", "-q", "--bare", work, "daniel/repo-a.git")
	run(t, root, "", "init", "-q", "--bare", "-b", "main", "daniel/repo-b.git")
	return root
}

// fakeGitea serves testdata/gitea/<path>.json, a generated avatar and the
// fixture repos over smart HTTP. It fails the test if a request carries
// credentials.
func fakeGitea(t *testing.T) *httptest.Server {
	t.Helper()
	root := fixtureRepos(t)
	rev, err := exec.Command("git", "--git-dir", filepath.Join(root, "daniel/repo-a.git"), "rev-parse", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	registry := fakeRegistry(strings.TrimSpace(string(rev)))
	gitBackend := &cgi.Handler{
		Path: "git",
		Args: []string{"http-backend"},
		Env:  append([]string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}, gitEnv...),
	}
	if p, err := exec.LookPath("git"); err == nil {
		gitBackend.Path = p
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the registry's own anonymous pull token is the one allowed header
		if a := r.Header.Get("Authorization"); a != "" && !(strings.HasPrefix(r.URL.Path, "/v2/") && a == "Bearer "+anonToken) || r.Header.Get("Cookie") != "" {
			t.Errorf("request to %s carried credentials", r.URL)
		}
		if strings.HasPrefix(r.URL.Path, "/v2/") {
			registry(w, r)
			return
		}
		if needToken[r.URL.Path] {
			http.Error(w, `{"message":"token is required"}`, http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.URL.Path, ".git/") {
			gitBackend.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/avatars/") {
			var buf bytes.Buffer
			png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 2)))
			w.Write(buf.Bytes())
			return
		}
		p := statusRe.ReplaceAllString(r.URL.Path, "/status") // any commit's status
		b, err := os.ReadFile(filepath.Join("../../testdata/gitea", filepath.FromSlash(p)+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testBuilder(t *testing.T) *Builder {
	srv := fakeGitea(t)
	cfg := &config.Config{
		GiteaURL: srv.URL,
		User:     "daniel",
		SiteURL:  "https://code.danieldaum.net",
		Links: map[string]string{
			"github":  "https://github.com/example",
			"website": "https://danieldaum.net",
			"atproto": "https://bsky.app/profile/daum.dev",
		},
		Repos:      []string{"repo-b", "leaky", "missing", "repo-a"},
		Images:     []string{"repo-a"},
		MaxCommits: 50,
		Theme:      "moss",
	}
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	return &Builder{
		Cfg:   cfg,
		Gitea: gitea.New(srv.URL, cfg.User),
		Data:  t.TempDir(),
		Now:   func() time.Time { return now },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func checkGolden(t *testing.T, dir, rel string) []byte {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("../../testdata/golden", rel)
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from %s; run `just golden` and review the diff", rel, golden)
	}
	bad := []string{"leaky", "should never render", "should-never-render", "unlisted-image", "sha256:0123", "<script>", "tailnet.example"}
	if !strings.Contains(rel, "/blob/") && !strings.Contains(rel, "commit") { // source text, escaped
		bad = append(bad, "javascript:", "tracker.example")
	}
	for _, bad := range bad {
		if bytes.Contains(got, []byte(bad)) {
			t.Errorf("%s contains %q", rel, bad)
		}
	}
	return got
}

func TestSiteGolden(t *testing.T) {
	dir := t.TempDir()
	if *keep != "" {
		dir = *keep
		os.RemoveAll(dir)
		os.MkdirAll(dir, 0o755)
	}
	if err := testBuilder(t).Render(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"index.html", "repos/index.html", "repo-a/index.html", "repo-b/index.html",
		"repo-a/tree/docs/index.html", "repo-a/tree/cmd/hello/index.html",
		"repo-a/blob/cmd/hello/main.go.html", "repo-a/blob/README.md.html",
		"repo-a/blob/docs/logo.png.html", "repo-a/blob/data.bin.html", "repo-a/blob/big.txt.html",
		"repo-a/log/index.html", "repo-b/log/index.html",
		"repo-a/branches/index.html", "repo-a/tags/index.html",
		"repo-a/releases/index.html", "repo-a/pulls/index.html", "repo-a/packages/index.html",
		"repo-b/releases/index.html",
	} {
		checkGolden(t, dir, rel)
	}
	commits, _ := filepath.Glob(filepath.Join(dir, "repo-a/commit/*/index.html"))
	if len(commits) != 27 {
		t.Fatalf("want 27 commit pages, got %d", len(commits))
	}
	for _, c := range commits {
		b, _ := os.ReadFile(c)
		switch {
		case bytes.Contains(b, []byte(">Add changelog</h2>")):
			os.WriteFile(filepath.Join(dir, "repo-a/commit-latest.html"), b, 0o644) // hash-free golden name; a copy, so -keep output keeps the page
			checkGolden(t, dir, "repo-a/commit-latest.html")
		case bytes.Contains(b, []byte(">Initial commit</h2>")) && !bytes.Contains(b, []byte("Diff truncated")): // 40k lines of big.txt
			t.Error("initial commit page: big.txt diff should be truncated")
		}
	}
	for _, f := range []string{"docs/logo.png", "go.mod"} {
		want, _ := os.ReadFile(filepath.Join("../../testdata/repo-a", f))
		got, err := os.ReadFile(filepath.Join(dir, "repo-a/raw", f))
		if err != nil || (want != nil && !bytes.Equal(got, want)) {
			t.Errorf("raw %s: wrong or missing (%v)", f, err)
		}
	}
	for _, f := range []string{"static/style.css", "static/avatar-65da4c2ded.png", "static/theme.js", "static/fonts/geistpixel/GeistPixel-Square.woff2", "static/fonts/geistmono/GeistMono-Regular.woff2"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
	for _, f := range []string{"leaky", "missing"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s was rendered", f)
		}
	}
}

func TestPackagesNeedToken(t *testing.T) {
	needToken["/api/v1/packages/daniel"] = true
	defer delete(needToken, "/api/v1/packages/daniel")
	dir := t.TempDir()
	if err := testBuilder(t).Render(context.Background(), dir); err != nil {
		t.Fatalf("build failed when the packages API wants a token: %v", err)
	}
	// images still come from the registry; API-only packages (npm) are gone
	b, _ := os.ReadFile(filepath.Join(dir, "repo-a/packages/index.html"))
	if !bytes.Contains(b, []byte("code.danieldaum.net/daniel/repo-a")) || bytes.Contains(b, []byte("@daniel/repo-a")) {
		t.Error("packages page should list the registry image and nothing from the API")
	}
}
