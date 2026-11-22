// Package build fetches public data from Gitea and renders the static site.
package build

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"daum.dev/showcase/internal/config"
	"daum.dev/showcase/internal/gitea"
	"daum.dev/showcase/internal/render"
)

type Builder struct {
	Cfg   *config.Config
	Gitea *gitea.Client
	Now   func() time.Time
	Log   *slog.Logger
}

func New(cfg *config.Config) *Builder {
	return &Builder{
		Cfg:   cfg,
		Gitea: gitea.New(cfg.GiteaURL, cfg.User),
		Now:   time.Now,
		Log:   slog.Default(),
	}
}

type Link struct{ Name, URL string }

type Profile struct {
	User    *gitea.User
	Avatar  string
	Links   []Link
	Heatmap template.HTML
	Total   int
	Repos   []*gitea.Repo
}

// Render writes a complete site into dir, which must be empty.
func (b *Builder) Render(ctx context.Context, dir string) error {
	r, err := render.New(b.Now())
	if err != nil {
		return err
	}
	user, err := b.Gitea.User(ctx)
	if err != nil {
		return err
	}
	if err := render.CopyStatic(dir); err != nil {
		return err
	}

	p := Profile{User: user, Links: links(b.Cfg.Links)}
	if img, ct, err := b.Gitea.Avatar(ctx, user.AvatarURL); err != nil {
		b.Log.Warn("avatar", "err", err)
	} else {
		name := "/static/avatar." + strings.TrimPrefix(ct, "image/")
		if err := render.WriteFile(filepath.Join(dir, name), img); err != nil {
			return err
		}
		p.Avatar = name
	}

	for _, name := range b.Cfg.Repos {
		repo, err := b.Gitea.Repo(ctx, name)
		if err != nil {
			b.Log.Warn("skipping repo", "repo", name, "err", err)
			continue
		}
		p.Repos = append(p.Repos, repo)
	}
	slices.SortStableFunc(p.Repos, func(a, c *gitea.Repo) int { return c.UpdatedAt.Compare(a.UpdatedAt) })

	p.Heatmap, p.Total = render.Heatmap(map[string]int{}, b.Now()) // filled from git in a later step
	title := user.FullName
	if title == "" {
		title = user.Login
	}
	if err := r.Write(dir, "index.html", "profile", render.Page{Title: title, D: p}); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	return nil
}

var linkOrder = []string{"website", "atproto", "tangled", "github"}
var linkNames = map[string]string{"website": "Website", "atproto": "Bluesky", "tangled": "Tangled", "github": "GitHub"}

func links(m map[string]string) []Link {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	rank := func(k string) int {
		if i := slices.Index(linkOrder, k); i >= 0 {
			return i
		}
		return len(linkOrder)
	}
	slices.SortFunc(keys, func(a, c string) int {
		if d := rank(a) - rank(c); d != 0 {
			return d
		}
		return strings.Compare(a, c)
	})
	out := make([]Link, 0, len(keys))
	for _, k := range keys {
		name := linkNames[k]
		if name == "" {
			name = k
		}
		out = append(out, Link{name, m[k]})
	}
	return out
}
