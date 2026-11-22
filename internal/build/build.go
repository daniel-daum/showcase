// Package build fetches public data from Gitea and renders the static site.
package build

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"maps"
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
	Data  string // holds mirrors/ (and later site/)
	Now   func() time.Time
	Log   *slog.Logger
	st    state
}

func New(cfg *config.Config, data string) *Builder {
	return &Builder{
		Cfg:   cfg,
		Gitea: gitea.New(cfg.GiteaURL, cfg.User),
		Data:  data,
		Now:   time.Now,
		Log:   slog.Default(),
	}
}

type Profile struct {
	User    *gitea.User
	Avatar  string
	Heatmap template.HTML
	Total   int
	Repos   []*gitea.Repo
}

// Render writes a complete site into dir, which must be empty.
func (b *Builder) Render(ctx context.Context, dir string) error {
	rd, err := render.New(b.Now())
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

	rd.Theme, rd.Links = b.Cfg.Theme, links(b.Cfg.Links)
	p := Profile{User: user}
	if img, ct, err := b.Gitea.Avatar(ctx, user.AvatarURL); err != nil {
		b.Log.Warn("avatar", "err", err)
	} else {
		name := "/static/avatar." + strings.TrimPrefix(ct, "image/")
		if err := render.WriteFile(filepath.Join(dir, name), img); err != nil {
			return err
		}
		p.Avatar = name
	}

	var repos []*repo
	days, since := map[string]int{}, b.Now().AddDate(-1, 0, -7)
	for _, name := range b.Cfg.Repos {
		r, err := b.loadRepo(ctx, name)
		if errors.Is(err, gitea.ErrNotFound) { // private, renamed or deleted: never shown
			b.Log.Warn("skipping repo", "repo", name, "err", err)
			continue
		}
		if err != nil { // anything else may be transient: keep the last good build
			return fmt.Errorf("%s: %w", name, err)
		}
		if r.Head != "" {
			if err := r.Git.CommitDays(ctx, r.Head, since, days); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		repos = append(repos, r)
		p.Repos = append(p.Repos, r.Repo)
	}
	pkgs, err := b.Gitea.Packages(ctx, b.Cfg.Repos)
	if err != nil && !errors.Is(err, gitea.ErrNotFound) {
		return fmt.Errorf("packages: %w", err)
	}
	for _, pk := range pkgs {
		for _, r := range repos {
			if pk.Repository.Name == r.Name {
				r.Packages = append(r.Packages, pk)
			}
		}
	}
	slices.SortStableFunc(p.Repos, func(a, c *gitea.Repo) int { return c.UpdatedAt.Compare(a.UpdatedAt) })

	p.Heatmap, p.Total = render.Heatmap(days, b.Now())
	title := user.FullName
	if title == "" {
		title = user.Login
	}
	if err := rd.Write(dir, "index.html", "profile", render.Page{Title: title, D: p}); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if err := rd.Write(dir, "404.html", "notfound", render.Page{Title: "not found"}); err != nil {
		return err
	}
	for _, rp := range repos {
		if err := b.renderRepo(ctx, rd, dir, rp); err != nil {
			return fmt.Errorf("%s: %w", rp.Name, err)
		}
		if err := b.renderLog(ctx, rd, dir, rp); err != nil {
			return fmt.Errorf("%s log: %w", rp.Name, err)
		}
		if err := b.renderExtras(rd, dir, rp); err != nil {
			return fmt.Errorf("%s: %w", rp.Name, err)
		}
	}
	return nil
}

// links orders the footer links: known keys first, then the rest by key.
func links(m map[string]string) []render.Link {
	seen := map[string]bool{}
	var out []render.Link
	for _, k := range append([]string{"website", "atproto", "tangled", "github"}, slices.Sorted(maps.Keys(m))...) {
		if u, ok := m[k]; ok && !seen[k] {
			seen[k] = true
			out = append(out, render.Link{Name: cmp.Or(map[string]string{"atproto": "bluesky"}[k], k), URL: u})
		}
	}
	return out
}
