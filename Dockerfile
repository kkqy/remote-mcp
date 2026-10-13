# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
ENV CGO_ENABLED=0 \
    TMPDIR=/src/.tmp \
    GOTMPDIR=/src/.tmp \
    GOCACHE=/src/.tmp/go-cache \
    GOMODCACHE=/src/.tmp/go-mod-cache
RUN mkdir -p /src/.tmp
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates bash procps python3 python-is-python3 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 remote-mcp \
    && useradd --uid 10001 --gid 10001 --home-dir /workspace --no-create-home --shell /bin/bash remote-mcp \
    && mkdir -p /workspace/.tmp \
    && chown -R 10001:10001 /workspace
COPY --from=build /out/remote-mcp /out/remote-mcp-transfer /usr/local/bin/
COPY --chmod=755 scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
ENV HOME=/workspace \
    TMPDIR=/workspace/.tmp \
    TMP=/workspace/.tmp \
    TEMP=/workspace/.tmp
WORKDIR /workspace
USER 10001:10001
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["--listen", "0.0.0.0:8080"]
