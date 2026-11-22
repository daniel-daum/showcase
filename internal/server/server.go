// Package server serves the built site and, later, the git and registry
// pass-through. Anything not explicitly routed is a 404.
package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"daum.dev/showcase/internal/config"
)

// script-src 'self' is only for /static/theme.js (palette) and copy.js (copy
// buttons); nothing inline.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' https:; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

type Server struct {
	Cfg   *config.Config
	Root  func() string // directory of the current build
	Ready func() bool
	Log   *slog.Logger
	proxy *httputil.ReverseProxy
}

func New(cfg *config.Config, root func() string, ready func() bool, log *slog.Logger) *Server {
	s := &Server{Cfg: cfg, Root: root, Ready: ready, Log: log}
	s.proxy = s.newProxy()
	return s
}

// ListenAndServe serves until ctx ends, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		// no WriteTimeout: clones and image layers can take long. Page routes
		// set their own 30s deadline.
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &recorder{ResponseWriter: w, status: 200}
	s.route(rec, r)
	s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
		"took", time.Since(start).Round(time.Millisecond), "ip", s.clientIP(r))
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", csp)
	if s.proxyRoute(w, r) { // works before the first build, too
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
		http.Error(w, "request body not allowed", http.StatusBadRequest)
		return
	}
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Second))

	p := r.URL.Path
	if p == "/healthz" {
		if !s.Ready() {
			http.Error(w, "no build yet", http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok\n")
		return
	}
	if !clean(p) {
		s.notFound(w, r)
		return
	}
	// Under a base_path the pages live at base/…; old root URLs redirect
	// there. `go get` keeps asking at the root (the module path doesn't move).
	base := s.Cfg.BasePath
	if base != "" {
		switch {
		case strings.HasPrefix(p, base+"/"):
			p = strings.TrimPrefix(p, base)
		case r.URL.Query().Has("go-get"):
		case p == base:
			http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
			return
		default:
			http.Redirect(w, r, base+p, http.StatusMovedPermanently)
			return
		}
	}
	if !s.Ready() {
		http.Error(w, "the site is being built, try again in a minute", http.StatusServiceUnavailable)
		return
	}

	first, rest, _ := strings.Cut(strings.TrimPrefix(p, "/"), "/")
	switch {
	case p == "/":
		s.file(w, r, "index.html", "text/html; charset=utf-8")
	case p == "/repos/": // every repo; the profile shows the featured ones
		s.file(w, r, "repos/index.html", "text/html; charset=utf-8")
	case p == "/repos":
		http.Redirect(w, r, base+"/repos/", http.StatusMovedPermanently)
	case first == "static" && rest != "":
		s.file(w, r, p, staticType(p))
	case !s.Cfg.HasRepo(first):
		s.notFound(w, r)
	case r.URL.Query().Has("go-get"): // `go get daum.dev/<repo>/sub/pkg`
		s.file(w, r, first+"/index.html", "text/html; charset=utf-8")
	case strings.HasPrefix(rest, "raw/"):
		s.raw(w, r, p)
	case strings.HasPrefix(rest, "blob/"):
		s.file(w, r, p+".html", "text/html; charset=utf-8")
	case rest == "" && !strings.HasSuffix(p, "/"), !strings.HasSuffix(p, "/") && s.isDir(p):
		http.Redirect(w, r, base+p+"/", http.StatusMovedPermanently)
	case strings.HasSuffix(p, "/"):
		s.file(w, r, p+"index.html", "text/html; charset=utf-8")
	default:
		s.notFound(w, r)
	}
}

// clean rejects paths with dot segments, doubled slashes or NULs, so file
// lookups can't leave the build directory.
func clean(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || strings.Contains(p, "\\") {
		return false
	}
	c := path.Clean(p)
	return c == p || c+"/" == p
}

func (s *Server) isDir(p string) bool {
	fi, err := os.Stat(filepath.Join(s.Root(), filepath.FromSlash(p)))
	return err == nil && fi.IsDir()
}

// open returns a regular file inside the current build, or nil.
func (s *Server) open(p string) (*os.File, os.FileInfo) {
	f, err := os.Open(filepath.Join(s.Root(), filepath.FromSlash(p)))
	if err != nil {
		return nil, nil
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil
	}
	return f, fi
}

func (s *Server) file(w http.ResponseWriter, r *http.Request, p, ctype string) {
	f, fi := s.open(p)
	if f == nil {
		s.notFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", ctype)
	cache := "public, max-age=60"
	if strings.HasPrefix(p, "/static/") {
		cache = "public, max-age=86400"
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// raw serves repo files: allowed image types as themselves, text as
// text/plain, anything else as a download. Never as HTML, SVG or script.
func (s *Server) raw(w http.ResponseWriter, r *http.Request, p string) {
	f, fi := s.open(p)
	if f == nil {
		s.notFound(w, r)
		return
	}
	defer f.Close()
	ctype := staticType(p)
	if !strings.HasPrefix(ctype, "image/") || ctype == "image/svg+xml" { // repo SVGs are never images here
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		f.Seek(0, io.SeekStart)
		ctype = "text/plain; charset=utf-8"
		if bytes.IndexByte(head[:n], 0) >= 0 {
			ctype = "application/octet-stream"
			w.Header().Set("Content-Disposition", "attachment")
		}
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=60")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	f, _ := s.open("404.html")
	if f == nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		io.Copy(w, f)
	}
}

// staticType is the content type for files we produced or allow as images.
func staticType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".css":
		return "text/css; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".webmanifest":
		return "application/manifest; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "text/plain; charset=utf-8"
}

// clientIP uses X-Forwarded-For only when the peer is a trusted proxy.
func (s *Server) clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	for _, t := range s.Cfg.TrustedProxies {
		pre, err := netip.ParsePrefix(t)
		if err != nil {
			a := netip.MustParseAddr(t) // validated by config
			pre = netip.PrefixFrom(a, a.BitLen())
		}
		if !pre.Contains(peer.Unmap()) {
			continue
		}
		if cf := r.Header.Get("CF-Connecting-IP"); cf != "" {
			return cf
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return host
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
