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
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daum.dev/showcase/internal/config"
	"daum.dev/showcase/internal/gitea"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fakeGitea serves testdata/gitea/<path>.json and a generated avatar. It
// fails the test if a request carries credentials.
func fakeGitea(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("request to %s carried credentials", r.URL)
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
		Now:   func() time.Time { return now },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestProfileGolden(t *testing.T) {
	dir := t.TempDir()
	if err := testBuilder(t).Render(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	golden := "../../testdata/golden/index.html"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("index.html differs from %s; run with -update and review the diff", golden)
	}

	for _, bad := range []string{"leaky", "should never render", "<script>", "tailnet.example"} {
		if bytes.Contains(got, []byte(bad)) {
			t.Errorf("index.html contains %q", bad)
		}
	}
	for _, f := range []string{"static/style.css", "static/avatar.png", "static/fonts/GeistPixel-Square.woff2"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
}
