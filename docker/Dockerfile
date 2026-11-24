# Frontend build stage
# The UI is plain JS/CSS, the same for every platform, so build it once
# on the runner's own platform instead of again under arm64 emulation.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend

WORKDIR /app

# Copy the frontend package files first so the dependency install layer
# caches until they change.
COPY web/ui/package.json web/ui/package-lock.json ./web/ui/
RUN cd web/ui && npm ci

# Copy the rest of the frontend source, including the vendored shared
# components under src/shared/ (git subtree from beacon-stack/web-shared).
COPY web/ui/ ./web/ui/
RUN cd web/ui && npm run build
# Output lands in /app/web/static/

# Go build stage
# Go cross-compiles: build natively and target the image's platform with
# GOOS/GOARCH. Emulating arm64 made this step ~10x slower (466 s vs 48 s).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

ARG VERSION=dev
ARG BUILD_TIME=unknown

WORKDIR /app

# Download dependencies first so the layer caches until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Full source after dependency fetch.
COPY . .

# Overwrite web/static/ with the freshly-built frontend from the earlier stage.
COPY --from=frontend /app/web/static/ ./web/static/

# anacrolix/torrent compiles cleanly with CGO_ENABLED=0 for a fully static
# binary. No cgo dependencies in haul's tree.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags "-X github.com/beacon-stack/haul/internal/version.Version=${VERSION} \
                -X github.com/beacon-stack/haul/internal/version.BuildTime=${BUILD_TIME} \
                -s -w" \
      -o /haul ./cmd/haul

# Files for the final image, which has no shell to make them: the config
# folder (owned by the app's user) and a "wget" link to the app, so
# compose health checks written for the old image keep working.
RUN mkdir -p /rootfs/config /rootfs/usr/bin && ln -s /usr/local/bin/haul /rootfs/usr/bin/wget


# Final stage
# Distroless: the app, CA certificates, timezone data and /tmp. No shell,
# no package manager, nothing to patch. Pinned by digest; bump on purpose.
FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2

COPY --from=builder /haul /usr/local/bin/haul
COPY --from=builder /rootfs/usr/bin/ /usr/bin/
COPY --from=builder --chown=1000:1000 /rootfs/config /config

ENV HAUL_DATABASE_PATH=/config/haul.db \
    HAUL_PULSE_URL=http://pulse:9696 \
    HAUL_TORRENT_LISTEN_PORT=6881 \
    HAUL_TORRENT_DOWNLOAD_DIR=/downloads \
    HAUL_LOG_LEVEL=info \
    TZ=UTC \
    HOME=/config

VOLUME ["/config", "/downloads"]

EXPOSE 8484 6881 6881/udp

# The same user ID as the old Alpine image, so existing /config folders
# keep working.
USER 1000:1000

HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["/usr/local/bin/haul", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/haul"]
