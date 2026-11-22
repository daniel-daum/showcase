package build

import (
	"context"

	"daum.dev/showcase/internal/gitx"
	"daum.dev/showcase/internal/render"
)

type LogPage struct {
	Repo    *repo
	Tab     string
	Commits []gitx.Commit
}

type CommitPage struct {
	Repo           *repo
	Tab            string
	C              gitx.Commit
	Parents        []Crumb // Href is "" when the parent has no page
	Files          []gitx.FileDiff
	Added, Deleted int
}

// renderLog writes the log page and one page per listed commit. Only those
// commits get pages, so the site's size stays bounded.
func (b *Builder) renderLog(ctx context.Context, rd *render.Renderer, dir string, r *repo) error {
	p := LogPage{Repo: r, Tab: "log"}
	if r.Head != "" {
		var err error
		if p.Commits, err = r.Git.Log(ctx, r.Head, b.Cfg.MaxCommits, ""); err != nil {
			return err
		}
	}
	if err := rd.Write(dir, r.Name+"/log/index.html", "log", render.Page{Title: "commits · " + r.Name, D: &p}); err != nil {
		return err
	}

	if b.reuse(r, dir, "commit") {
		return nil
	}
	listed := map[string]bool{}
	for _, c := range p.Commits {
		listed[c.Hash] = true
	}
	for _, c := range p.Commits {
		cp := CommitPage{Repo: r, Tab: "log", C: c}
		for _, h := range c.Parents {
			cr := Crumb{Name: h[:min(len(h), 10)]}
			if listed[h] {
				cr.Href = "/" + r.Name + "/commit/" + h + "/"
			}
			cp.Parents = append(cp.Parents, cr)
		}
		var err error
		if cp.Files, err = r.Git.Diff(ctx, c.Hash); err != nil {
			return err
		}
		for _, f := range cp.Files {
			cp.Added += f.Added
			cp.Deleted += f.Deleted
		}
		page := render.Page{Title: c.Subject + " · " + r.Name, Wide: true, D: &cp}
		if err := rd.Write(dir, r.Name+"/commit/"+c.Hash+"/index.html", "commit", page); err != nil {
			return err
		}
	}
	return nil
}
