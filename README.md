<p align="center">
  <h1 align="center">Haul</h1>
  <p align="center">A self-hosted BitTorrent client for home servers and the Beacon media stack.</p>
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

Haul is a BitTorrent client with a React web UI and a REST API. Run it on its own as a modern qBittorrent / Transmission alternative, or alongside [Pulse](https://github.com/beacon-stack/pulse), [Pilot](https://github.com/beacon-stack/pilot), and [Prism](https://github.com/beacon-stack/prism) — the [Beacon Stack](https://beaconstack.io) — where it picks up rename-on-complete, stall blocklisting, and the rest of the integrated *arr pipeline. It's built on [anacrolix/torrent](https://github.com/anacrolix/torrent), runs as a single Go binary, stores state in a single SQLite file, and is configured from the UI or through environment variables.

## Is this for you?

Haul is built to be approachable by default and capable when you want it to be. The out-of-the-box defaults are tuned so you can `docker run` it, open the UI, and be downloading inside of a minute — sensible save paths, a working rate tracker, stall detection, and VPN awareness all on from the start. The deeper features are there too: full REST and WebSocket APIs, configurable stall thresholds, per-category save paths, webhook event routing, custom rename formats. They stay out of your way until you turn them on.

You'll probably like Haul if you:

- Run a homelab and want a torrent client with a modern web UI that doesn't look dated
- Use or plan to use Pilot or Prism for TV and movie management
- Want accurate ETAs and reliable dead-torrent handling without manually babysitting grabs
- Appreciate sensible defaults now and the option to grow into advanced features later

## Features

- **Modern React UI**, live-updated over WebSocket — no polling, no stale progress bars
- **Accurate ETAs.** Rates and time-remaining are computed from a short moving average rather than cumulative totals, so numbers track reality instead of flickering
- **Categories and tags** with per-category save paths and tag-based filtering
- **Rename-on-complete** — when Pilot or Prism grab a torrent and pass through metadata, Haul renames the output into `Show/Season 02/Show - S02E05.mkv` format automatically
- **Stall detection** with three classification levels. Dead torrents (no peers ever, or gone silent past the timeout) are published to `/api/v1/stalls` so Pilot's stallwatcher can blocklist them before they waste another retry
- **VPN awareness.** Haul detects whether it's running inside a VPN tunnel and surfaces the external IP in the dashboard — useful for catching VPN drops before they become a problem
- **Notifications** to Discord, Slack, Telegram, ntfy, Pushover, Gotify, email or another app's URL, sent through Pulse when connected or by Haul itself
- **Global rate limits** with scheduled alternative-speed windows
- **Magnet URIs, DHT, PEX, µTP**, and crash-safe resume via a persistent piece-completion store
- **Full REST API** (OpenAPI docs at `/api/docs`) and a WebSocket event stream at `/api/v1/ws`
- **SQLite-backed state** for torrents, categories, tags, and settings — one file in `/config`, no database server
- **Zero telemetry.** No analytics, no crash reporting, no phoning home

## Getting started

### Docker

Haul runs on its own. It saves torrents in `/data/torrents/<category>`, so Pilot's downloads (category `tv`) go to `/data/torrents/tv` and Prism's (`movies`) to `/data/torrents/movies`.

Set the two paths in the compose file:

- **`/config`**: Haul's database and settings. Keep it on a local disk.
- **`/data`**: your media *and* downloads, in one folder. Give every Beacon app the same host folder at `/data`: imports are then instant hardlinks, and every app sees the same paths so no path mappings are needed. It must be writable by uid 1000.

The compose file is [`docker/docker-compose.yml`](docker/docker-compose.yml). Then:

```bash
docker compose -f docker/docker-compose.yml up -d
```

The web UI is at `http://<this-host>:8484`. Haul generates an API key on first run; find it in Settings → System. To use it from Pilot or Prism, add it there as a download client: `http://<this-host>:8484` plus that API key.

### Running it with the other Beacon apps

Each app ships its own compose file; run the ones you want and connect them. [beaconstack.io](https://beaconstack.io) has larger examples, from Pilot + Haul up to the full stack with Pulse and a VPN.

### Build from source

Requires Go 1.25+ and Node 22+.

```bash
git clone https://github.com/beacon-stack/haul
cd haul
cd web/ui && npm ci && npm run build && cd ../..
make build       # outputs bin/haul
./bin/haul
```

## Configuration

Most settings live in the web UI. For the ones you'll want at container-start time, use environment variables or a YAML config file at `/config/config.yaml` (also searched at `~/.config/haul/config.yaml` and `./config.yaml`).

| Variable | Default | Description |
|---|---|---|
| `HAUL_SERVER_PORT` | `8484` | Web UI and API port |
| `HAUL_TORRENT_LISTEN_PORT` | `6881` | Peer-wire listen port |
| `HAUL_TORRENT_DOWNLOAD_DIR` | `/downloads` | Where torrents are saved, in a subfolder per category. The compose file sets `/data/torrents`. |
| `HAUL_DATABASE_PATH` | `/config/haul.db` | SQLite database file |
| `HAUL_AUTH_API_KEY` | auto | API key; autogenerated on first run if unset |
| `HAUL_PULSE_URL` | — | Pulse URL, e.g. `http://192.168.1.10:9696` (optional) |
| `HAUL_PULSE_API_KEY` | — | Pulse's API key, needed with `HAUL_PULSE_URL` |
| `ADVERTISE_HOST` | container hostname | Address Pulse uses to reach Haul; set it to this host's IP when the apps run as separate compose projects |
| `HAUL_TORRENT_RENAME_ON_COMPLETE` | `false` | Rename completed downloads using media metadata |
| `HAUL_TORRENT_PAUSE_ON_COMPLETE` | `false` | Pause torrents as soon as they finish (for ratio-sensitive trackers) |
| `HAUL_TORRENT_STALL_TIMEOUT` | `120` | Seconds of inactivity before a torrent is classified as stalled |

## Where Haul fits in the Beacon stack

```
┌──────────┐     ┌──────────┐     ┌──────────┐
│  Pilot   │     │  Prism   │     │  Pulse   │
│   (TV)   │     │ (movies) │     │ (control │
│          │     │          │     │  plane)  │
└────┬─────┘     └────┬─────┘     └────┬─────┘
     │                │                │
     │ grab torrent   │ grab torrent   │
     ▼                ▼                │
┌───────────────────────┐              │
│        Haul           │◄─────────────┘
│   (BitTorrent)        │  optional:
│                       │  stall events, notifications
└───────────┬───────────┘
            │
            ▼
        downloads/
```

Pilot and Prism POST to `/api/v1/torrents` when they grab a release, passing through media metadata so Haul can rename on completion. Haul sends notifications on completion and publishes stall events to `/api/v1/stalls`, which Pilot polls to blocklist dead torrents automatically.

You can run Haul standalone and ignore the rest — the media-manager integration is opt-in via the `rename_on_complete` setting and the upstream service passing metadata.

## Power user notes

A few things worth knowing if you want to go deeper than the UI:

**Rate tracker.** anacrolix/torrent exposes cumulative byte counters, not rates. Haul samples those counters on each API request and pushes them through an exponential moving average with a 5-second time constant. Gaps over 30 seconds reset the tracker to avoid extrapolating from stale data. The math lives in `internal/core/torrent/session.go` — tweak the time constant there if the default feels too slow or too twitchy for your connection.

**Stall classification.** One classifier in `internal/core/torrent/stall.go` drives every stall consumer. Reasons on the wire:
- `no_peers_ever` — never saw a single peer after the first-peer window (the classic dead-torrent signal)
- `no_peers` — had peers once, now has none and no data past the stall timeout
- `no_seeders` — connected peers but no seeds, no data past the stall timeout
- `no_data_received` — peers and seeds present, but nothing is flowing

Severity escalates with inactivity: level 1 at `stall_timeout`, level 2 at 2x, level 3 (auto-pause) at 5x. Stalled torrents show up on `/api/v1/stalls`, which Pilot polls to blocklist dead releases.

**Tests.** `make test` runs the Go suite in a couple of seconds. The session-wiring, stall-detection and settings tests pin behaviour that has broken before, so fix the code rather than the test when one fails.

**Notifications.** Set up channels in Pulse and every connected app uses them, or add them in Haul's own Settings when it runs alone. The "Other app (URL)" channel posts each event as JSON, signed if you set a secret.

**API surface.** The REST API is complete — anything the UI does is available over HTTP. Interactive docs at `/api/docs`.

## Privacy

Haul collects nothing: no telemetry, no analytics, no crash reporting. It connects to:

- **peers and trackers** for your torrents, including a short list of public trackers it adds to public torrents;
- **api.ipify.org**, to show the public IP address your torrents use (the VPN check on the dashboard);
- **your notification channels, or Pulse**, if you set them up;
- **GitHub**, only when you press Check for updates.

Your browser also loads the interface fonts from Google Fonts. API keys and credentials stay in your local database.

## Built with Claude

Haul was built by one person with extensive help from [Claude](https://claude.ai) (Anthropic). Architecture, design decisions, bug triage, and this README are mine. Many of the keystrokes are not. If something in the code or the docs doesn't make sense, that's a bug worth reporting — [open an issue](https://github.com/beacon-stack/haul/issues).

## Development

```bash
make build    # compile to bin/haul
make run      # build + run
make web      # rebuild the frontend into web/static
make test     # go test ./...
make check    # lint, dead-code checks, tests, type-check
```

## Contributing

Bug reports, feature requests, and pull requests are welcome. Please open an issue before starting anything large.

## License

MIT — see [LICENSE](LICENSE).
