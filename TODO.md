# Haul — TODO

### qBittorrent API v2 compatibility layer
- Migration path for existing Sonarr/Radarr users
- Map qBit API shapes to Haul's internal types
- Register on the chi router under `/api/v2/`

### Integration tests for the torrent lifecycle
- Every method that touches `mt.t` (the anacrolix torrent handle) can panic
  if called before metadata arrives (`mt.ready == false`); the guards keep
  regressing because nothing verifies coverage automatically
- **Need:** a harness that creates a real `Session`, adds a magnet (which
  stays in the metadata-resolving state), then calls every periodic method
  (`CheckSeedLimits`, `CheckStalls`, `GetHealth`, `GetTransferStats`) to
  verify none of them panic
- **Challenge:** the anacrolix client needs a listen port and takes time to
  initialize; tag as integration tests (`go test -tags=integration`) so they
  don't slow the unit suite
- **Alternative:** extract the ready-guard into one wrapper
  (`forEachReadyTorrent(fn)`) so there's a single place to get it right

### Prometheus /metrics endpoint
- Use `prometheus/client_golang`, register gauges, mount at `/metrics`
