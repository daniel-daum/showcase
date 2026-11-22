// Package gitea is a minimal, anonymous Gitea API client. It never sends
// credentials, so it can only see what the public can see.
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
	base string
	user string
	http *http.Client
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
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Merged    bool      `json:"merged"`
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

// fetch GETs base+path (never anything else) without credentials.
func (c *Client) fetch(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return nil, err
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
	b, err := c.fetch(ctx, path)
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

// Packages returns the user's packages that are linked to one of repos.
// Package visibility follows the owner, not the repo, so the filter matters.
func (c *Client) Packages(ctx context.Context, repos []string) ([]Package, error) {
	allowed := map[string]bool{}
	for _, r := range repos {
		allowed[r] = true
	}
	var out []Package
	for page := 1; page <= 20; page++ {
		var ps []Package
		path := fmt.Sprintf("/api/v1/packages/%s?limit=50&page=%d", url.PathEscape(c.user), page)
		if err := c.get(ctx, path, &ps); err != nil {
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
	b, err := c.fetch(ctx, u.RequestURI())
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
