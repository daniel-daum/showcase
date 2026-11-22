package gitea

import (
	"archive/tar"
	"bufio"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// Registry reads public images from Gitea's container registry, for the
// packages page. Gitea answers /v2/ with a bearer challenge even for public
// images, then hands out a pull token without credentials. That token can
// only read what anyone can `docker pull`, so this stays as anonymous as the
// API client. The token is kept in memory and never rendered.
type Registry struct {
	c     *Client
	image string // "<user>/<image>"
	token string
	// RealmHost is the host name in the token challenge: Gitea's ROOT_URL, which
	// may be a tailnet name. Callers treat it as private.
	RealmHost string
}

const (
	maxManifest = 4 << 20
	maxLayer    = 64 << 20 // bigger base layers aren't read for os-release
)

var acceptManifests = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Platform    *Platform         `json:"platform"`
	Annotations map[string]string `json:"annotations"`
}

type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

// Manifest is an image index (Manifests set) or an image (Config, Layers).
type Manifest struct {
	MediaType string       `json:"mediaType"`
	Manifests []Descriptor `json:"manifests"`
	Config    Descriptor   `json:"config"`
	Layers    []Descriptor `json:"layers"`
}

// ImageConfig holds the parts of an image config the page shows. Env is
// left out on purpose: it is never read, so it can't be rendered.
type ImageConfig struct {
	Created      time.Time `json:"created"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	Variant      string    `json:"variant"`
	Config       struct {
		User         string              `json:"User"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		Entrypoint   []string            `json:"Entrypoint"`
		Cmd          []string            `json:"Cmd"`
		Labels       map[string]string   `json:"Labels"`
	} `json:"config"`
}

var (
	serviceRe = regexp.MustCompile(`service="([^"]*)"`)
	realmRe   = regexp.MustCompile(`realm="([^"]*)"`)
	digestRe  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Registry starts a pull session for one image.
func (c *Client) Registry(ctx context.Context, image string) (*Registry, error) {
	r := &Registry{c: c, image: c.user + "/" + image}
	resp, err := r.do(ctx, "/v2/", "")
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return r, nil
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("registry: GET /v2/: %s", resp.Status)
	}
	// The realm names Gitea's ROOT_URL; ask the configured base instead.
	service, challenge := "container_registry", resp.Header.Get("Www-Authenticate")
	if m := serviceRe.FindStringSubmatch(challenge); m != nil {
		service = m[1]
	}
	if m := realmRe.FindStringSubmatch(challenge); m != nil {
		if u, err := url.Parse(m[1]); err == nil {
			r.RealmHost = u.Hostname()
		}
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	q := url.Values{"service": {service}, "scope": {"repository:" + r.image + ":pull"}}
	b, err := r.read(ctx, "/v2/token?"+q.Encode(), "", maxManifest)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	if r.token = cmp.Or(tok.Token, tok.AccessToken); r.token == "" {
		return nil, fmt.Errorf("registry: no anonymous token for %s: %w", image, ErrNotFound)
	}
	return r, nil
}

func (r *Registry) do(ctx context.Context, p, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.c.base+p, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if r.token != "" && !strings.HasPrefix(p, "/v2/token") {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	return r.c.http.Do(req)
}

func (r *Registry) read(ctx context.Context, p, accept string, limit int64) ([]byte, error) {
	resp, err := r.do(ctx, p, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("registry: GET %s: %s: %w", p, resp.Status, ErrNotFound)
	default:
		return nil, fmt.Errorf("registry: GET %s: %s", p, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("registry: %s is over %d bytes", p, limit)
	}
	return b, err
}

// Tags lists the image's tags (the first 100).
func (r *Registry) Tags(ctx context.Context) ([]string, error) {
	var t struct {
		Tags []string `json:"tags"`
	}
	b, err := r.read(ctx, "/v2/"+r.image+"/tags/list?n=100", "", maxManifest)
	if err != nil {
		return nil, err
	}
	return t.Tags, json.Unmarshal(b, &t)
}

// Manifest fetches a manifest by tag or digest and returns its digest.
// A manifest fetched by digest must hash to it.
func (r *Registry) Manifest(ctx context.Context, ref string) (*Manifest, string, error) {
	b, err := r.read(ctx, "/v2/"+r.image+"/manifests/"+url.PathEscape(ref), acceptManifests, maxManifest)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if strings.HasPrefix(ref, "sha256:") && ref != digest {
		return nil, "", fmt.Errorf("registry: manifest %s hashes to %s", ref, digest)
	}
	var m Manifest
	return &m, digest, json.Unmarshal(b, &m)
}

// Config fetches and checks an image config blob.
func (r *Registry) Config(ctx context.Context, d Descriptor) (*ImageConfig, error) {
	if !digestRe.MatchString(d.Digest) {
		return nil, fmt.Errorf("registry: bad config digest %q", d.Digest)
	}
	b, err := r.read(ctx, "/v2/"+r.image+"/blobs/"+d.Digest, "", maxManifest)
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(b); "sha256:"+hex.EncodeToString(sum[:]) != d.Digest {
		return nil, fmt.Errorf("registry: config %s doesn't match its digest", d.Digest)
	}
	var c ImageConfig
	return &c, json.Unmarshal(b, &c)
}

// ErrNoOSRelease means a layer has no os-release, or can't be read for one
// (zstd, or over maxLayer).
var ErrNoOSRelease = errors.New("registry: no os-release in layer")

// OSRelease reads a gzip or plain tar layer for /etc/os-release (following
// the usual symlink to /usr/lib/os-release) and returns its PRETTY_NAME, or
// NAME and VERSION_ID.
func (r *Registry) OSRelease(ctx context.Context, d Descriptor) (string, error) {
	gz := strings.HasSuffix(d.MediaType, "gzip")
	if !digestRe.MatchString(d.Digest) || d.Size > maxLayer || !gz && !strings.HasSuffix(d.MediaType, ".tar") {
		return "", ErrNoOSRelease
	}
	resp, err := r.do(ctx, "/v2/"+r.image+"/blobs/"+d.Digest, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry: layer %s: %s", d.Digest, resp.Status)
	}
	var in io.Reader = bufio.NewReader(io.LimitReader(resp.Body, maxLayer))
	if gz {
		if in, err = gzip.NewReader(in); err != nil {
			return "", err
		}
	}
	tr := tar.NewReader(in)
	var etc, usr []byte
	linked := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch name := path.Clean(strings.TrimPrefix(h.Name, "./")); {
		case name == "etc/os-release" && h.Typeflag == tar.TypeSymlink:
			linked = true
		case name == "etc/os-release" && h.Typeflag == tar.TypeReg:
			etc, _ = io.ReadAll(io.LimitReader(tr, 16<<10))
		case name == "usr/lib/os-release" && h.Typeflag == tar.TypeReg:
			usr, _ = io.ReadAll(io.LimitReader(tr, 16<<10))
		}
		if etc != nil || linked && usr != nil {
			break
		}
	}
	if etc == nil {
		etc = usr
	}
	if v := parseOSRelease(etc); v != "" {
		return v, nil
	}
	return "", ErrNoOSRelease
}

func parseOSRelease(b []byte) string {
	kv := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			kv[k] = strings.Trim(v, `"'`)
		}
	}
	if v := kv["PRETTY_NAME"]; v != "" {
		return v
	}
	return strings.TrimSpace(kv["NAME"] + " " + kv["VERSION_ID"])
}
