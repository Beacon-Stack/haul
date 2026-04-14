# ── Frontend build stage ──────────────────────────────────────────────────────
FROM node:22-alpine AS frontend

WORKDIR /app/web/ui

COPY haul/web/ui/package.json haul/web/ui/package-lock.json ./
RUN npm ci

COPY haul/web/ui/ ./

# Shared UI components (Modal, ConfirmDialog). The tsconfig paths + vite
# alias resolve `@beacon-shared/*` to `../../../web-shared/*`, and from
# WORKDIR `/app/web/ui` that's `/web-shared`. Install web-shared's own
# minimal devDependencies (react + @types/react) so TypeScript can find
# react types when type-checking files from that directory.
COPY web-shared/ /web-shared/
RUN cd /web-shared && npm install --no-audit --no-fund --loglevel=error

RUN npm run build
# Output lands in /app/web/static/

# ── Go build stage ───────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Copy Pulse SDK (needed for local replace directive)
COPY pulse/ /pulse/

# Copy Haul source
COPY haul/ .

# Overwrite web/static/ with the freshly built frontend
COPY --from=frontend /app/web/static/ ./web/static/

# Rewrite the replace directive to point to the copied Pulse module
RUN sed -i 's|replace github.com/beacon-stack/pulse => .*|replace github.com/beacon-stack/pulse => /pulse|' go.mod

RUN go mod download
RUN CGO_ENABLED=0 go build -o /haul ./cmd/haul

# ── Final stage ───────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /haul /usr/local/bin/haul

EXPOSE 8484 6881 6881/udp

VOLUME ["/config", "/downloads"]

ENTRYPOINT ["haul"]
