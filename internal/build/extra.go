package build

import (
	"context"
	"html/template"
	"net/url"

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
	Packages []PackageView // other package types, from the API
	Images   []ImageView   // container images, from the registry
	Kind     string        // "branches" or "tags" on the refs pages
	Refs     []gitx.Ref
}

// renderExtras writes the releases, pulls and packages pages from API and
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
		body, err := render.Markdown([]byte(x.Body), base, "")
		if err != nil {
			return err
		}
		pulls.Pulls = append(pulls.Pulls, PullView{x, body})
	}

	pkgs := ExtraPage{Repo: r, Tab: "packages", Images: r.Images}
	for _, x := range r.Packages {
		if x.Type == "container" {
			continue // images come from the registry, and only allowlisted ones
		}
		pkgs.Packages = append(pkgs.Packages, PackageView{Package: x})
	}

	for _, p := range []ExtraPage{rel, pulls, pkgs} {
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
