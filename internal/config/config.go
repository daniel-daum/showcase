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
	GiteaURL   string            `json:"gitea_url"`
	User       string            `json:"user"`
	SiteURL    string            `json:"site_url"`
	Links      map[string]string `json:"links"`
	Repos      []string          `json:"repos"`
	Images     []string          `json:"images"`
	MaxCommits int               `json:"max_commits"`
	Theme      string            `json:"theme"` // saturn palette, default moss
	// Proxies (IPs or CIDRs) whose X-Forwarded-For is believed, for logs only.
	TrustedProxies []string `json:"trusted_proxies"`
}

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	imageRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	// Top-level names the site or router uses for itself.
	reserved = []string{"static", "v2", "healthz", "api", "user", "index.html", "404.html"}
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
	seen := map[string]bool{}
	for _, r := range c.Repos {
		l := strings.ToLower(r)
		if !nameRe.MatchString(r) || strings.HasSuffix(l, ".git") || slices.Contains(reserved, l) || seen[l] {
			return fmt.Errorf("bad or duplicate repo name %q", r)
		}
		seen[l] = true
	}
	for _, im := range c.Images {
		if !imageRe.MatchString(im) {
			return fmt.Errorf("bad image name %q", im)
		}
	}
	for k, v := range c.Links {
		if p, err := url.Parse(v); err != nil || p.Scheme != "https" {
			return fmt.Errorf("link %q must be an https url", k)
		}
	}
	for _, p := range c.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err != nil {
			if _, err := netip.ParseAddr(p); err != nil {
				return fmt.Errorf("bad trusted proxy %q", p)
			}
		}
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
