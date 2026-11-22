# syntax=docker/dockerfile:1

# Base images are pinned by digest. To bump: crane digest <image>:<tag>
FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/daumdev ./cmd/daumdev

# git is needed at runtime for the mirrors, which is why this isn't distroless.
FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
# hadolint ignore=DL3018
RUN apk add --no-cache git ca-certificates \
    && mkdir /data \
    && chown 10001:10001 /data
COPY --from=build /out/daumdev /usr/local/bin/daumdev
# the mirrors are owned by this user, so no safe.directory is needed
USER 10001:10001
ENV HOME=/data
EXPOSE 8080
VOLUME /data
# /healthz is 503 until the first build is published
HEALTHCHECK --interval=30s --timeout=5s --start-period=5m \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/usr/local/bin/daumdev"]
CMD ["-config", "/config.json", "-data", "/data", "-listen", ":8080"]
