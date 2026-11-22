package build

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"daum.dev/showcase/internal/gitea"
)

const (
	maxImageTags     = 30 // tags whose manifests are fetched
	maxImageVersions = 8  // distinct digests shown
)

// ImageView is one container image on the packages page, read from the
// registry. Versions are distinct digests, newest first, each with every
// tag that points at it.
type ImageView struct {
	Name, Ref string // "repo-a", "daum.dev/daniel/repo-a"
	Pull      string // docker pull of the newest tag
	Versions  []ImageVersion
	More      int // digests not shown
	Missing   bool
}

type ImageVersion struct {
	Tags      []string
	Digest    string
	Kind      string // "OCI index", "Docker image", …
	Created   time.Time
	Pull      string // docker pull by digest
	Platforms []ImagePlatform
	Meta      []Meta
}

type ImagePlatform struct {
	Name    string // linux/arm64/v8
	Digest  string
	Size    int64 // config plus compressed layers, what a pull downloads
	Layers  int
	Distro  string // from os-release, "" if none
	Created time.Time
	config  *gitea.ImageConfig
}

// Meta is one line of image metadata. Href is set for safe links only.
type Meta struct{ Key, Value, Href string }

var mediaKinds = map[string]string{
	"application/vnd.oci.image.index.v1+json":                   "OCI index",
	"application/vnd.docker.distribution.manifest.list.v2+json": "Docker manifest list",
	"application/vnd.oci.image.manifest.v1+json":                "OCI image",
	"application/vnd.docker.distribution.manifest.v2+json":      "Docker image",
}

// images lists the allowlisted images shown on a repo's packages page: the
// image named like the repo, plus container packages the API links to it.
// Registry trouble never fails the build; the image is shown without details.
func (b *Builder) images(ctx context.Context, r *repo) []ImageView {
	names := []string{}
	if b.Cfg.HasImage(r.Name) {
		names = append(names, r.Name)
	}
	for _, p := range r.Packages {
		if p.Type == "container" && b.Cfg.HasImage(p.Name) && !slices.Contains(names, p.Name) {
			names = append(names, p.Name)
		}
	}
	var out []ImageView
	for _, n := range names {
		ref := siteHost(b.Cfg.SiteURL) + "/" + b.Cfg.User + "/" + n
		v := ImageView{Name: n, Ref: ref, Pull: "docker pull " + ref}
		if err := b.loadImage(ctx, r, &v); err != nil {
			b.Log.Warn("image details", "image", n, "err", err)
			v.Missing, v.Versions, v.More = true, nil, 0
		}
		out = append(out, v)
	}
	return out
}

func (b *Builder) loadImage(ctx context.Context, r *repo, v *ImageView) error {
	reg, err := b.Gitea.Registry(ctx, v.Name)
	if err != nil {
		return err
	}
	tags, err := reg.Tags(ctx)
	if err != nil {
		return err
	}
	slices.Sort(tags)
	byDigest := map[string]*ImageVersion{}
	for _, tag := range tags[:min(len(tags), maxImageTags)] {
		m, digest, err := reg.Manifest(ctx, tag)
		if err != nil {
			return err
		}
		if ver, ok := byDigest[digest]; ok {
			ver.Tags = append(ver.Tags, tag)
			continue
		}
		ver := &ImageVersion{Tags: []string{tag}, Digest: digest, Kind: cmp.Or(mediaKinds[m.MediaType], "image"), Pull: "docker pull " + v.Ref + "@" + digest}
		byDigest[digest] = ver
		descs := []gitea.Descriptor{{Digest: digest}} // a single image: itself
		if len(m.Manifests) > 0 {
			descs = m.Manifests
		}
		for _, d := range descs {
			if d.Platform != nil && (d.Platform.OS == "unknown" || d.Annotations["vnd.docker.reference.type"] != "") {
				continue // build attestations, not runnable images
			}
			p, err := b.platform(ctx, reg, d, m)
			if err != nil {
				return err
			}
			ver.Platforms = append(ver.Platforms, p)
			if p.Created.After(ver.Created) {
				ver.Created = p.Created
			}
		}
		if len(ver.Platforms) > 0 {
			ver.Meta = b.imageMeta(r, ver.Platforms[0].config, reg.RealmHost)
		}
	}
	for _, ver := range byDigest {
		slices.SortFunc(ver.Tags, func(a, c string) int { // latest first, then newest-looking
			return cmp.Or(cmp.Compare(boolInt(c == "latest"), boolInt(a == "latest")), strings.Compare(c, a))
		})
		v.Versions = append(v.Versions, *ver)
	}
	slices.SortFunc(v.Versions, func(a, c ImageVersion) int {
		return cmp.Or(c.Created.Compare(a.Created), strings.Compare(a.Tags[0], c.Tags[0]))
	})
	if len(v.Versions) > maxImageVersions {
		v.Versions, v.More = v.Versions[:maxImageVersions], len(v.Versions)-maxImageVersions
	}
	if len(v.Versions) > 0 {
		v.Pull += ":" + v.Versions[0].Tags[0]
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// platform loads one runnable image. Everything it reads is addressed by
// digest, so results are cached for the life of the process.
func (b *Builder) platform(ctx context.Context, reg *gitea.Registry, d gitea.Descriptor, top *gitea.Manifest) (ImagePlatform, error) {
	if p, ok := b.st.images[d.Digest]; ok {
		return p, nil
	}
	m := top
	if len(top.Manifests) > 0 {
		var err error
		if m, _, err = reg.Manifest(ctx, d.Digest); err != nil {
			return ImagePlatform{}, err
		}
	}
	cfg, err := reg.Config(ctx, m.Config)
	if err != nil {
		return ImagePlatform{}, err
	}
	p := ImagePlatform{Digest: d.Digest, Layers: len(m.Layers), Size: m.Config.Size, Created: cfg.Created, config: cfg}
	p.Name = strings.Join(slices.DeleteFunc([]string{cfg.OS, cfg.Architecture, cfg.Variant}, func(s string) bool { return s == "" }), "/")
	for _, l := range m.Layers {
		p.Size += l.Size
	}
	for _, l := range m.Layers[:min(len(m.Layers), 3)] { // the base OS is in the first layers
		distro, err := reg.OSRelease(ctx, l)
		if err == nil {
			p.Distro = distro
			break
		}
		if !errors.Is(err, gitea.ErrNoOSRelease) {
			return ImagePlatform{}, err
		}
	}
	if b.st.images == nil {
		b.st.images = map[string]ImagePlatform{}
	}
	b.st.images[d.Digest] = p
	return p, nil
}

var labelMeta = []struct{ label, key string }{
	{"org.opencontainers.image.version", "Version"},
	{"org.opencontainers.image.revision", "Revision"},
	{"org.opencontainers.image.base.name", "Base image"},
	{"org.opencontainers.image.licenses", "Licenses"},
	{"org.opencontainers.image.authors", "Authors"},
	{"org.opencontainers.image.vendor", "Vendor"},
	{"org.opencontainers.image.description", "Description"},
	{"org.opencontainers.image.source", "Source"},
	{"org.opencontainers.image.url", "URL"},
	{"org.opencontainers.image.documentation", "Documentation"},
}

// imageMeta picks the known OCI labels and the runtime settings worth
// showing. Gitea's hosts (the configured one and its ROOT_URL from the
// registry challenge, often a tailnet name) are private: a link to an
// allowlisted repo there points at its page here instead, and any other
// value naming them, or a .ts.net name, is dropped. Only https links are
// linked.
func (b *Builder) imageMeta(r *repo, c *gitea.ImageConfig, realmHost string) []Meta {
	var hosts []string
	if u, err := url.Parse(b.Cfg.GiteaURL); err == nil && u.Hostname() != "" {
		hosts = append(hosts, u.Hostname())
	}
	if realmHost != "" {
		hosts = append(hosts, realmHost)
	}
	private := func(s string) bool {
		return strings.Contains(s, ".ts.net") || slices.ContainsFunc(hosts, func(h string) bool { return strings.Contains(s, h) })
	}
	var out []Meta
	add := func(m Meta) {
		if m.Value != "" && !private(m.Value) {
			out = append(out, m)
		}
	}
	for _, l := range labelMeta {
		v := c.Config.Labels[l.label]
		m := Meta{Key: l.key, Value: v}
		switch l.key {
		case "Revision":
			if len(v) >= 7 {
				m.Value, m.Href = v[:min(len(v), 12)], r.CommitHref(v)
			}
		case "Source", "URL", "Documentation":
			if repo := b.giteaRepo(hosts, v); repo != "" {
				out = append(out, Meta{l.key, siteHost(b.Cfg.SiteURL) + "/" + repo, "/" + repo + "/"})
				continue
			}
			if u, err := url.Parse(v); err != nil || u.Scheme != "https" || u.Host == "" {
				continue
			}
			m.Href = v
		}
		add(m)
	}
	add(Meta{Key: "Entrypoint", Value: strings.Join(c.Config.Entrypoint, " ")})
	add(Meta{Key: "Command", Value: strings.Join(c.Config.Cmd, " ")})
	add(Meta{Key: "Ports", Value: strings.Join(slices.Sorted(maps.Keys(c.Config.ExposedPorts)), ", ")})
	add(Meta{Key: "User", Value: c.Config.User})
	return out
}

// giteaRepo returns the allowlisted repo a URL on one of Gitea's hosts
// points at, or "".
func (b *Builder) giteaRepo(hosts []string, v string) string {
	u, err := url.Parse(v)
	if err != nil || !slices.Contains(hosts, u.Hostname()) {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) == 2 && parts[0] == b.Cfg.User && slices.Contains(b.Cfg.Repos, parts[1]) {
		return parts[1]
	}
	return ""
}
