// Package config loads and validates config.json.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
)

type Config struct {
	GiteaURL string `json:"gitea_url"`
	User     string `json:"user"`
	SiteURL  string `json:"site_url"`
	// BasePath ("/projects") puts the pages under one path segment, so a
	// proxy can serve them at danieldaum.net/projects. Clone, go get, docker
	// pull and /healthz stay at the root of site_url. Empty means the root.
	BasePath string   `json:"base_path"`
	Repos    []string `json:"repos"`
	Images   []string `json:"images"`
	// Featured repos for the profile, in order (up to 6). Empty means the
	// most recently updated ones.
	Featured   []string `json:"featured"`
	MaxCommits int      `json:"max_commits"`
	Theme      string   `json:"theme"` // saturn palette, default moss
	// Proxies (IPs or CIDRs) whose X-Forwarded-For is believed, for logs only.
	TrustedProxies []string `json:"trusted_proxies"`
}

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	baseRe  = regexp.MustCompile(`^(/[a-z0-9][a-z0-9-]*)?$`)
	imageRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	// Top-level names the site or router uses for itself.
	reserved = []string{"static", "v2", "healthz", "api", "user", "repos", "index.html", "404.html"}
	themes   = []string{"moss", "ink", "wine", "plum", "teal", "flexoki"}
)

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

func (c *Config) validate() error {
	c.GiteaURL = strings.TrimRight(c.GiteaURL, "/")
	c.SiteURL = strings.TrimRight(c.SiteURL, "/")
	for _, u := range []string{c.GiteaURL, c.SiteURL} {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return fmt.Errorf("bad url %q", u)
		}
	}
	if !nameRe.MatchString(c.User) {
		return fmt.Errorf("bad user %q", c.User)
	}
	// Gitea matches user names in any case, but image refs must be lowercase
	c.User = strings.ToLower(c.User)
	seen := map[string]bool{}
	for _, r := range c.Repos {
		l := strings.ToLower(r)
		if !nameRe.MatchString(r) || strings.HasSuffix(l, ".git") || slices.Contains(reserved, l) || seen[l] {
			return fmt.Errorf("bad or duplicate repo name %q", r)
		}
		seen[l] = true
	}
	for _, f := range c.Featured {
		if !c.HasRepo(f) {
			return fmt.Errorf("featured repo %q is not in repos", f)
		}
	}
	for _, im := range c.Images {
		if !imageRe.MatchString(im) {
			return fmt.Errorf("bad image name %q", im)
		}
	}
	for _, p := range c.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err != nil {
			if _, err := netip.ParseAddr(p); err != nil {
				return fmt.Errorf("bad trusted proxy %q", p)
			}
		}
	}
	if b := strings.TrimPrefix(c.BasePath, "/"); !baseRe.MatchString(c.BasePath) || slices.Contains(reserved, b) || seen[b] {
		return fmt.Errorf("base_path %q must be empty or one lowercase segment like /projects, not a reserved or repo name", c.BasePath)
	}
	if c.Theme == "" {
		c.Theme = "moss"
	}
	if !slices.Contains(themes, c.Theme) {
		return fmt.Errorf("unknown theme %q", c.Theme)
	}
	if c.MaxCommits <= 0 {
		c.MaxCommits = 50
	}
	return nil
}

// HasRepo reports whether name is allowlisted. Names are compared exactly.
func (c *Config) HasRepo(name string) bool { return slices.Contains(c.Repos, name) }

// HasImage reports whether image is allowlisted.
func (c *Config) HasImage(name string) bool { return slices.Contains(c.Images, name) }
