package build

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"daum.dev/showcase/internal/gitea"
	"daum.dev/showcase/internal/gitx"
	"daum.dev/showcase/internal/render"
)

const (
	maxRender = 512 << 10 // larger files get no highlighted view
	maxRaw    = 50 << 20  // larger files get no raw copy
)

// repo is one allowlisted repo with its API metadata and local mirror.
type repo struct {
	*gitea.Repo
	Git      gitx.Repo
	Head     string // "" for an empty repo
	Releases []gitea.Release
	Pulls    []gitea.PullRequest
	Packages []gitea.Package
}

// LatestTag is the newest non-prerelease tag, for the repo header.
func (r *repo) LatestTag() string {
	for _, rel := range r.Releases {
		if !rel.Prerelease {
			return rel.TagName
		}
	}
	return ""
}

type Row struct {
	gitx.Entry
	Base string // last path segment
	Href string
	Last gitx.Commit
}

type Crumb struct{ Name, Href string }

type RepoPage struct {
	Repo     *repo
	Tab      string
	CloneURL string
	Latest   *gitx.Commit
	Crumbs   []Crumb // tree pages only
	Rows     []Row
	Readme   template.HTML
}

type BlobPage struct {
	Repo          *repo
	Tab           string
	Crumbs        []Crumb
	Path, Raw     string
	Size          int64
	Lines         []template.HTML
	Image, Binary bool
	Large         bool // too big to render; Raw is "" if too big to keep
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
	if r.Releases, err = b.Gitea.Releases(ctx, name); err != nil && !errors.Is(err, gitea.ErrNotFound) {
		return nil, err
	}
	if r.Pulls, err = b.Gitea.Pulls(ctx, name); err != nil && !errors.Is(err, gitea.ErrNotFound) {
		return nil, err
	}
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
	p := RepoPage{Repo: r, Tab: "code", CloneURL: b.Cfg.SiteURL + "/" + r.Name + ".git"}
	page := render.Page{Title: r.Name, D: &p}
	if r.Head == "" {
		return rd.Write(dir, r.Name+"/index.html", "repo", page)
	}

	log, err := r.Git.Log(ctx, r.Head, 1, "")
	if err != nil || len(log) == 0 {
		return fmt.Errorf("latest commit: %v", err)
	}
	p.Latest = &log[0]
	entries, err := r.Git.Tree(ctx, r.Head)
	if err != nil {
		return err
	}
	last, err := r.Git.LastChanges(ctx, r.Head)
	if err != nil {
		return err
	}
	kids := map[string][]gitx.Entry{}
	for _, e := range entries {
		if slices.ContainsFunc(strings.Split(e.Name, "/"), func(s string) bool { return s == "" || s == "." || s == ".." }) {
			continue
		}
		parent := path.Dir(e.Name)
		if parent == "." {
			parent = ""
		}
		kids[parent] = append(kids[parent], e)
	}

	// root
	if p.Rows, p.Readme, err = b.listing(ctx, r, "", kids[""], last); err != nil {
		return err
	}
	if slices.ContainsFunc(kids[""], func(e gitx.Entry) bool { return e.Name == "go.mod" }) {
		page.GoImport = goImport(b.Cfg.SiteURL, r.Name)
	}
	if err := rd.Write(dir, r.Name+"/index.html", "repo", page); err != nil {
		return err
	}

	if b.reuse(r, dir, "tree", "blob", "raw") {
		return nil
	}
	for _, e := range entries {
		switch {
		case e.Type == "tree":
			tp := RepoPage{Repo: r, Tab: "code", Crumbs: crumbs(r.Name, e.Name)}
			if tp.Rows, tp.Readme, err = b.listing(ctx, r, e.Name, kids[e.Name], last); err != nil {
				return err
			}
			rel := r.Name + "/tree/" + e.Name + "/index.html"
			if err := rd.Write(dir, rel, "dir", render.Page{Title: e.Name + " · " + r.Name, D: &tp}); err != nil {
				return err
			}
		case e.Type == "blob":
			if err := b.renderBlob(ctx, rd, dir, r, e); err != nil {
				return fmt.Errorf("%s: %w", e.Name, err)
			}
		}
	}
	return nil
}

// listing builds the rows and README for one directory.
func (b *Builder) listing(ctx context.Context, r *repo, dir string, entries []gitx.Entry, last map[string]gitx.Commit) ([]Row, template.HTML, error) {
	var readme template.HTML
	rows := make([]Row, 0, len(entries))
	for _, e := range entries {
		row := Row{Entry: e, Base: path.Base(e.Name), Last: last[e.Name]}
		switch e.Type {
		case "tree":
			row.Href = "/" + r.Name + "/tree/" + render.EscapePath(e.Name) + "/"
		case "blob":
			row.Href = "/" + r.Name + "/blob/" + render.EscapePath(e.Name)
			if readme == "" && isReadme(row.Base) && e.Size <= maxRender {
				src, err := r.Git.Blob(ctx, e.Hash)
				if err != nil {
					return nil, "", err
				}
				if readme, err = readmeHTML(row.Base, src, "/"+r.Name, dir); err != nil {
					return nil, "", err
				}
			}
		}
		rows = append(rows, row)
	}
	slices.SortStableFunc(rows, func(a, c Row) int {
		if (a.Type == "tree") != (c.Type == "tree") {
			return cmp.Compare(c.Type, a.Type) // "tree" sorts before "blob"/"commit"
		}
		return cmp.Compare(strings.ToLower(a.Base), strings.ToLower(c.Base))
	})
	return rows, readme, nil
}

func (b *Builder) renderBlob(ctx context.Context, rd *render.Renderer, dir string, r *repo, e gitx.Entry) error {
	p := BlobPage{Repo: r, Tab: "code", Crumbs: crumbs(r.Name, e.Name), Path: e.Name, Size: e.Size}
	p.Image = isImage(e.Name)
	if e.Size <= maxRaw {
		src, err := r.Git.Blob(ctx, e.Hash)
		if err != nil {
			return err
		}
		if err := render.WriteFile(filepath.Join(dir, r.Name, "raw", filepath.FromSlash(e.Name)), src); err != nil {
			return err
		}
		p.Raw = "/" + r.Name + "/raw/" + render.EscapePath(e.Name)
		p.Binary = bytes.IndexByte(src[:min(len(src), 8000)], 0) >= 0 || !utf8.Valid(src)
		if !p.Binary && e.Size <= maxRender {
			p.Lines = render.Highlight(e.Name, "", string(src))
		}
	}
	p.Large = e.Size > maxRender
	// blob pages are stored as <path>.html; the server maps /blob/<path> to them
	return rd.Write(dir, r.Name+"/blob/"+e.Name+".html", "blob", render.Page{Title: e.Name + " · " + r.Name, Wide: true, D: &p})
}

// crumbs links the repo and every parent directory of p; the last part has no link.
func crumbs(repoName, p string) []Crumb {
	cs := []Crumb{{repoName, "/" + repoName + "/"}}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		c := Crumb{Name: part}
		if i < len(parts)-1 {
			c.Href = "/" + repoName + "/tree/" + render.EscapePath(strings.Join(parts[:i+1], "/")) + "/"
		}
		cs = append(cs, c)
	}
	return cs
}

func readmeHTML(name string, src []byte, base, dir string) (template.HTML, error) {
	n := strings.ToLower(name)
	if strings.HasSuffix(n, ".md") || strings.HasSuffix(n, ".markdown") {
		return render.Markdown(src, base, dir)
	}
	return template.HTML("<pre>" + template.HTMLEscapeString(string(src)) + "</pre>"), nil
}

func isReadme(name string) bool {
	n := strings.ToLower(name)
	return n == "readme" || n == "readme.md" || n == "readme.markdown" || n == "readme.txt"
}

// isImage reports whether the raw route serves name as an image (see
// docs/decisions.md). SVG is deliberately not one.
func isImage(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func goImport(siteURL, name string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(siteURL, "https://"), "http://")
	return host + "/" + name + " git " + siteURL + "/" + name + ".git"
}
