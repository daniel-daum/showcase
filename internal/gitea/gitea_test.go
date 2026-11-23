package gitea

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestTokenOnlyForPackages proves the token goes to the package listing and
// nowhere else, including redirects.
func TestTokenOnlyForPackages(t *testing.T) {
	var mu sync.Mutex
	authed := map[string]string{} // path -> Authorization header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authed[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/repos/daniel/a":
			w.Write([]byte(`{"name":"a"}`))
		case r.URL.Path == "/api/v1/packages/daniel":
			w.Write([]byte(`[{"name":"p","type":"npm","repository":{"name":"a"}},{"name":"q","type":"npm","repository":{"name":"secret","private":true}},{"name":"u","type":"generic"}]`))
		case r.URL.Path == "/avatars/x":
			w.Write([]byte("\x89PNG\r\n\x1a\n"))
		case strings.HasPrefix(r.URL.Path, "/api/v1/repos/daniel/a/"):
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	c := New(srv.URL, "daniel")
	c.SetToken("s3cret")
	if _, err := c.Repo(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	c.Issues(ctx, "a")
	c.Pulls(ctx, "a")
	c.Comments(ctx, "a", 1)
	c.Releases(ctx, "a")
	c.Languages(ctx, "a")
	c.User(ctx)
	c.Avatar(ctx, srv.URL+"/avatars/x")
	pkgs, err := c.Packages(ctx, []string{"a", "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "p" {
		t.Errorf("packages = %+v, want only p (linked to a public, listed repo)", pkgs)
	}
	for p, a := range authed {
		want := ""
		if p == "/api/v1/packages/daniel" {
			want = "token s3cret"
		}
		if a != want {
			t.Errorf("%s got Authorization %q, want %q", p, a, want)
		}
	}
	if authed["/api/v1/packages/daniel"] == "" {
		t.Error("packages call never carried the token")
	}

	// asking for the token on any other path fails before a request is sent
	for _, p := range []string{"/api/v1/repos/daniel/a", "/api/v1/packages/daniel/npm/p", "/api/v1/packages/danielx?limit=1", "/api/v1/user"} {
		delete(authed, strings.Split(p, "?")[0])
		if _, err := c.fetch(ctx, p, true); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("fetch(%s, auth) = %v, want a refusal", p, err)
		}
		if _, sent := authed[strings.Split(p, "?")[0]]; sent {
			t.Errorf("fetch(%s, auth) reached the server", p)
		}
	}
}

// TestTokenNotFollowedOnRedirect: a redirect from the packages API is an
// error, so the token is never replayed to another URL.
func TestTokenNotFollowedOnRedirect(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			hit = true
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	c := New(srv.URL, "daniel")
	c.SetToken("s3cret")
	if _, err := c.Packages(context.Background(), []string{"a"}); err == nil {
		t.Error("redirect was followed")
	}
	if hit {
		t.Error("the redirect target was requested")
	}
}

func TestNoTokenIsAnonymous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "" {
			t.Errorf("%s carried %q without a token", r.URL.Path, a)
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	New(srv.URL, "daniel").Packages(context.Background(), []string{"a"})
}
