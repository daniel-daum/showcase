# Deploying showcase

Three stages. Each one works on its own.

1. **code.danieldaum.net**: the container on the server, behind your
   reverse proxy. Gitea is still public at git.danieldaum.net.
2. **Gitea on the tailnet**: only `gitea_url` changes.
3. **danieldaum.net/projects**: saturn's Pages Function serves showcase's
   pages. Clone, `go get` and `docker pull` stay on code.danieldaum.net.

## Release an image

Tagging, building, pushing and pinning a version are in the main
[README](../README.md#release-a-version).

## 1. code.danieldaum.net

On the server, in a folder holding `compose.yaml` and `config.json` from
here:

```sh
docker compose up -d
docker compose logs -f              # the first build mirrors every repo
curl -s 127.0.0.1:8080/healthz      # "ok" once the first build is live (503 until then)
```

- Point your reverse proxy for `code.danieldaum.net` at `127.0.0.1:8080`
  (plain HTTP; the proxy does TLS).
- Optional: put the proxy's address in `trusted_proxies`, so logs show
  visitors' IPs. Behind Cloudflare, `CF-Connecting-IP` is used.
- Gitea must allow anonymous reads (`REQUIRE_SIGNIN_VIEW = false`, HTTP git
  on), or the site is empty and clones fail.
- `images: ["showcase"]` lists the image on the packages page and lets
  `docker pull code.danieldaum.net/daniel/showcase:0.1.0` through.

Saturn: apply `saturn/saturn.patch` (`git apply` in the saturn repo). It:

- shares the palette with code.danieldaum.net through a `theme` cookie
  (`theme.js`), so both sites show the same one;
- adds `social/functions/projects/`, which redirects `/projects/…` to
  code.danieldaum.net (so `/projects/saturn/` lands on the saturn repo);
- takes `/projects/` out of likes and comments (`isLikeable`). The email
  worker uses it too, so run `mise run social-email` after deploying.

The old pages in saturn's `projects/` are never served after this. Delete
them when you like.

## 2. Gitea on the tailnet

Set `gitea_url` to the tailnet address (for example
`https://code.aardvark-halibut.ts.net`) and restart the container. Mirrors
follow the new address on the next build. Nothing else changes: Gitea's
address never reaches a page.

The container has to reach that address, and `*.ts.net` names often don't
resolve inside Docker. In order of preference:

- Gitea on the same host: put both on one Docker network and use
  `http://gitea:3000`. No tailnet hop.
- A Tailscale sidecar container, sharing its network
  (`network_mode: service:tailscale`).
- The Gitea machine's 100.x tailnet IP. Only plain HTTP works this way,
  since the HTTPS certificate is for the name.

Check from the server:
`docker compose exec showcase wget -qO- <gitea_url>/api/v1/version`.

Once Gitea is internal, the server pulls the image from the tailnet name
(`code.aardvark-halibut.ts.net/daniel/showcase`), or from showcase itself
(`code.danieldaum.net/daniel/showcase`).

## 3. danieldaum.net/projects

1. In `config.json`, add `"base_path": "/projects"` and restart. Pages
   move to `code.danieldaum.net/projects/…`, and old addresses redirect
   there. Clone, `go get`, `docker pull` and `/healthz` stay at the root.
2. In saturn's `social/functions/projects/[[path]].js`, set
   `MODE = "proxy"` and deploy. danieldaum.net/projects now serves
   showcase.

Check: `curl -sI https://danieldaum.net/projects/` (200, showcase's
`Content-Security-Policy`), and `git clone https://code.danieldaum.net/<repo>.git`.

Each page view is a Functions request (100k a day on the free plan).
Waiting on code.danieldaum.net doesn't count toward the CPU limit.
