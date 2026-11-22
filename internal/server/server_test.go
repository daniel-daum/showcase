package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daum.dev/showcase/internal/config"
)

func testServer(t *testing.T, ready bool) *Server {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.html":                  "home",
		"404.html":                    "<h1>not found</h1>",
		"static/style.css":            "body{}",
		"static/fonts/x.woff2":        "font",
		"repo-a/index.html":           "repo page",
		"repo-a/tree/docs/index.html": "docs dir",
		"repo-a/blob/main.go.html":    "main.go page",
		"repo-a/raw/main.go":          "package main",
		"repo-a/raw/logo.png":         "\x89PNG",
		"repo-a/raw/evil.svg":         "<svg onload=alert(1)>",
		"repo-a/raw/page.html":        "<script>alert(1)</script>",
		"repo-a/raw/data.bin":         "bin\x00ary",
		"repo-a/log/index.html":       "log",
		"secret/index.html":           "unlisted repo left on disk",
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	return &Server{
		Cfg:   &config.Config{Repos: []string{"repo-a"}, TrustedProxies: []string{"10.0.0.1"}},
		Root:  func() string { return root },
		Ready: func() bool { return ready },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func do(s *Server, method, target string, body io.Reader) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, body)
	s.ServeHTTP(w, req)
	return w
}

func TestRoutes(t *testing.T) {
	s := testServer(t, true)
	cases := []struct {
		method, path string
		status       int
		ctype, body  string
	}{
		{"GET", "/", 200, "text/html", "home"},
		{"HEAD", "/", 200, "text/html", ""},
		{"GET", "/healthz", 200, "", "ok"},
		{"GET", "/static/style.css", 200, "text/css", "body{}"},
		{"GET", "/static/fonts/x.woff2", 200, "font/woff2", ""},
		{"GET", "/repo-a/", 200, "text/html", "repo page"},
		{"GET", "/repo-a", 301, "", ""},
		{"GET", "/repo-a/tree/docs", 301, "", ""},
		{"GET", "/repo-a/tree/docs/", 200, "text/html", "docs dir"},
		{"GET", "/repo-a/blob/main.go", 200, "text/html", "main.go page"},
		{"GET", "/repo-a/log/", 200, "text/html", "log"},
		{"GET", "/repo-a/sub/pkg?go-get=1", 200, "text/html", "repo page"},
		// raw: images as images, everything else as text or a download
		{"GET", "/repo-a/raw/main.go", 200, "text/plain", "package main"},
		{"GET", "/repo-a/raw/logo.png", 200, "image/png", ""},
		{"GET", "/repo-a/raw/evil.svg", 200, "text/plain", ""},
		{"GET", "/repo-a/raw/page.html", 200, "text/plain", ""},
		{"GET", "/repo-a/raw/data.bin", 200, "application/octet-stream", ""},
		// default 404: Gitea's UI and API, unlisted repos, odd paths
		{"GET", "/repo-a/blob/missing", 404, "text/html", "not found"},
		{"GET", "/secret/", 404, "", ""},
		{"GET", "/api/v1/users/daniel", 404, "", ""},
		{"GET", "/user/login", 404, "", ""},
		{"GET", "/daniel/repo-a", 404, "", ""},
		{"GET", "/repo-a/raw/../../secret/index.html", 404, "", ""},
		{"GET", "/repo-a//index.html", 404, "", ""},
		{"GET", "/static/", 404, "", ""},
		{"GET", "/index.html", 404, "", ""},
		{"POST", "/", 404, "", ""},
		{"PUT", "/repo-a/", 404, "", ""},
		{"DELETE", "/repo-a/", 404, "", ""},
	}
	for _, c := range cases {
		w := do(s, c.method, c.path, nil)
		if w.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, w.Code, c.status)
			continue
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.ctype) {
			t.Errorf("%s %s: content type %q, want %q", c.method, c.path, ct, c.ctype)
		}
		if !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s %s: body %q lacks %q", c.method, c.path, w.Body.String(), c.body)
		}
		if w.Header().Get("Content-Security-Policy") != csp || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
			w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s %s: missing security headers", c.method, c.path)
		}
	}
}

func TestBodyRejected(t *testing.T) {
	if w := do(testServer(t, true), "GET", "/", strings.NewReader("x")); w.Code != http.StatusBadRequest {
		t.Errorf("GET with body: %d", w.Code)
	}
}

func TestNotReady(t *testing.T) {
	s := testServer(t, false)
	for _, p := range []string{"/", "/healthz", "/repo-a/"} {
		if w := do(s, "GET", p, nil); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s before first build: %d, want 503", p, w.Code)
		}
	}
}

func TestClientIP(t *testing.T) {
	s := testServer(t, true)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.RemoteAddr = "192.0.2.9:5555"
	if ip := s.clientIP(req); ip != "192.0.2.9" {
		t.Errorf("untrusted peer: got %s", ip)
	}
	req.RemoteAddr = "10.0.0.1:5555"
	if ip := s.clientIP(req); ip != "1.2.3.4" {
		t.Errorf("trusted peer: got %s", ip)
	}
}
