package build

import (
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
	Body template.HTML
}

type PackageView struct {
	gitea.Package
	Command string // only for images the registry proxy serves
}

type ExtraPage struct {
	Repo     *repo
	Tab      string
	Releases []ReleaseView
	Pulls    []PullView
	Packages []PackageView
	Kind     string // "branches" or "tags" on the refs pages
	Refs     []gitx.Ref
}

// renderExtras writes the releases, pulls and packages pages from API data.
func (b *Builder) renderExtras(rd *render.Renderer, dir string, r *repo) error {
	base := "/" + r.Name
	rel := ExtraPage{Repo: r, Tab: "releases"}
	for _, x := range r.Releases {
		v := ReleaseView{Release: x}
		var err error
		if v.Notes, err = render.Markdown([]byte(x.Body), base, ""); err != nil {
			return err
		}
		for _, a := range x.Assets {
			v.Assets = append(v.Assets, Crumb{a.Name, base + "/releases/download/" + url.PathEscape(x.TagName) + "/" + url.PathEscape(a.Name)})
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

	pkgs := ExtraPage{Repo: r, Tab: "packages"}
	for _, x := range r.Packages {
		if strings.HasPrefix(x.Version, "sha256:") {
			continue // untagged image manifests
		}
		v := PackageView{Package: x}
		if x.Type == "container" {
			if !b.Cfg.HasImage(x.Name) {
				continue // not pullable through the proxy, so don't advertise it
			}
			v.Command = "docker pull " + siteHost(b.Cfg.SiteURL) + "/" + b.Cfg.User + "/" + x.Name + ":" + x.Version
		}
		pkgs.Packages = append(pkgs.Packages, v)
	}

	for _, p := range []ExtraPage{rel, pulls, pkgs} {
		page := render.Page{Title: p.Tab + " · " + r.Name, Wide: true, D: &p}
		if err := rd.Write(dir, r.Name+"/"+p.Tab+"/index.html", p.Tab, page); err != nil {
			return err
		}
	}
	for _, p := range []ExtraPage{
		{Repo: r, Tab: "code", Kind: "branches", Refs: r.Branches},
		{Repo: r, Tab: "code", Kind: "tags", Refs: r.Tags},
	} {
		page := render.Page{Title: p.Kind + " · " + r.Name, Wide: true, D: &p}
		if err := rd.Write(dir, r.Name+"/"+p.Kind+"/index.html", "refs", page); err != nil {
			return err
		}
	}
	return nil
}
