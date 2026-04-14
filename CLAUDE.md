# Haul — Beacon Torrent Download Client

## Project Structure

Standard Beacon Go app layout:
- `cmd/haul/main.go` — entry point
- `internal/` — core business logic
- `web/` — embedded React SPA
- `pkg/` — shared/public packages

## Development

```bash
# Build
go build -o haul ./cmd/haul

# Run
./haul

# Lint + type check
make check

# Run tests
go test ./...
```

## Docker

Haul runs in Docker behind a VPN container. The frontend is embedded into the Go binary via `//go:embed` from `web/static/`. **UI changes require two steps:**

1. Rebuild the frontend (outputs to `web/static/`)
2. Rebuild the Docker image (embeds `web/static/` into the Go binary)

```bash
# From haul root — rebuild frontend then Docker
cd web/ui && npm run build && cd ../../docker
docker compose build --no-cache haul && docker compose up -d haul
```

Editing `.tsx` source files alone won't update the served UI — `web/static/` must be regenerated first.

## Conventions

- Always work on feature branches, never directly on `main`
- Pre-push checks: `make check` (golangci-lint + TypeScript compile)
- App name is a top-level constant in `internal/version/version.go` — use it everywhere
- Follow the same patterns as Prism and Pulse (Chi + Huma, sqlc, Goose, Viper)
- Port: 8484

## Regression guard: torrent stalls

Torrent "stalls at 0 peers" has regressed multiple times due to subtle
wiring bugs (self-dialing, DHT misconfig, listen port env binding,
tracker injection, IP blocklist, orphan periodic methods). A full
regression suite exists to catch every failure mode we've seen —
**run `make test` before touching any of the files below**:

- `internal/core/torrent/session.go` (NewSession wiring)
- `internal/core/torrent/stall.go` (CheckStalls, ListStalled, StallLevel)
- `internal/core/torrent/trackers.go` (`DefaultPublicTrackers`)
- `internal/config/load.go` (env var BindEnv, torrent.* defaults)
- `cmd/haul/main.go` (the `stallTicker` wiring — if this goroutine isn't
  running, stall detection is silently disabled)
- `../docker/docker-compose.yml` (haul service env, VPN ports)

### The dead-torrent regression suite

Every file below is in `go test ./...` (default `make test`). No build
tags, no `-short` gating — they're cheap and need to run every time.

**Session + listen port + IP blocklist**:
- `internal/core/torrent/session_integration_test.go`
  - `TestSessionIntegration_DownloadFromPeer` — spins up two real anacrolix
    clients on loopback, wires them via `AddPeers`, asserts download
    succeeds. Catches listen port / IPBlocklist / DHT config / Session.Add
    regressions. Runs in ~200ms, no external dependencies.

**Stall detection** (`internal/core/torrent/stall_test.go`):
- `TestCheckStalls_NoPeersEver_FiresAfterTimeout` — adds a torrent with no
  peers, waits past `firstPeerTimeout`, asserts a `TypeTorrentStalled`
  event fires with `reason=no_peers_ever`. **This is the headline
  regression test.** If it fails, torrents with zero peers are invisible
  to stall detection again — the original 847-seeders-stuck-at-0% bug.
- `TestCheckStalls_SessionStartupGrace` — asserts no stall event fires
  during the 10-minute session startup grace period. Prevents false
  positives when Haul is warming up its DHT routing table.
- `TestListStalled_ReturnsNoPeersEverTorrents` — asserts `ListStalled()`
  (the bulk HTTP endpoint Pilot's stallwatcher polls) returns the dead
  torrent. Both `CheckStalls` and `ListStalled` must agree.
- `TestListStalled_SkipsGracePeriod` — asserts `ListStalled` honors the
  startup grace.
- `TestFirstPeerAt_NilUntilFirstPeer` — locks down the `firstPeerAt`
  invariant: a torrent that never sees a peer keeps `firstPeerAt == nil`.

**Config / env var binding** (`internal/config/config_test.go`):
- `TestLoadFromEnv_Critical` — asserts `HAUL_TORRENT_LISTEN_PORT`,
  `HAUL_PULSE_URL`, `HAUL_DATABASE_DSN`, `HAUL_TORRENT_PAUSE_ON_COMPLETE`,
  `HAUL_TORRENT_RENAME_ON_COMPLETE` actually reach the config struct.
  Catches the Viper `AutomaticEnv` gotcha where a missing `SetDefault` or
  `BindEnv` silently drops the env var. The `pause_on_complete` case was
  a real production bug — the toggle in the UI appeared to save but the
  env var on docker-compose was dropped, so the startup config stayed
  `false` regardless. **If you add a new `torrent.*` bool setting, add it
  to this test AND to the `SetDefault` list in `internal/config/load.go`.**

**Runtime settings dispatch** (`internal/api/v1/settings_test.go`,
`internal/core/torrent/stall_test.go` `TestPauseOnComplete_*`):
- `TestApplyRuntimeSettings_PauseOnComplete` — asserts the PUT /api/v1/settings
  handler actually dispatches `pause_on_complete` to the live Session via
  `SetPauseOnComplete`. Before this test existed, the UI toggle was a
  phantom write: it persisted to the Postgres `settings` table but nothing
  in the torrent engine ever read that table, so the toggle never took
  effect at runtime.
- `TestApplyRuntimeSettings_AlsoAcceptsOne` — "1" is truthy alongside "true".
- `TestApplyRuntimeSettings_UnknownKeyIgnored` — unknown/startup-only keys
  don't crash the dispatcher.
- `TestApplyRuntimeSettings_NilSession` — graceful no-op.
- `TestPauseOnComplete_RuntimeToggle` (in the `torrent` package) — the
  concurrent Get/Set contract on the Session side.
- `TestPauseOnComplete_InitializesFromCfg` — `cfg.PauseOnComplete=true`
  propagates to `Session.PauseOnComplete()`.

**Adding a new runtime-effective setting**: follow the checklist at the top
of `internal/api/v1/settings.go`. Add a runtimeMu-protected field on
Session, a Set*() method, a case in `applyRuntimeSettings`, and extend the
test here. If you don't, the toggle will appear to save but have no effect.

### Rules

- If a test fails, **fix the code, not the test**. The failure message
  names the specific file and line to check.
- Never gate these behind build tags or `-short`. The whole suite runs
  in under 2 seconds and must be on every developer's every save.
- When adding new stall classification branches or new
  `managedTorrent` fields relevant to stall detection, extend the suite.
  `stall_test.go` has a shared `newTestSession` helper — reuse it.
- The Pilot side of this system has its own regression suite — see
  `pilot/CLAUDE.md` for the stallwatcher / blocklist / filter pass tests.
  Changes in Haul that affect the `/api/v1/stalls` endpoint shape must
  be coordinated with Pilot's `stallwatcher` package.
