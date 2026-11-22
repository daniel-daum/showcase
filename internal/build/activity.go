package build

import (
	"cmp"
	"html/template"
	"slices"
	"strconv"
	"strings"
	"time"

	"daum.dev/showcase/internal/gitea"
	"daum.dev/showcase/internal/render"
)

const (
	featuredCards = 6
	sparkWeeks    = 12
	yearWeeks     = 52
	feedDays      = 90 // how far back the feed looks
	feedItems     = 6  // and how many events it shows at most
	feedCommits   = 2  // commit lines under a push; the rest are "+N more"
	activeFor     = 7 * 24 * time.Hour
)

// RepoCard is a repo on the profile grid and the repos page.
type RepoCard struct {
	*gitea.Repo
	Spark   template.HTML // 12-week commit sparkline
	Release string        // latest release tag
	Active  bool          // a commit in the last week
}

// FeedItem is one event: "push", "created", "release", "opened" or "merged".
type FeedItem struct {
	Kind   string
	When   time.Time
	Stamp  string // "09:00" today, else "Nov 19"
	Actor  string
	Own    bool // the profile's user: shown with their avatar
	Repo   string
	Branch string // push
	Label  string // release tag or "#N"
	Href   string
	Title  string // release name, pull title or repo description
	Pre    bool   // pre-release

	Commits []FeedCommit // push: the newest few
	Count   int          // push: all commits in it
	More    int          // push: commits not listed
}

type FeedCommit struct{ Short, Subject, Href string }

// activity fills the profile's activity parts from the repos' commits,
// releases and pulls. Only allowlisted repos are in repos, so nothing else
// can show up here.
func (b *Builder) activity(p *Profile, repos []*repo, login string) {
	now := b.Now()
	var all []time.Time
	for _, r := range repos {
		for _, c := range r.Year {
			all = append(all, c.When)
		}
	}
	weeks := render.Weeks(all, now, yearWeeks)
	p.Weekly, p.Streak = render.WeeklyBars(weeks, now), render.Streak(weeks)
	for _, n := range weeks {
		if n > 0 {
			p.ActiveWeeks++
		}
	}

	for _, r := range repos {
		var ts []time.Time
		for _, c := range r.Year {
			ts = append(ts, c.When)
		}
		c := RepoCard{Repo: r.Repo, Spark: render.Sparkline(render.Weeks(ts, now, sparkWeeks)), Release: r.LatestTag()}
		c.Active = len(r.Recent) > 0 && now.Sub(r.Recent[0].When) < activeFor
		p.Repos = append(p.Repos, c)
	}
	slices.SortStableFunc(p.Repos, func(a, c RepoCard) int { return c.UpdatedAt.Compare(a.UpdatedAt) })
	p.Featured = p.Repos[:min(len(p.Repos), featuredCards)]
	if len(b.Cfg.Featured) > 0 {
		p.Featured = nil
		for _, name := range b.Cfg.Featured[:min(len(b.Cfg.Featured), featuredCards)] {
			if i := slices.IndexFunc(p.Repos, func(c RepoCard) bool { return c.Name == name }); i >= 0 {
				p.Featured = append(p.Featured, p.Repos[i])
			}
		}
	}

	p.Feed = stamp(b.feedItems(repos, login, now.Add(-feedDays*24*time.Hour)), now)
}

// feedItems lists the events since a time, newest first. Commits become
// pushes: one per repo, author and day, since git doesn't record pushes.
func (b *Builder) feedItems(repos []*repo, login string, since time.Time) []FeedItem {
	var items []FeedItem
	for _, r := range repos {
		base := "/" + r.Name + "/"
		pushes := map[string]int{} // day and author -> index in items
		for _, c := range r.Year { // newest first
			if c.When.Before(since) {
				break
			}
			key := c.When.Format("2006-01-02") + "\x00" + c.Author
			i, ok := pushes[key]
			if !ok {
				own := r.IsOwner(c.Author)
				actor := c.Author
				if own {
					actor = login
				}
				items = append(items, FeedItem{Kind: "push", When: c.When, Actor: actor, Own: own, Repo: r.Name,
					Branch: r.DefaultBranch, Href: base + "log/"})
				i = len(items) - 1
				pushes[key] = i
			}
			it := &items[i]
			if it.Count++; len(it.Commits) < feedCommits {
				it.Commits = append(it.Commits, FeedCommit{Short: c.Hash[:min(len(c.Hash), 7)], Subject: c.Subject, Href: r.CommitHref(c.Hash)})
			} else {
				it.More++
			}
		}
		if !r.CreatedAt.Before(since) {
			items = append(items, FeedItem{Kind: "created", When: r.CreatedAt, Actor: login, Own: true, Repo: r.Name, Href: base, Title: r.Description})
		}
		for _, rel := range r.Releases {
			if !rel.PublishedAt.Before(since) {
				items = append(items, FeedItem{Kind: "release", When: rel.PublishedAt, Actor: login, Own: true, Repo: r.Name,
					Label: rel.TagName, Href: base + "releases/#" + rel.TagName, Title: rel.Name, Pre: rel.Prerelease})
			}
		}
		for _, pr := range r.Pulls {
			n := strconv.Itoa(pr.Number)
			it := FeedItem{Repo: r.Name, Label: "#" + n, Href: base + "pulls/#pr-" + n, Title: pr.Title}
			if !pr.CreatedAt.Before(since) {
				it.Kind, it.When, it.Actor, it.Own = "opened", pr.CreatedAt, pr.User.Login, strings.EqualFold(pr.User.Login, login)
				items = append(items, it)
			}
			if pr.MergedAt != nil && !pr.MergedAt.Before(since) { // the owner merges
				it.Kind, it.When, it.Actor, it.Own = "merged", *pr.MergedAt, login, true
				items = append(items, it)
			}
		}
	}
	slices.SortStableFunc(items, func(a, c FeedItem) int { return cmp.Or(c.When.Compare(a.When), strings.Compare(a.Repo, c.Repo)) })
	return items[:min(len(items), feedItems)]
}

// stamp labels each item's time column: the time today, else the date.
// The full time is in the title.
func stamp(items []FeedItem, now time.Time) []FeedItem {
	today := now.UTC().Format("2006-01-02")
	for i, it := range items {
		if t := it.When.UTC(); t.Format("2006-01-02") == today {
			items[i].Stamp = t.Format("15:04")
		} else {
			items[i].Stamp = t.Format("Jan 2")
		}
	}
	return items
}
