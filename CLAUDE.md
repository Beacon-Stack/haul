# Haul: notes for Claude Code

Haul is the Beacon stack's BitTorrent client. `cmd/haul` is the entry
point, `internal/` the logic, `web/ui` the React UI. It listens on 8484.

## Working here

- Work on a branch and merge through a pull request. Never commit to `main`.
- `gh` commands target `Beacon-Stack/haul`.
- `make check` before pushing: lint, dead-code checks, Go tests,
  type-check and frontend tests.
- Comments say what the code does and why, not how it got that way;
  history belongs in the commit message.
- `web/ui/src/shared/` is a copy of the `web-shared` repo. Change it
  there, not here.
- The UI is embedded in the binary from `web/static/`, which is built by
  `make web` and not committed (only a placeholder `index.html` is). The
  Docker image and releases build it themselves.
- The Docker image builds with the Go version in `docker/Dockerfile`.
  After `go get`, check the `go` line in `go.mod` didn't move past it.

## Torrents that never find peers

Torrents stuck at zero peers have come from wiring mistakes more than
once: the listen port not reaching the session, DHT misconfigured,
trackers not injected, the IP blocklist, a periodic job not started. The
tests pin each one. Run `make test` before changing:

- `internal/core/torrent/session.go` (session wiring)
- `internal/core/torrent/stall.go` (stall detection and `ListStalled`)
- `internal/core/torrent/trackers.go`
- `internal/config/load.go` (environment variables and defaults)
- `cmd/haul/main.go` (the stall ticker: if it isn't running, stall
  detection is silently off)

If a test fails, fix the code, not the test. They run in under two
seconds; don't put them behind build tags or `-short`.

## Settings

- A new `torrent.*` setting needs a `SetDefault` in
  `internal/config/load.go`, or its environment variable is dropped
  silently. Add it to `TestLoadFromEnv_Critical`.
- A setting that should take effect without a restart needs a field on
  the session, a setter, and a case in `applyRuntimeSettings`
  (`internal/api/v1/settings.go` has the checklist). Without that the
  toggle saves and does nothing.

## Shared with Pilot

Pilot's stall watcher polls `/api/v1/stalls`. Its response shape is a
contract: change it together with Pilot's
`plugins/downloaders/haul/haul.go`.
