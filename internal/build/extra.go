package build

import (
	"context"
	"html/template"
	"net/url"
	"strings"

	"daum.dev/showcase/internal/gitea"
	"daum.dev/showcase/internal/gitx"
	"daum.dev/showcase/internal/render"
)

type ReleaseView struct {
	gitea.Release
	Notes  template.HTML
	Assets []Crumb // Name and Href; files are served by the release proxy route
}

type PullView struct {
	gitea.PullRequest
	Body   template.HTML
	Thread []CommentView
	Hidden int // older comments left out (maxThread)
}

type IssueView struct {
	gitea.Issue
	Body   template.HTML
	Thread []CommentView
	Hidden int
}

type CommentView struct {
	gitea.Comment
	Body template.HTML
}

type PackageView struct {
	gitea.Package
}

type ExtraPage struct {
	Repo     *repo
	Tab      string
	Releases []ReleaseView
	Pulls    []PullView
	Issues   []IssueView
	Open     int // issues, by state
	Closed   int
	Packages []PackageView // other package types, from the API
	Images   []ImageView   // container images, from the registry
	Kind     string        // "branches" or "tags" on the refs pages
	Refs     []gitx.Ref
}

// Limits on text anyone with a Gitea account can write: a body longer
// than maxUserText is cut, and a thread shows its newest maxThread
// comments. They keep a spammer from turning one page into megabytes.
const (
	maxUserText = 32 << 10
	maxThread   = 100
)

// userMarkdown renders Markdown from issues, pulls and comments. It goes
// through the same allowlist as READMEs; the cap keeps pages small.
func userMarkdown(s, base string) (template.HTML, error) {
	if len(s) > maxUserText {
		s = strings.ToValidUTF8(s[:maxUserText], "") + "\n\n*(cut: too long to show here)*"
	}
	return render.Markdown([]byte(s), base, "")
}

// thread renders the newest maxThread comments on issue or pull n and
// says how many older ones it left out.
func thread(r *repo, n int, base string) ([]CommentView, int, error) {
	cs := r.Comments[n]
	hidden := max(0, len(cs)-maxThread)
	var out []CommentView
	for _, c := range cs[hidden:] {
		body, err := userMarkdown(c.Body, base)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, CommentView{c, body})
	}
	return out, hidden, nil
}

// renderExtras writes the releases, issues, pulls and packages pages from API and
// registry data.
func (b *Builder) renderExtras(ctx context.Context, rd *render.Renderer, dir string, r *repo) error {
	base := "/" + r.Name
	// Downloads go through the proxy at the root of site_url. Under a
	// base_path the pages may be served elsewhere, so link there directly.
	dl := base
	if b.Cfg.BasePath != "" {
		dl = b.Cfg.SiteURL + base
	}
	rel := ExtraPage{Repo: r, Tab: "releases"}
	for _, x := range r.Releases {
		v := ReleaseView{Release: x}
		var err error
		if v.Notes, err = render.Markdown([]byte(x.Body), base, ""); err != nil {
			return err
		}
		for _, a := range x.Assets {
			v.Assets = append(v.Assets, Crumb{a.Name, dl + "/releases/download/" + url.PathEscape(x.TagName) + "/" + url.PathEscape(a.Name)})
		}
		rel.Releases = append(rel.Releases, v)
	}

	pulls := ExtraPage{Repo: r, Tab: "pulls"}
	for _, x := range r.Pulls {
		body, err := userMarkdown(x.Body, base)
		if err != nil {
			return err
		}
		th, hidden, err := thread(r, x.Number, base)
		if err != nil {
			return err
		}
		pulls.Pulls = append(pulls.Pulls, PullView{x, body, th, hidden})
	}

	issues := ExtraPage{Repo: r, Tab: "issues"}
	for _, x := range r.Issues {
		body, err := userMarkdown(x.Body, base)
		if err != nil {
			return err
		}
		th, hidden, err := thread(r, x.Number, base)
		if err != nil {
			return err
		}
		issues.Issues = append(issues.Issues, IssueView{x, body, th, hidden})
		if x.State == "open" {
			issues.Open++
		} else {
			issues.Closed++
		}
	}

	pkgs := ExtraPage{Repo: r, Tab: "packages", Images: r.Images}
	for _, x := range r.Packages {
		if x.Type == "container" {
			continue // images come from the registry, and only allowlisted ones
		}
		pkgs.Packages = append(pkgs.Packages, PackageView{Package: x})
	}

	for _, p := range []ExtraPage{rel, issues, pulls, pkgs} {
		page := render.Page{Title: p.Tab + " · " + r.Name, D: &p}
		if err := rd.Write(dir, r.Name+"/"+p.Tab+"/index.html", p.Tab, page); err != nil {
			return err
		}
	}
	for _, p := range []ExtraPage{
		{Repo: r, Tab: "code", Kind: "branches", Refs: r.Branches},
		{Repo: r, Tab: "code", Kind: "tags", Refs: r.Tags},
	} {
		page := render.Page{Title: p.Kind + " · " + r.Name, D: &p}
		if err := rd.Write(dir, r.Name+"/"+p.Kind+"/index.html", "refs", page); err != nil {
			return err
		}
	}
	return nil
}
