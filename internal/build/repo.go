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
	Issues   []gitea.Issue
	Comments map[int][]gitea.Comment // by issue or pull number
	Packages []gitea.Package
	Images   []ImageView // allowlisted container images, from the registry

	Owner, OwnerName, Avatar string // for the header and commit rows
	Commits                  int
	Branches, Tags           []gitx.Ref
	Status                   *gitea.Status              // CI status of HEAD, nil if none
	Sigs                     map[string]gitea.Signature // signed commits among Recent, by hash
	Langs                    []Lang
	License, LicenseHref     string
	Listed                   map[string]bool // commits that have their own page
	Recent                   []gitx.Commit   // the log page's commits, newest first
	Year                     []gitx.Commit   // the last year's commits, for activity
}

// Lang is one segment of the language bar, X and W on a 0..1000 scale.
type Lang struct {
	Name      string
	Pct, X, W float64
}

// IsOwner reports whether a commit author is the profile's user, whose
// avatar we have.
func (r *repo) IsOwner(author string) bool {
	return author != "" && (strings.EqualFold(author, r.OwnerName) || strings.EqualFold(author, r.Owner))
}

// Release returns the release for a tag, if any.
func (r *repo) Release(tag string) *gitea.Release {
	for i := range r.Releases {
		if r.Releases[i].TagName == tag {
			return &r.Releases[i]
		}
	}
	return nil
}

// Sig is Gitea's verdict on a commit's signature, nil if it is unsigned or
// not among the log page's commits.
func (r *repo) Sig(hash string) *gitea.Signature {
	if s, ok := r.Sigs[hash]; ok {
		return &s
	}
	return nil
}

// CommitHref links a commit to its page, if it has one.
func (r *repo) CommitHref(hash string) string {
	if !r.Listed[hash] {
		return ""
	}
	return "/" + r.Name + "/commit/" + hash + "/"
}

// LastCommit is the newest commit on the default branch, nil if none.
func (r *repo) LastCommit() *gitx.Commit {
	if len(r.Recent) == 0 {
		return nil
	}
	return &r.Recent[0]
}

// PackageCount counts what the packages page lists: registry images plus
// non-container packages from the API.
func (r *repo) PackageCount() int {
	n := len(r.Images)
	for _, p := range r.Packages {
		if p.Type != "container" {
			n++
		}
	}
	return n
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
	Base       string // last path segment
	Href       string
	Last       gitx.Commit
	CommitHref string
}

type Crumb struct{ Name, Href string }

type RepoPage struct {
	Repo     *repo
	Tab      string
	CloneURL string
	GoGet    string // "daum.dev/<repo>" for Go modules
	Latest   *gitx.Commit
	Crumbs   []Crumb // tree pages only
	Rows     []Row
	Docs     []Doc // README first; the repo root adds license, security and so on
}

// Doc is one tab of the README box.
type Doc struct {
	Key, Label, Icon string
	HTML             template.HTML
}

// docKinds are the tabs next to the README, in tab order: GitHub's
// community files plus the usual project documents. Each is found by file
// stem (any case) in the repo root, .github/ or docs/, with no extension or
// one in docExts. License names match by prefix (LICENSE-MIT).
var docKinds = []struct {
	key, label, icon string
	stems            []string
}{
	{"license", "License", "law", []string{"LICENSE", "LICENCE", "COPYING"}},
	{"security", "Security", "shield", []string{"SECURITY"}},
	{"contributing", "Contributing", "users", []string{"CONTRIBUTING"}},
	{"conduct", "Code of conduct", "conduct", []string{"CODE_OF_CONDUCT", "CODE-OF-CONDUCT"}},
	{"support", "Support", "support", []string{"SUPPORT"}},
	{"governance", "Governance", "organization", []string{"GOVERNANCE"}},
	{"funding", "Funding", "heart", []string{"FUNDING"}},
	{"citation", "Citation", "quote", []string{"CITATION"}},
	{"authors", "Authors", "user", []string{"AUTHORS", "CONTRIBUTORS", "MAINTAINERS"}},
	{"notice", "Notice", "info", []string{"NOTICE"}},
	{"changelog", "Changelog", "history", []string{"CHANGELOG", "CHANGES", "HISTORY", "NEWS", "RELEASES"}},
}

// docExts: Markdown renders, the rest are shown as plain text.
var docExts = []string{"", "MD", "MARKDOWN", "TXT", "RST", "ADOC", "YML", "YAML", "CFF", "BIB"}

type BlobPage struct {
	Repo          *repo
	Tab           string
	Crumbs        []Crumb
	Path, Raw     string
	Name          string // last path segment
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
	if r.Issues, err = b.Gitea.Issues(ctx, name); err != nil && !errors.Is(err, gitea.ErrNotFound) {
		return nil, err
	}
	if err := b.loadComments(ctx, r); err != nil {
		return nil, err
	}
	src := b.Cfg.GiteaURL + "/" + url.PathEscape(b.Cfg.User) + "/" + url.PathEscape(name) + ".git"
	if r.Git, err = gitx.Mirror(ctx, src, filepath.Join(b.Data, "mirrors", name+".git")); err != nil {
		return nil, err
	}
	langs, err := b.Gitea.Languages(ctx, name)
	if err != nil && !errors.Is(err, gitea.ErrNotFound) {
		return nil, err
	}
	r.Langs = langBar(langs)
	if meta.Empty {
		return r, nil
	}
	if r.Head, err = r.Git.Head(ctx, meta.DefaultBranch); err != nil {
		return nil, err
	}
	if r.Commits, err = r.Git.CommitCount(ctx, r.Head); err != nil {
		return nil, err
	}
	if r.Branches, err = r.Git.Refs(ctx, "heads"); err != nil {
		return nil, err
	}
	if r.Tags, err = r.Git.Refs(ctx, "tags"); err != nil {
		return nil, err
	}
	switch st, err := b.Gitea.Status(ctx, name, r.Head); {
	case err == nil && st.TotalCount > 0:
		r.Status = st
	case err != nil && !errors.Is(err, gitea.ErrNotFound):
		return nil, err
	}
	if r.Sigs, err = b.Gitea.Signatures(ctx, name, r.Head, b.Cfg.MaxCommits); err != nil && !errors.Is(err, gitea.ErrNotFound) {
		return nil, err
	}
	log, err := r.Git.Log(ctx, r.Head, b.Cfg.MaxCommits, "")
	r.Listed, r.Recent = map[string]bool{}, log
	for _, c := range log {
		r.Listed[c.Hash] = true
	}
	return r, err
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
	var readme template.HTML
	if p.Rows, readme, err = b.listing(ctx, r, "", kids[""], last); err != nil {
		return err
	}
	for _, e := range kids[""] {
		switch u := strings.ToUpper(e.Name); {
		case e.Name == "go.mod":
			page.GoImport = goImport(b.Cfg.SiteURL, r.Name)
			p.GoGet = siteHost(b.Cfg.SiteURL) + "/" + r.Name
		case r.License == "" && e.Type == "blob" && e.Size < 64<<10 &&
			(strings.HasPrefix(u, "LICENSE") || strings.HasPrefix(u, "LICENCE") || strings.HasPrefix(u, "COPYING")):
			src, err := r.Git.Blob(ctx, e.Hash)
			if err != nil {
				return err
			}
			r.License, r.LicenseHref = licenseName(src), "/"+r.Name+"/blob/"+render.EscapePath(e.Name)
		}
	}
	if p.Docs, err = b.docs(ctx, r, readme, kids); err != nil {
		return err
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
			var readme template.HTML
			if tp.Rows, readme, err = b.listing(ctx, r, e.Name, kids[e.Name], last); err != nil {
				return err
			}
			if readme != "" {
				tp.Docs = []Doc{{"readme", "README", "book", readme}}
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

// docs collects the README tabs for the repo root: the README, then the
// first match of each docKinds entry, looking in the root, .github/ and docs/.
func (b *Builder) docs(ctx context.Context, r *repo, readme template.HTML, kids map[string][]gitx.Entry) ([]Doc, error) {
	var out []Doc
	if readme != "" {
		out = append(out, Doc{"readme", "README", "book", readme})
	}
	for _, k := range docKinds {
	search:
		for _, dir := range []string{"", ".github", "docs"} {
			for _, e := range kids[dir] {
				if e.Type != "blob" || e.Size > maxRender || !isDoc(path.Base(e.Name), k.stems, k.key == "license") {
					continue
				}
				src, err := r.Git.Blob(ctx, e.Hash)
				if err != nil {
					return nil, err
				}
				h, err := readmeHTML(path.Base(e.Name), src, "/"+r.Name, dir)
				if err != nil {
					return nil, err
				}
				label := k.label
				if k.key == "license" && r.License != "" && r.License != "license" {
					label = r.License + " license"
				}
				out = append(out, Doc{k.key, label, k.icon, h})
				break search
			}
		}
	}
	return out, nil
}

func isDoc(name string, stems []string, prefix bool) bool {
	stem, ext, _ := strings.Cut(strings.ToUpper(name), ".")
	if !slices.Contains(docExts, ext) {
		return false
	}
	for _, s := range stems {
		if stem == s || prefix && strings.HasPrefix(stem, s) {
			return true
		}
	}
	return false
}

// listing builds the rows and README for one directory.
func (b *Builder) listing(ctx context.Context, r *repo, dir string, entries []gitx.Entry, last map[string]gitx.Commit) ([]Row, template.HTML, error) {
	var readme template.HTML
	rows := make([]Row, 0, len(entries))
	for _, e := range entries {
		row := Row{Entry: e, Base: path.Base(e.Name), Last: last[e.Name]}
		row.CommitHref = r.CommitHref(row.Last.Hash)
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
	p := BlobPage{Repo: r, Tab: "code", Crumbs: crumbs(r.Name, e.Name), Path: e.Name, Name: path.Base(e.Name), Size: e.Size}
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
	return rd.Write(dir, r.Name+"/blob/"+e.Name+".html", "blob", render.Page{Title: e.Name + " · " + r.Name, D: &p})
}

// loadComments fetches the comments of every issue and pull that has any.
// Issues and pulls share one numbering, so one map holds both.
func (b *Builder) loadComments(ctx context.Context, r *repo) error {
	var nums []int
	for _, x := range r.Issues {
		if x.Comments > 0 {
			nums = append(nums, x.Number)
		}
	}
	for _, x := range r.Pulls {
		if x.Comments > 0 {
			nums = append(nums, x.Number)
		}
	}
	r.Comments = map[int][]gitea.Comment{}
	for _, n := range nums {
		cs, err := b.Gitea.Comments(ctx, r.Name, n)
		if err != nil && !errors.Is(err, gitea.ErrNotFound) {
			return fmt.Errorf("comments on #%d: %w", n, err)
		}
		r.Comments[n] = cs
	}
	return nil
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
	return siteHost(siteURL) + "/" + name + " git " + siteURL + "/" + name + ".git"
}

func siteHost(siteURL string) string {
	return strings.TrimPrefix(strings.TrimPrefix(siteURL, "https://"), "http://")
}

// licenseName names common licenses from their text; anything else is just
// "license".
func licenseName(src []byte) string {
	t := string(src[:min(len(src), 2048)])
	for _, l := range []struct{ marker, name string }{
		{"GNU AFFERO GENERAL PUBLIC LICENSE", "AGPL-3.0"},
		{"GNU LESSER GENERAL PUBLIC LICENSE", "LGPL"},
		{"GNU GENERAL PUBLIC LICENSE\n                       Version 2", "GPL-2.0"},
		{"GNU GENERAL PUBLIC LICENSE", "GPL-3.0"},
		{"Apache License", "Apache-2.0"},
		{"Mozilla Public License", "MPL-2.0"},
		{"Permission is hereby granted, free of charge", "MIT"},
		{"ISC License", "ISC"},
		{"Redistribution and use in source and binary forms", "BSD"},
		{"This is free and unencumbered software", "Unlicense"},
	} {
		if strings.Contains(t, l.marker) {
			return l.name
		}
	}
	return "license"
}

// langBar turns Gitea's bytes-per-language into bar segments: the three
// largest, plus "other" for the rest, so the legend fits one line on phones.
func langBar(bytes map[string]int64) []Lang {
	var total int64
	names := make([]string, 0, len(bytes))
	for n, b := range bytes {
		names, total = append(names, n), total+b
	}
	if total == 0 {
		return nil
	}
	slices.SortFunc(names, func(a, c string) int { return cmp.Or(cmp.Compare(bytes[c], bytes[a]), cmp.Compare(a, c)) })
	var out []Lang
	x, rest := 0.0, 0.0
	for i, n := range names {
		pct := 100 * float64(bytes[n]) / float64(total)
		if i >= 3 || pct < 0.5 {
			rest += pct
			continue
		}
		out = append(out, Lang{Name: n, Pct: pct, X: x, W: pct * 10})
		x += pct * 10
	}
	if rest > 0 {
		out = append(out, Lang{Name: "other", Pct: rest, X: x, W: 1000 - x})
	}
	return out
}
