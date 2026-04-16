# ── Frontend build stage ──────────────────────────────────────────────────────
FROM node:22-alpine AS frontend

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

# ── Go build stage ───────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-X github.com/beacon-stack/haul/internal/version.Version=${VERSION} \
                -X github.com/beacon-stack/haul/internal/version.BuildTime=${BUILD_TIME} \
                -s -w" \
      -o /haul ./cmd/haul

# ── Final stage ───────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates is needed for outbound HTTPS to trackers and indexers.
# tzdata provides timezone data for correct time display in the UI.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 haul

COPY --from=builder /haul /usr/local/bin/haul

# /config holds the local database and settings; /downloads holds torrent data.
VOLUME ["/config", "/downloads"]

# Web UI + API / peer-wire / peer-wire UDP.
EXPOSE 8484 6881 6881/udp

USER haul

ENTRYPOINT ["/usr/local/bin/haul"]
