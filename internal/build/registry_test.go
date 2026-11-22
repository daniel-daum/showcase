package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const anonToken = "anon-pull-token"

// fakeRegistry serves daniel/repo-a like Gitea's container registry: a bearer
// challenge whose realm is a tailnet URL, an anonymous token, then tags,
// manifests and blobs. latest and v0.2.0 are a two-platform OCI index (plus a
// build attestation) on an Alpine-like base; v0.1.0 is an older single
// Docker image with no os-release. rev is put in the revision label.
func fakeRegistry(rev string) http.HandlerFunc {
	blobs, manifests := map[string][]byte{}, map[string][]byte{}
	put := func(b []byte) string {
		sum := sha256.Sum256(b)
		d := "sha256:" + hex.EncodeToString(sum[:])
		blobs[d] = b
		return d
	}
	layer := func(files ...tar.Header) []byte {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(zw)
		for _, h := range files {
			body := h.Linkname
			if h.Typeflag == tar.TypeSymlink {
				body = ""
			} else {
				h.Linkname, h.Size = "", int64(len(body))
			}
			h.ModTime, h.Mode = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0o644
			tw.WriteHeader(&h)
			tw.Write([]byte(body))
		}
		tw.Close()
		zw.Close()
		return buf.Bytes()
	}
	// a regular file's body travels in Linkname, to keep the table short
	base := put(layer(
		tar.Header{Name: "etc/", Typeflag: tar.TypeDir},
		tar.Header{Name: "etc/os-release", Typeflag: tar.TypeSymlink, Linkname: "../usr/lib/os-release"},
		tar.Header{Name: "usr/lib/os-release", Typeflag: tar.TypeReg, Linkname: "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.24.4\nPRETTY_NAME=\"Alpine Linux v3.24\"\n"},
	))
	app := put(layer(tar.Header{Name: "usr/local/bin/hello", Typeflag: tar.TypeReg, Linkname: "#!/bin/sh\necho hello\n"}))
	js := func(v any) []byte { b, _ := json.Marshal(v); return b }
	config := func(arch, variant, created string, labels map[string]string) string {
		return put(js(map[string]any{
			"created": created, "os": "linux", "architecture": arch, "variant": variant,
			"config": map[string]any{
				"User": "10001", "Entrypoint": []string{"/usr/local/bin/hello"}, "ExposedPorts": map[string]any{"8080/tcp": map[string]any{}},
				"Env": []string{"SECRET=should-never-render"}, "Labels": labels,
			},
		}))
	}
	labels := map[string]string{
		"org.opencontainers.image.title":       "repo-a",
		"org.opencontainers.image.version":     "0.2.0",
		"org.opencontainers.image.revision":    rev,
		"org.opencontainers.image.licenses":    "MIT",
		"org.opencontainers.image.base.name":   "docker.io/library/alpine:3.24.4",
		"org.opencontainers.image.source":      "http://gitea.tailnet.example:3000/daniel/repo-a",
		"org.opencontainers.image.url":         "https://example.com/repo-a",
		"org.opencontainers.image.description": "built on gitea.tailnet.example",
	}
	image := func(mt, cfg string, layers ...string) []byte {
		var ls []map[string]any
		for _, l := range layers {
			ls = append(ls, map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": l, "size": len(blobs[l])})
		}
		return js(map[string]any{"schemaVersion": 2, "mediaType": mt,
			"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": cfg, "size": len(blobs[cfg])},
			"layers": ls})
	}
	const oci = "application/vnd.oci.image.manifest.v1+json"
	amd := image(oci, config("amd64", "", "2026-10-01T10:00:00Z", labels), base, app)
	arm := image(oci, config("arm64", "v8", "2026-10-01T10:05:00Z", labels), base, app)
	att := image(oci, put([]byte(`{"architecture":"unknown","os":"unknown"}`)))
	for _, m := range [][]byte{amd, arm, att} {
		manifests[put(m)] = m
	}
	desc := func(m []byte, os, arch, variant string, ann map[string]string) map[string]any {
		sum := sha256.Sum256(m)
		return map[string]any{"mediaType": oci, "digest": "sha256:" + hex.EncodeToString(sum[:]), "size": len(m),
			"platform": map[string]string{"os": os, "architecture": arch, "variant": variant}, "annotations": ann}
	}
	index := js(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []any{
		desc(amd, "linux", "amd64", "", nil), desc(arm, "linux", "arm64", "v8", nil),
		desc(att, "unknown", "unknown", "", map[string]string{"vnd.docker.reference.type": "attestation-manifest"}),
	}})
	old := image("application/vnd.docker.distribution.manifest.v2+json", config("amd64", "", "2026-06-02T10:00:00Z", nil), app)
	manifests["latest"], manifests["v0.2.0"], manifests["v0.1.0"] = index, index, old
	manifests[put(index)], manifests[put(old)] = index, old

	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/v2/token" {
			if r.URL.Query().Get("scope") != "repository:daniel/repo-a:pull" {
				http.Error(w, "denied", http.StatusUnauthorized)
				return
			}
			w.Write(js(map[string]string{"token": anonToken}))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+anonToken {
			w.Header().Set("Www-Authenticate", `Bearer realm="http://gitea.tailnet.example:3000/v2/token",service="container_registry",scope="*"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rest, ok := strings.CutPrefix(p, "/v2/daniel/repo-a/")
		switch {
		case p == "/v2/":
			w.Write([]byte("{}"))
		case ok && rest == "tags/list":
			w.Write(js(map[string]any{"name": "daniel/repo-a", "tags": []string{"v0.1.0", "latest", "v0.2.0"}}))
		case ok && strings.HasPrefix(rest, "manifests/") && manifests[strings.TrimPrefix(rest, "manifests/")] != nil:
			w.Write(manifests[strings.TrimPrefix(rest, "manifests/")])
		case ok && strings.HasPrefix(rest, "blobs/") && blobs[strings.TrimPrefix(rest, "blobs/")] != nil:
			w.Write(blobs[strings.TrimPrefix(rest, "blobs/")])
		default:
			http.NotFound(w, r)
		}
	}
}
