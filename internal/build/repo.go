package build

import (
	"cmp"
	"context"
	"fmt"
	"html/template"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"daum.dev/showcase/internal/gitea"
	"daum.dev/showcase/internal/gitx"
	"daum.dev/showcase/internal/render"
)

const maxRender = 512 << 10 // files larger than this aren't rendered

// repo is one allowlisted repo with its API metadata and local mirror.
type repo struct {
	*gitea.Repo
	Git  gitx.Repo
	Head string // "" for an empty repo
}

type Row struct {
	gitx.Entry
	Href string
	Last gitx.Commit
}

type RepoPage struct {
	Repo     *gitea.Repo
	Tab      string
	CloneURL string
	Latest   *gitx.Commit
	Rows     []Row
	Readme   template.HTML
}

// loadRepo fetches metadata and updates the mirror. Anonymous access means a
// private repo fails here and is skipped by the caller.
func (b *Builder) loadRepo(ctx context.Context, name string) (*repo, error) {
	meta, err := b.Gitea.Repo(ctx, name)
	if err != nil {
		return nil, err
	}
	if meta.Name != name {
		return nil, fmt.Errorf("api returned repo %q", meta.Name)
	}
	r := &repo{Repo: meta}
	src := b.Cfg.GiteaURL + "/" + url.PathEscape(b.Cfg.User) + "/" + url.PathEscape(name) + ".git"
	if r.Git, err = gitx.Mirror(ctx, src, filepath.Join(b.Data, "mirrors", name+".git")); err != nil {
		return nil, err
	}
	if !meta.Empty {
		if r.Head, err = r.Git.Head(ctx, meta.DefaultBranch); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (b *Builder) renderRepo(ctx context.Context, rd *render.Renderer, dir string, r *repo) error {
	base := "/" + r.Name
	p := RepoPage{Repo: r.Repo, Tab: "code", CloneURL: b.Cfg.SiteURL + base + ".git"}
	page := render.Page{Title: r.Name, D: &p}
	if r.Head == "" {
		return rd.Write(dir, r.Name+"/index.html", "repo", page)
	}

	log, err := r.Git.Log(ctx, r.Head, 1, "")
	if err != nil || len(log) == 0 {
		return fmt.Errorf("latest commit: %v", err)
	}
	p.Latest = &log[0]

	entries, err := r.Git.Tree(ctx, r.Head, "")
	if err != nil {
		return err
	}
	rows, err := b.rows(ctx, r, "", entries)
	if err != nil {
		return err
	}
	p.Rows = rows

	for _, e := range entries {
		if e.Name == "go.mod" {
			page.GoImport = goImport(b.Cfg.SiteURL, r.Name)
		}
		if p.Readme == "" && e.Type == "blob" && isReadme(e.Name) {
			if p.Readme, err = b.readme(ctx, r, e, base); err != nil {
				return err
			}
		}
	}
	return rd.Write(dir, r.Name+"/index.html", "repo", page)
}

// rows sorts entries (directories first) and finds each one's last commit.
func (b *Builder) rows(ctx context.Context, r *repo, dir string, entries []gitx.Entry) ([]Row, error) {
	rows := make([]Row, 0, len(entries))
	for _, e := range entries {
		full := strings.TrimPrefix(dir+"/"+e.Name, "/")
		row := Row{Entry: e}
		switch e.Type {
		case "tree":
			row.Href = "/" + r.Name + "/tree/" + render.EscapePath(full) + "/"
		case "blob":
			row.Href = "/" + r.Name + "/blob/" + render.EscapePath(full)
		}
		log, err := r.Git.Log(ctx, r.Head, 1, full)
		if err != nil {
			return nil, err
		}
		if len(log) > 0 {
			row.Last = log[0]
		}
		rows = append(rows, row)
	}
	slices.SortStableFunc(rows, func(a, c Row) int {
		if (a.Type == "tree") != (c.Type == "tree") {
			return cmp.Compare(c.Type, a.Type) // "tree" sorts before "blob"/"commit"
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(c.Name))
	})
	return rows, nil
}

func (b *Builder) readme(ctx context.Context, r *repo, e gitx.Entry, base string) (template.HTML, error) {
	src, err := r.Git.Blob(ctx, e.Hash, e.Size, maxRender)
	if err != nil || src == nil {
		return "", err
	}
	if strings.HasSuffix(strings.ToLower(e.Name), ".md") || strings.HasSuffix(strings.ToLower(e.Name), ".markdown") {
		return render.Markdown(src, base, "")
	}
	return template.HTML("<pre>" + template.HTMLEscapeString(string(src)) + "</pre>"), nil
}

func isReadme(name string) bool {
	n := strings.ToLower(name)
	return n == "readme" || n == "readme.md" || n == "readme.markdown" || n == "readme.txt"
}

func goImport(siteURL, name string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(siteURL, "https://"), "http://")
	return host + "/" + name + " git " + siteURL + "/" + name + ".git"
}
