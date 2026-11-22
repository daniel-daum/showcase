package build

import (
	"bytes"
	"context"
	"flag"
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
	"strings"
	"testing"
	"time"

	"daum.dev/showcase/internal/config"
	"daum.dev/showcase/internal/gitea"
)

var (
	update = flag.Bool("update", false, "rewrite golden files")
	keep   = flag.String("keep", "", "also render the fixture site into this directory")
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
	run(t, work, "", "init", "-q", "-b", "main")
	run(t, work, "", "add", "-A", ":!CHANGELOG.md")
	run(t, work, "2026-06-01T10:00:00Z", "commit", "-q", "--no-gpg-sign", "-m", "Initial commit")
	os.WriteFile(filepath.Join(work, "CHANGELOG.md"), []byte("# Changes\n"), 0o644)
	run(t, work, "", "add", "CHANGELOG.md")
	run(t, work, "2026-11-19T08:30:00Z", "commit", "-q", "--no-gpg-sign", "-m", "Add changelog", "-m", "With a body.")
	run(t, root, "", "clone", "-q", "--bare", work, "daniel/repo-a.git")
	run(t, root, "", "init", "-q", "--bare", "-b", "main", "daniel/repo-b.git")
	return root
}

// fakeGitea serves testdata/gitea/<path>.json, a generated avatar and the
// fixture repos over smart HTTP. It fails the test if a request carries
// credentials.
func fakeGitea(t *testing.T) *httptest.Server {
	t.Helper()
	gitBackend := &cgi.Handler{
		Path: "git",
		Args: []string{"http-backend"},
		Env:  append([]string{"GIT_PROJECT_ROOT=" + fixtureRepos(t), "GIT_HTTP_EXPORT_ALL=1"}, gitEnv...),
	}
	if p, err := exec.LookPath("git"); err == nil {
		gitBackend.Path = p
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("request to %s carried credentials", r.URL)
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
		b, err := os.ReadFile(filepath.Join("../../testdata/gitea", filepath.FromSlash(r.URL.Path)+".json"))
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
		SiteURL:  "https://daum.dev",
		Links: map[string]string{
			"github":  "https://github.com/example",
			"website": "https://danieldaum.net",
			"atproto": "https://bsky.app/profile/daum.dev",
		},
		Repos:      []string{"repo-b", "leaky", "missing", "repo-a"},
		MaxCommits: 50,
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
	for _, bad := range []string{"leaky", "should never render", "<script>", "tailnet.example", "javascript:", "tracker.example"} {
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
	for _, rel := range []string{"index.html", "repo-a/index.html", "repo-b/index.html"} {
		checkGolden(t, dir, rel)
	}
	for _, f := range []string{"static/style.css", "static/avatar.png", "static/fonts/GeistPixel-Square.woff2"} {
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
