package build

import (
	"context"
	"time"

	"daum.dev/showcase/internal/gitea"

	"daum.dev/showcase/internal/gitx"
	"daum.dev/showcase/internal/render"
)

type LogPage struct {
	Repo    *repo
	Tab     string
	Commits []gitx.Commit
	Days    []LogDay // the same commits, grouped by day (UTC)
}

type LogDay struct {
	Title   string // "Today", "Yesterday", "Nov 14", "Nov 14, 2025"
	Commits []LogCommit
}

type LogCommit struct {
	gitx.Commit
	gitx.Stat
}

type CommitPage struct {
	Repo           *repo
	Tab            string
	C              gitx.Commit
	Parents        []Crumb // Href is "" when the parent has no page
	Files          []gitx.FileDiff
	Added, Deleted int
	Status         *gitea.Status // CI checks; only known for HEAD
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
		stats, err := r.Git.ShortStats(ctx, r.Head, b.Cfg.MaxCommits)
		if err != nil {
			return err
		}
		last := ""
		for _, c := range p.Commits {
			if d := c.When.Format("2006-01-02"); d != last {
				p.Days, last = append(p.Days, LogDay{Title: dayTitle(c.When, b.Now())}), d
			}
			day := &p.Days[len(p.Days)-1]
			day.Commits = append(day.Commits, LogCommit{c, stats[c.Hash]})
		}
	}
	if err := rd.Write(dir, r.Name+"/log/index.html", "log", render.Page{Title: "commits · " + r.Name, Wide: true, D: &p}); err != nil {
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
		if c.Hash == r.Head {
			cp.Status = r.Status
		}
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

// dayTitle names a day relative to now (UTC).
func dayTitle(t, now time.Time) string {
	t, now = t.UTC(), now.UTC()
	switch d := t.Format("2006-01-02"); {
	case d == now.Format("2006-01-02"):
		return "Today"
	case d == now.AddDate(0, 0, -1).Format("2006-01-02"):
		return "Yesterday"
	case t.Year() != now.Year():
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2")
}
