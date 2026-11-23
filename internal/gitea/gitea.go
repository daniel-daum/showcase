// Package gitea is a minimal Gitea API client. Calls are anonymous, so they
// only see what the public can see. The one exception is an optional token
// (SetToken), which only the package listing may send; see tokenPath.
package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxBody = 8 << 20

// ErrNotFound means the data isn't publicly visible: a 404 (missing, or a
// feature turned off) or a 401/403 (Gitea wants a token, e.g. the packages API).
var ErrNotFound = errors.New("gitea: not publicly visible")

type Client struct {
	base  string
	user  string
	http  *http.Client
	token string // packages API only; never sent anywhere else
}

func New(baseURL, user string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		user: user,
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("gitea: refusing redirect")
			},
		},
	}
}

type User struct {
	Login       string `json:"login"`
	FullName    string `json:"full_name"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

type Repo struct {
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Website       string    `json:"website"`
	DefaultBranch string    `json:"default_branch"`
	Topics        []string  `json:"topics"`
	Language      string    `json:"language"`
	UpdatedAt     time.Time `json:"updated_at"`
	CreatedAt     time.Time `json:"created_at"`
	Private       bool      `json:"private"`
	Empty         bool      `json:"empty"`
	Archived      bool      `json:"archived"`
}

type Asset struct {
	Name string `json:"name"` // the download URL is rebuilt, never Gitea's
}

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

type PullRequest struct {
	Number    int        `json:"number"`
	Comments  int        `json:"comments"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	Merged    bool       `json:"merged"`
	CreatedAt time.Time  `json:"created_at"`
	MergedAt  *time.Time `json:"merged_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type Label struct {
	Name string `json:"name"` // the color is dropped: the CSP allows no inline styles
}

type Issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	Comments  int        `json:"comments"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Labels    []Label    `json:"labels"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest *struct{} `json:"pull_request"` // set when the issue is a pull
}

type Comment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type Package struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Version    string `json:"version"`
	Repository *Repo  `json:"repository"`
}

// SetToken lets the package listing see packages that need a token. Every
// other call stays anonymous, so repos, issues, avatars and clones still
// only show what the public sees.
func (c *Client) SetToken(t string) { c.token = t }

// tokenPath reports whether path may carry the token: only the user's
// package listing. Extend it one endpoint at a time.
func (c *Client) tokenPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/packages/"+url.PathEscape(c.user)+"?")
}

// fetch GETs base+path (never anything else). Only with auth, and only for
// a tokenPath, does it send the token.
func (c *Client) fetch(ctx context.Context, path string, auth bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return nil, err
	}
	if auth && c.token != "" {
		if !c.tokenPath(path) {
			return nil, fmt.Errorf("gitea: refusing to send the token to %s", path)
		}
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("GET %s: %s: %w", path, resp.Status, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea: GET %s: %s", path, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	return c.decode(ctx, path, false, v)
}

func (c *Client) decode(ctx context.Context, path string, auth bool, v any) error {
	b, err := c.fetch(ctx, path, auth)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (c *Client) repoPath(repo, rest string) string {
	return "/api/v1/repos/" + url.PathEscape(c.user) + "/" + url.PathEscape(repo) + rest
}

func (c *Client) User(ctx context.Context) (*User, error) {
	var u User
	return &u, c.get(ctx, "/api/v1/users/"+url.PathEscape(c.user), &u)
}

func (c *Client) Repo(ctx context.Context, name string) (*Repo, error) {
	var r Repo
	if err := c.get(ctx, c.repoPath(name, ""), &r); err != nil {
		return nil, err
	}
	if r.Private { // anonymous calls should never see one; refuse anyway
		return nil, fmt.Errorf("repo %s is private: %w", name, ErrNotFound)
	}
	return &r, nil
}

// Status is a commit's combined CI status.
type Status struct {
	State      string `json:"state"`
	TotalCount int    `json:"total_count"`
	Statuses   []struct {
		Status      string `json:"status"`
		Context     string `json:"context"`
		Description string `json:"description"`
	} `json:"statuses"`
}

// Passed counts the successful checks.
func (s *Status) Passed() int {
	n := 0
	for _, x := range s.Statuses {
		if x.Status == "success" {
			n++
		}
	}
	return n
}

func (c *Client) Status(ctx context.Context, repo, sha string) (*Status, error) {
	var s Status
	return &s, c.get(ctx, c.repoPath(repo, "/commits/"+url.PathEscape(sha)+"/status"), &s)
}

func (c *Client) Languages(ctx context.Context, repo string) (map[string]int64, error) {
	var m map[string]int64
	return m, c.get(ctx, c.repoPath(repo, "/languages"), &m)
}

func (c *Client) Releases(ctx context.Context, repo string) ([]Release, error) {
	var rs []Release
	if err := c.get(ctx, c.repoPath(repo, "/releases?draft=false&limit=50"), &rs); err != nil {
		return nil, err
	}
	out := rs[:0]
	for _, r := range rs {
		if !r.Draft {
			out = append(out, r)
		}
	}
	return out, nil
}

func (c *Client) Pulls(ctx context.Context, repo string) ([]PullRequest, error) {
	var ps []PullRequest
	return ps, c.get(ctx, c.repoPath(repo, "/pulls?state=all&sort=recentupdate&limit=50"), &ps)
}

// Issues returns the 50 newest issues (Gitea sorts them by creation),
// pulls left out.
func (c *Client) Issues(ctx context.Context, repo string) ([]Issue, error) {
	var is []Issue
	if err := c.get(ctx, c.repoPath(repo, "/issues?state=all&type=issues&limit=50"), &is); err != nil {
		return nil, err
	}
	out := is[:0]
	for _, i := range is {
		if i.PullRequest == nil {
			out = append(out, i)
		}
	}
	return out, nil
}

// Comments returns the comments on issue or pull n, oldest first.
func (c *Client) Comments(ctx context.Context, repo string, n int) ([]Comment, error) {
	var cs []Comment
	return cs, c.get(ctx, c.repoPath(repo, fmt.Sprintf("/issues/%d/comments", n)), &cs)
}

// Packages returns the user's packages that are linked to one of repos,
// which must be repos that loaded anonymously. Package visibility follows
// the owner, not the repo, and this call may carry the token, so the filter
// is what keeps private packages out.
func (c *Client) Packages(ctx context.Context, repos []string) ([]Package, error) {
	allowed := map[string]bool{}
	for _, r := range repos {
		allowed[r] = true
	}
	var out []Package
	for page := 1; page <= 20; page++ {
		var ps []Package
		path := fmt.Sprintf("/api/v1/packages/%s?limit=50&page=%d", url.PathEscape(c.user), page)
		if err := c.decode(ctx, path, true, &ps); err != nil {
			return nil, err
		}
		for _, p := range ps {
			if p.Repository != nil && !p.Repository.Private && allowed[p.Repository.Name] {
				out = append(out, p)
			}
		}
		if len(ps) < 50 {
			break
		}
	}
	return out, nil
}

// Avatar fetches the user's avatar. Gitea builds avatar URLs from its
// ROOT_URL, so only the path is kept and fetched from the configured base.
func (c *Client) Avatar(ctx context.Context, avatarURL string) ([]byte, string, error) {
	u, err := url.Parse(avatarURL)
	if err != nil {
		return nil, "", err
	}
	b, err := c.fetch(ctx, u.RequestURI(), false)
	if err != nil {
		return nil, "", err
	}
	switch ct := http.DetectContentType(b); ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return b, ct, nil
	default:
		return nil, "", fmt.Errorf("gitea: avatar has type %s", ct)
	}
}
