<p align="center">
  <h1 align="center">Haul</h1>
  <p align="center">A self-hosted BitTorrent download client built for the Beacon media stack.</p>
</p>
<p align="center">
  <a href="https://github.com/beacon-stack/haul/blob/main/LICENSE"><img src="https://img.shields.io/github/license/beacon-stack/haul" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25">
</p>
<p align="center">
  <a href="https://beaconstack.io">Website</a> ·
  <a href="https://github.com/beacon-stack/haul/issues">Bug Reports</a>
</p>

---

**Haul** is a BitTorrent download client with a React web UI and a REST API, built to be driven by [Pilot](https://github.com/beacon-stack/pilot) and [Prism](https://github.com/beacon-stack/prism). It wraps [anacrolix/torrent](https://github.com/anacrolix/torrent) with the service-level concerns that library intentionally leaves out: category-based save paths, stall detection with failure-reason hand-off, webhooks, rate-smoothed ETAs, and a rename-on-complete pipeline that understands TV and movie metadata.

You can also run it on its own as a standalone qBittorrent-ish client if you just want a clean modern web UI and don't need the rest of the Beacon stack.

## Features

**Torrenting**

- Full anacrolix/torrent session with persistent piece-completion store — torrents resume correctly across container restarts
- DHT, PEX, µTP, and HTTP/magnet-URL adds
- Public tracker bootstrap for torrents that arrive trackerless
- IP blocklist support
- Per-torrent and global upload/download rate limiting
- Sequential download mode for preview/streaming
- First/last piece priority for media players
- Categories with save-path templating
- Tags for filtering and routing

**Operations**

- Stall detection classifies dead-torrent failure modes (`no_peers_ever`, `activity_lost`, etc.) and surfaces them on a `/api/v1/stalls` endpoint that Pilot's stallwatcher consumes for automatic blocklisting
- Rate tracker smooths anacrolix's cumulative byte counters into real bytes-per-second values using an exponential moving average with a 5-second time constant, producing qBittorrent-parity download rate and ETA
- Webhook dispatcher with per-event filters
- VPN health check — detects tunnel interfaces and external IP to confirm traffic is egressing through the VPN
- Rename-on-complete for TV and movies, fed by Pilot or Prism metadata passed through at grab time
- Pause-on-complete toggle for ratio-limited trackers
- Graceful shutdown persists in-flight state

**UI**

- React 19 + Vite frontend embedded in the Go binary via `//go:embed`
- Torrent list with live progress, speed, ETA, peers, seeds, and status
- Torrent detail page with files, peers, trackers, and piece bar visualization
- Categories and tags CRUD
- Media management settings — rename format, colon replacement, pause-on-complete, verify-on-start
- Webhook configuration
- System and health pages
- WebSocket live updates — no polling

**Operations**

- Single static binary, no runtime dependencies
- Postgres-backed state (torrents, categories, tags, settings)
- Zero telemetry
- Auto-generated API key on first run
- OpenAPI documentation at `/api/docs`
- Default port 8484

## Getting started

### Docker Compose (recommended, as part of the Beacon stack)

The easiest way to run Haul is as part of the full Beacon stack — see [`beacon-stack/stack`](https://github.com/beacon-stack/stack) for the full docker-compose setup with Postgres, Pulse, Pilot, Prism, and Haul behind a VPN container.

### Standalone Docker

```bash
docker run -d \
  --name haul \
  -p 8484:8484 \
  -v /path/to/config:/config \
  -v /path/to/downloads:/downloads \
  ghcr.io/beacon-stack/haul:latest
```

Open `http://localhost:8484`. Haul ships with sensible defaults and a local Postgres fallback for standalone use.

### Build from source

Requires Go 1.25+ and Node.js 22+.

```bash
git clone https://github.com/beacon-stack/haul
cd haul
cd web/ui && npm ci && npm run build && cd ../..
make build
./bin/haul
```

## Configuration

Haul works with zero configuration. All settings are editable through the web UI or via environment variables.

### Key environment variables

| Variable | Default | Description |
|---|---|---|
| `HAUL_SERVER_HOST` | `0.0.0.0` | Bind address |
| `HAUL_SERVER_PORT` | `8484` | HTTP port |
| `HAUL_DATABASE_DSN` | | Postgres connection string |
| `HAUL_TORRENT_LISTEN_PORT` | `6881` | Peer-wire listen port |
| `HAUL_TORRENT_DOWNLOADS_PATH` | `/downloads` | Default save path |
| `HAUL_TORRENT_STALL_TIMEOUT` | `120` | Seconds of inactivity before a torrent is classified as stalled |
| `HAUL_TORRENT_RENAME_ON_COMPLETE` | `false` | Rename downloaded files using Pilot/Prism metadata on completion |
| `HAUL_TORRENT_PAUSE_ON_COMPLETE` | `false` | Pause torrents when they finish downloading |
| `HAUL_AUTH_API_KEY` | auto-generated | API key for external access |
| `HAUL_PULSE_URL` | | Pulse control-plane URL (optional) |

### Config file

Haul looks for `config.yaml` in `/config/config.yaml`, `~/.config/haul/config.yaml`, `/etc/haul/config.yaml`, or `./config.yaml` (in that order).

## Where Haul fits in the Beacon stack

```
┌──────────┐     ┌──────────┐     ┌──────────┐
│  Pilot   │     │  Prism   │     │  Pulse   │
│   (TV)   │     │ (Movies) │     │ (control │
│          │     │          │     │  plane)  │
└────┬─────┘     └────┬─────┘     └────┬─────┘
     │                │                │
     │ grab torrent   │ grab torrent   │
     ▼                ▼                │
     ┌───────────────────────┐         │
     │        Haul           │◄────────┘
     │   (BitTorrent)        │  stall events,
     │                       │  webhook callbacks
     └───────────┬───────────┘
                 │
                 ▼
             downloads/
```

Pilot and Prism grab releases by pushing a `POST /api/v1/torrents` to Haul. Haul downloads, renames using metadata the parent service passed through, and publishes a webhook when the torrent completes. Pilot's stallwatcher polls `/api/v1/stalls` to catch dead torrents and blocklist them before they waste another retry.

You can also use Haul standalone — none of the Beacon-specific features are mandatory.

## Privacy

Haul makes outbound connections only to peers (BitTorrent), trackers, and the optional Pulse URL you configure. No telemetry, no analytics, no crash reporting, no update checks. Credentials and API keys are stored in your local database only.

## Project structure

```
cmd/haul/                 Entry point
internal/
  api/                    HTTP router, middleware, v1 handlers, WebSocket hub
  config/                 Configuration loading (Viper + env vars)
  core/
    category/             Category CRUD + save-path templating
    renamer/              TV/movie rename pipeline
    tag/                  Tag CRUD
    torrent/              anacrolix session wrapper, stall detection, rate tracker, webhooks
  db/                     Migrations and generated query code (sqlc)
  events/                 In-process event bus
  pulse/                  Optional Pulse control-plane integration
  version/                Build-time version info
web/
  embed.go                Go embed for serving the SPA
  static/                 Built frontend assets
  ui/                     React 19 + TypeScript + Vite source
```

## Development

```bash
make build         # compile binary to bin/haul
make run           # build + run
make dev           # hot reload with air
make test          # go test ./...
make check         # golangci-lint + tsc --noEmit
make sqlc          # regenerate SQLC code
```

The full regression suite runs in under 2 seconds and locks in the dead-torrent failure modes the project has regressed into in the past — see [CLAUDE.md](CLAUDE.md) for the guarded files and rationale.

## Contributing

Bug reports, feature requests, and pull requests are welcome. Please open an issue before starting large changes.

## License

MIT
