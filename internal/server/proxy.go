package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maxUploadPack = 10 << 20
	maxGitProxied = 8 // concurrent clones and fetches; more get a 503
)

// forwardHeaders are the only request headers that reach Gitea. Anything
// else (cookies, proxy-auth headers like X-WEBAUTH-USER, X-Forwarded-*,
// Gitea's own X-Gitea-*) is dropped, so a visitor can't speak to Gitea as
// anyone but an anonymous client.
var forwardHeaders = []string{
	"Accept", "Accept-Encoding", "Content-Type", "Content-Encoding", "Git-Protocol",
	"User-Agent", "Range", "If-Range", "If-None-Match", "If-Modified-Since", "Authorization",
}

var (
	realmRe  = regexp.MustCompile(`realm="[^"]*"`)
	digestRe = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	refRe    = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9._-]{0,127}|sha256:[a-f0-9]{64})$`)
)

// newProxy forwards allowlisted git, registry and release-download requests
// to Gitea. It never forwards cookies or proxy headers, refuses upstream
// redirects, and strips cookies and Gitea headers from responses.
func (s *Server) newProxy() *httputil.ReverseProxy {
	up, _ := url.Parse(s.Cfg.GiteaURL)
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy, t.ResponseHeaderTimeout = nil, 60*time.Second
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Path, pr.Out.URL.RawPath = pr.In.Context().Value(upstreamPath{}).(string), ""
			pr.SetURL(up)
			pr.Out.Host = up.Host
			for k := range pr.Out.Header {
				if !slices.Contains(forwardHeaders, http.CanonicalHeaderKey(k)) {
					pr.Out.Header.Del(k)
				}
			}
			// only the registry's own (anonymous) bearer tokens pass, never credentials
			a := pr.In.Header.Get("Authorization")
			if !strings.HasPrefix(pr.In.URL.Path, "/v2/") || pr.In.URL.Path == "/v2/token" || !strings.HasPrefix(a, "Bearer ") {
				pr.Out.Header.Del("Authorization")
			}
		},
		FlushInterval: -1, // stream clones and layers
		Transport:     t,
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				return errors.New("upstream redirect refused") // would leak a tailnet or storage URL
			}
			h := resp.Header
			h.Del("Set-Cookie")
			for k := range h {
				if lk := strings.ToLower(k); strings.HasPrefix(lk, "x-gitea") || strings.HasPrefix(lk, "x-forgejo") {
					h.Del(k)
				}
			}
			// point the registry's token realm at us; drop other challenges so
			// clients never prompt for (or send) credentials
			if v := h.Get("Www-Authenticate"); strings.HasPrefix(v, "Bearer ") {
				h.Set("Www-Authenticate", realmRe.ReplaceAllLiteralString(v, `realm="`+s.Cfg.SiteURL+`/v2/token"`))
			} else {
				h.Del("Www-Authenticate")
			}
			if strings.Contains(resp.Request.URL.Path, "/releases/download/") || strings.Contains(resp.Request.URL.Path, "/blobs/") {
				// uploaded assets and image layers could be anything, including HTML
				h.Set("Content-Type", "application/octet-stream")
				h.Set("Content-Disposition", "attachment")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.Log.Warn("proxy", "path", r.URL.Path, "err", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
}

type upstreamPath struct{}

// proxyRoute handles the pass-through routes and reports whether p was one.
// Within them anything not explicitly allowed is refused.
func (s *Server) proxyRoute(w http.ResponseWriter, r *http.Request) bool {
	p, user := r.URL.Path, s.Cfg.User
	if !clean(p) {
		return false // route() turns it into a 404
	}
	first, rest, _ := strings.Cut(strings.TrimPrefix(p, "/"), "/")
	get := r.Method == http.MethodGet || r.Method == http.MethodHead
	noBody := r.ContentLength == 0 && len(r.TransferEncoding) == 0
	var target string

	switch {
	case strings.Contains(p, "git-receive-pack") || strings.Contains(r.URL.RawQuery, "git-receive-pack"):
		http.Error(w, "this is a read-only mirror", http.StatusForbidden)
		return true

	case strings.HasSuffix(first, ".git"): // smart HTTP clone
		repo := strings.TrimSuffix(first, ".git")
		switch {
		case !s.Cfg.HasRepo(repo):
		case rest == "info/refs" && r.Method == http.MethodGet && r.URL.RawQuery == "service=git-upload-pack" && noBody:
			target = "/" + user + "/" + repo + ".git/info/refs"
		case rest == "git-upload-pack" && r.Method == http.MethodPost &&
			r.Header.Get("Content-Type") == "application/x-git-upload-pack-request":
			r.Body = http.MaxBytesReader(w, r.Body, maxUploadPack)
			target = "/" + user + "/" + repo + ".git/git-upload-pack"
		}

	case first == "v2": // container registry, pull only
		switch {
		case !get || !noBody:
		case rest == "":
			target = "/v2/"
		case rest == "token" && s.pullOnly(r.URL.Query()["scope"]):
			target = "/v2/token"
		default:
			if sub, ok := strings.CutPrefix(rest, user+"/"); ok {
				if image, ok := s.registryImage(sub); ok {
					target = "/v2/" + user + "/" + image + p[len("/v2/"+user+"/"+image):]
				}
			}
		}

	case s.Cfg.HasRepo(first) && strings.HasPrefix(rest, "releases/download/"):
		parts := strings.Split(strings.TrimPrefix(rest, "releases/download/"), "/")
		if get && noBody && len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			target = "/" + user + "/" + first + "/" + rest
		}

	default:
		return false
	}

	if target == "" {
		http.NotFound(w, r)
		return true
	}
	if strings.HasSuffix(first, ".git") { // upload-pack is the expensive one for Gitea
		select {
		case s.gitSlots <- struct{}{}:
			defer func() { <-s.gitSlots }()
		default:
			w.Header().Set("Retry-After", "30")
			http.Error(w, "busy, try again shortly", http.StatusServiceUnavailable)
			return true
		}
	}
	s.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), upstreamPath{}, target)))
	return true
}

// registryImage splits "<image>/(manifests|blobs)/<ref>" and checks the
// image against the allowlist. Image names may contain slashes.
func (s *Server) registryImage(rest string) (string, bool) {
	for kind, ok := range map[string]*regexp.Regexp{"/manifests/": refRe, "/blobs/": digestRe} {
		if i := strings.LastIndex(rest, kind); i > 0 {
			image, ref := rest[:i], rest[i+len(kind):]
			return image, ok.MatchString(ref) && s.Cfg.HasImage(image)
		}
	}
	return "", false
}

// pullOnly allows token requests whose scopes are all pulls of allowlisted images.
func (s *Server) pullOnly(scopes []string) bool {
	for _, sc := range scopes {
		image, ok := strings.CutPrefix(sc, "repository:"+s.Cfg.User+"/")
		if !ok || !strings.HasSuffix(image, ":pull") || !s.Cfg.HasImage(strings.TrimSuffix(image, ":pull")) {
			return false
		}
	}
	return true
}
