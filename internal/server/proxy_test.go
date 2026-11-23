package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// upstream is a fake Gitea that records what reaches it.
type upstream struct {
	got  []*http.Request
	resp func(w http.ResponseWriter, r *http.Request)
}

func proxyServer(t *testing.T, u *upstream, ready bool) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.got = append(u.got, r.Clone(r.Context()))
		if u.resp != nil {
			u.resp(w, r)
			return
		}
		w.Header().Set("Set-Cookie", "i_like_gitea=1")
		w.Header().Set("X-Gitea-Debug", "yes")
		w.Header().Set("Www-Authenticate", `Bearer realm="http://gitea.tailnet.example:3000/v2/token",service="container_registry",scope="*"`)
		io.WriteString(w, "upstream "+r.URL.RequestURI())
	}))
	t.Cleanup(srv.Close)
	s := testServer(t, ready)
	cfg := *s.Cfg
	cfg.GiteaURL = srv.URL
	return New(&cfg, s.Root, s.Ready, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestProxyRoutes(t *testing.T) {
	u := &upstream{}
	s := proxyServer(t, u, true)
	pack := map[string]string{"Content-Type": "application/x-git-upload-pack-request"}
	cases := []struct {
		method, path string
		hdr          map[string]string
		status       int
		upstream     string // expected upstream RequestURI, "" if it must not be reached
	}{
		{"GET", "/repo-a.git/info/refs?service=git-upload-pack", nil, 200, "/daniel/repo-a.git/info/refs?service=git-upload-pack"},
		{"POST", "/repo-a.git/git-upload-pack", pack, 200, "/daniel/repo-a.git/git-upload-pack"},
		{"GET", "/v2/", nil, 200, "/v2/"},
		{"HEAD", "/v2/", nil, 200, "/v2/"},
		{"GET", "/v2/token?scope=repository:daniel/repo-a:pull&service=container_registry", nil, 200, "/v2/token?scope=repository:daniel/repo-a:pull&service=container_registry"},
		{"GET", "/v2/daniel/repo-a/manifests/latest", nil, 200, "/v2/daniel/repo-a/manifests/latest"},
		{"HEAD", "/v2/daniel/tools/cli/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, 200, "/v2/daniel/tools/cli/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"GET", "/v2/daniel/repo-a/manifests/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, 200, "/v2/daniel/repo-a/manifests/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"GET", "/repo-a/releases/download/v1.0/app.tar.gz", nil, 200, "/daniel/repo-a/releases/download/v1.0/app.tar.gz"},

		// push and anything else on these prefixes never reaches Gitea
		{"GET", "/repo-a.git/info/refs?service=git-receive-pack", nil, 403, ""},
		{"POST", "/repo-a.git/git-receive-pack", nil, 403, ""},
		{"GET", "/repo-a.git/info/refs", nil, 404, ""},
		{"GET", "/repo-a.git/info/refs?service=git-upload-pack&x=1", nil, 404, ""},
		{"GET", "/repo-a.git/HEAD", nil, 404, ""},
		{"POST", "/repo-a.git/git-upload-pack", nil, 404, ""},
		{"GET", "/secret.git/info/refs?service=git-upload-pack", nil, 404, ""},
		{"GET", "/repo-a.git/../secret.git/info/refs?service=git-upload-pack", nil, 404, ""},
		{"PUT", "/v2/daniel/repo-a/manifests/latest", nil, 404, ""},
		{"POST", "/v2/daniel/repo-a/blobs/uploads/", nil, 404, ""},
		{"PATCH", "/v2/daniel/repo-a/blobs/uploads/x", nil, 404, ""},
		{"DELETE", "/v2/daniel/repo-a/manifests/latest", nil, 404, ""},
		{"GET", "/v2/token?scope=repository:daniel/repo-a:pull,push", nil, 404, ""},
		{"GET", "/v2/token?scope=repository:daniel/secret:pull", nil, 404, ""},
		{"GET", "/v2/daniel/secret/manifests/latest", nil, 404, ""},
		{"GET", "/v2/other/repo-a/manifests/latest", nil, 404, ""},
		{"GET", "/v2/_catalog", nil, 404, ""},
		{"GET", "/v2/daniel/repo-a/blobs/uploads", nil, 404, ""},
		{"GET", "/v2/daniel/repo-a/blobs/sha256:abc", nil, 404, ""},
		{"GET", "/v2/daniel/repo-a/manifests/-bad", nil, 404, ""},
		{"GET", "/v2/daniel/repo-a/tags/list", nil, 404, ""},
		{"GET", "/secret/releases/download/v1/x", nil, 404, ""},
		{"GET", "/repo-a/releases/download/v1/a/b", nil, 404, ""},
		{"GET", "/api/v1/repos/daniel/repo-a", nil, 404, ""},
		{"GET", "/daniel/repo-a.git/info/refs?service=git-upload-pack", nil, 404, ""},
	}
	for _, c := range cases {
		u.got = nil
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Cookie", "session=abc")
		req.Header.Set("Authorization", "Basic ZGFuaWVsOnNlY3JldA==")
		for k, v := range c.hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, w.Code, c.status)
		}
		switch {
		case c.upstream == "" && len(u.got) > 0:
			t.Errorf("%s %s: reached upstream as %s", c.method, c.path, u.got[0].URL.RequestURI())
		case c.upstream != "" && len(u.got) != 1:
			t.Errorf("%s %s: did not reach upstream", c.method, c.path)
		case c.upstream != "":
			g := u.got[0]
			if g.URL.RequestURI() != c.upstream {
				t.Errorf("%s %s: upstream got %s, want %s", c.method, c.path, g.URL.RequestURI(), c.upstream)
			}
			if g.Header.Get("Cookie") != "" || g.Header.Get("X-Forwarded-For") != "" {
				t.Errorf("%s %s: forwarded cookie or proxy headers", c.method, c.path)
			}
			if g.Header.Get("Authorization") != "" { // the test sends Basic credentials
				t.Errorf("%s %s: forwarded credentials", c.method, c.path)
			}
			h := w.Header()
			if h.Get("Set-Cookie") != "" || h.Get("X-Gitea-Debug") != "" {
				t.Errorf("%s %s: leaked upstream headers", c.method, c.path)
			}
			if a := h.Get("Www-Authenticate"); strings.Contains(a, "tailnet") || !strings.Contains(a, `realm="https://code.danieldaum.net/v2/token"`) {
				t.Errorf("%s %s: realm not rewritten: %s", c.method, c.path, a)
			}
		}
	}
}

func TestProxyBeforeFirstBuild(t *testing.T) {
	u := &upstream{}
	s := proxyServer(t, u, false)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/repo-a.git/info/refs?service=git-upload-pack", nil))
	if w.Code != 200 || len(u.got) != 1 {
		t.Errorf("clone route before first build: %d", w.Code)
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v2/daniel/repo-a/manifests/latest", nil)
	req.Header.Set("Authorization", "Bearer anon-token")
	s.ServeHTTP(w, req)
	if len(u.got) != 2 || u.got[1].Header.Get("Authorization") != "Bearer anon-token" {
		t.Error("registry bearer token was not forwarded")
	}
}

func TestProxyResponses(t *testing.T) {
	u := &upstream{}
	s := proxyServer(t, u, true)

	u.resp = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<script>alert(1)</script>")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/repo-a/releases/download/v1/x.html", nil))
	if w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("Content-Disposition") != "attachment" {
		t.Errorf("release asset served as %q", w.Header().Get("Content-Type"))
	}

	u.resp = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://minio.tailnet.example/bucket/blob", http.StatusTemporaryRedirect)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/v2/daniel/repo-a/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil))
	if w.Code != http.StatusBadGateway || strings.Contains(w.Header().Get("Location"), "tailnet") {
		t.Errorf("upstream redirect passed through: %d %s", w.Code, w.Header().Get("Location"))
	}

	u.resp = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate", `Basic realm="Gitea"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/repo-a.git/info/refs?service=git-upload-pack", nil))
	if w.Header().Get("Www-Authenticate") != "" {
		t.Error("Basic challenge passed through; git would prompt for credentials")
	}

	u.resp = func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body) }
	big := strings.NewReader(strings.Repeat("x", maxUploadPack+1))
	req := httptest.NewRequest("POST", "/repo-a.git/git-upload-pack", big)
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Error("oversized upload-pack body was accepted")
	}
}

// TestCloneAndPush runs real git through the proxy: clone works, push fails.
func TestCloneAndPush(t *testing.T) {
	git := func(dir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("git %v: %s", args, out)
		}
		return err
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	os.MkdirAll(work, 0o755)
	os.WriteFile(filepath.Join(work, "f"), []byte("hi\n"), 0o644)
	if git(work, "init", "-q", "-b", "main") != nil || git(work, "add", "f") != nil ||
		git(work, "commit", "-q", "--no-gpg-sign", "-m", "c") != nil ||
		git(root, "clone", "-q", "--bare", work, "daniel/repo-a.git") != nil ||
		git(filepath.Join(root, "daniel/repo-a.git"), "config", "http.receivepack", "true") != nil {
		t.Fatal("fixture setup failed")
	}
	gitPath, _ := exec.LookPath("git")
	backend := &cgi.Handler{Path: gitPath, Args: []string{"http-backend"},
		Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	up := httptest.NewServer(backend)
	defer up.Close()

	s := testServer(t, true)
	cfg := *s.Cfg
	cfg.GiteaURL = up.URL
	site := httptest.NewServer(New(&cfg, s.Root, s.Ready, s.Log))
	defer site.Close()

	clone := filepath.Join(root, "clone")
	if err := git(root, "clone", "-q", site.URL+"/repo-a.git", clone); err != nil {
		t.Fatal("clone through the proxy failed")
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f")); string(b) != "hi\n" {
		t.Fatal("clone has wrong content")
	}
	os.WriteFile(filepath.Join(clone, "f"), []byte("changed\n"), 0o644)
	git(clone, "commit", "-qam", "push attempt", "--no-gpg-sign")
	if err := git(clone, "push", "-q", "origin", "main"); err == nil {
		t.Fatal("push through the proxy succeeded")
	}
	if err := git(root, "clone", "-q", site.URL+"/secret.git", filepath.Join(root, "nope")); err == nil {
		t.Fatal("cloned a repo that is not allowlisted")
	}
}

// Clones, pulls and downloads stay at the root under a base_path.
func TestProxyRoutesIgnoreBasePath(t *testing.T) {
	u := &upstream{}
	s := proxyServer(t, u, true)
	s.Cfg.BasePath = "/projects"
	for path, want := range map[string]string{
		"/repo-a.git/info/refs?service=git-upload-pack": "/daniel/repo-a.git/info/refs?service=git-upload-pack",
		"/v2/": "/v2/",
		"/repo-a/releases/download/v1.0/app.tar.gz": "/daniel/repo-a/releases/download/v1.0/app.tar.gz",
	} {
		u.got = nil
		w := do(s, "GET", path, nil)
		if w.Code != 200 || len(u.got) != 1 || u.got[0].RequestURI != want {
			t.Errorf("GET %s: %d, upstream %v, want %q", path, w.Code, u.got, want)
		}
	}
}

// TestProxyDropsHeaders: only forwardHeaders reach Gitea, so headers Gitea
// might trust (reverse-proxy auth, its own X-Gitea-*) can't be injected.
func TestProxyDropsHeaders(t *testing.T) {
	u := &upstream{}
	s := proxyServer(t, u, true)
	req := httptest.NewRequest("GET", "/repo-a.git/info/refs?service=git-upload-pack", nil)
	for k, v := range map[string]string{
		"X-WEBAUTH-USER": "daniel", "X-Forwarded-User": "daniel", "X-Gitea-OTP": "1", "Cookie": "i_like_gitea=1",
		"Authorization": "Basic ZGFuaWVsOnB3", "Proxy-Authorization": "Basic eA==", "Git-Protocol": "version=2",
	} {
		req.Header.Set(k, v)
	}
	s.ServeHTTP(httptest.NewRecorder(), req)
	if len(u.got) != 1 {
		t.Fatalf("upstream got %d requests", len(u.got))
	}
	for k := range u.got[0].Header {
		if !slices.Contains(forwardHeaders, k) {
			t.Errorf("header %s reached Gitea", k)
		}
	}
	if u.got[0].Header.Get("Authorization") != "" {
		t.Error("credentials reached Gitea on a git route")
	}
	if u.got[0].Header.Get("Git-Protocol") != "version=2" {
		t.Error("Git-Protocol was dropped; protocol v2 clones need it")
	}
}

// TestProxyGitSlots: past maxGitProxied concurrent git requests, more get
// a 503 instead of piling onto Gitea.
func TestProxyGitSlots(t *testing.T) {
	s := proxyServer(t, &upstream{}, true)
	for range maxGitProxied {
		s.gitSlots <- struct{}{}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/repo-a.git/info/refs?service=git-upload-pack", nil))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("full slots: got %d", w.Code)
	}
	<-s.gitSlots
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/repo-a.git/info/refs?service=git-upload-pack", nil))
	if w.Code != http.StatusOK {
		t.Errorf("free slot: got %d", w.Code)
	}
}
