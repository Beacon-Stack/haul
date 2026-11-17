# Haul — TODO

## Bugs

### BUG-001: Nil pointer panic on torrents without metadata — FIXED

**Fix:** Added `ready` field to `managedTorrent`. Set to `true` after `GotInfo()` fires. All torrent handle method calls are guarded with `if !mt.ready { skip }` across session.go, health.go, control.go, stall.go, and bandwidth.go.

---

### BUG-002: Torrent restore on restart hangs the engine — FIXED

**Fix:** Added `torrent_data BLOB` column (migration 003). After `GotInfo()`, the .torrent metainfo bytes are saved to the DB. On restart, `restoreFromDB()` uses `AddTorrent(metainfo)` for fast-resume — no metadata fetch from peers needed. Legacy rows without .torrent data are cleaned up.

---

### BUG-003: Pulse SDK registration blocks startup — MITIGATED

`pulse.New()` runs in a goroutine so it doesn't block HTTP server startup. The goroutine still hangs until the HTTP timeout expires if Pulse is unreachable, but this is acceptable.

---

### BUG-004: VPN check blocks startup when DNS is slow — FIXED

`CheckVPN()` runs in a goroutine. Session startup uses `publicip.Get4()` (anacrolix's own library) with a 10s timeout for IP detection.

---

### BUG-005: anacrolix-torrent promotes completed files to 0o444 — WORKAROUND IN PLACE

**Root cause (in anacrolix, not ours):** `storage/file-piece.go:259` in `github.com/anacrolix/torrent` does `os.Chmod(f.safeOsPath, filePerm&^0o222)` right after renaming `.part` → final name. Every file inside a multi-file torrent (and every single-file torrent) is left at `0o444` on completion. The library's own log message calls this "setting promoted file to read-only."

**Why it bites us:** On any Haul restart (apt upgrade, image redeploy, crash), anacrolix's restore opens every file in the saved fileset for read+**write** to set up storage. The chmod the library itself applied makes that open fail with `permission denied`. `client.AddTorrent` returns an error and — until BUG-005's workaround — Haul's restore loop in `session.go` called `s.deleteTorrent(hash)`, wiping in-progress download state for the rest of the torrent (e.g. losing E01/E02 progress because E03 was complete-and-locked).

**Workaround (this bug):**
- `session.go` restore loop no longer deletes on `AddTorrent` failure.
- On permission-denied errors, walk the saved fileset, `chmod u+w` any `0o444` files, retry `AddTorrent` once. anacrolix will re-apply `0o444` after re-verification, but the storage handle opens cleanly the second time.
- On any other failure (or if the retry still fails), the torrent stays in the DB with `status='error'` and an error message; the UI surfaces a Retry/Remove affordance.

**Cleaner fix to revisit:**
- Upstream PR or option to make the read-only promotion configurable. Check whether anacrolix already exposes a `StorageOption` we missed.
- Alternative: ship our own `storage.ClientImpl` that doesn't chmod on promotion. The interface is small (`OpenTorrent`, `Piece`, `Flush`).
- Either path lets us delete the chmod-and-retry dance and the `'error'` status code from the restore path.

---

## Feature Gaps

### Pilot media management settings UI
- Prism has a full media management settings page with rename format preview
- Pilot has the backend but no UI — settings must be changed via API or directly in DB
- **Files:** Need to create `pilot/web/ui/src/pages/settings/media-management/MediaManagementPage.tsx`

### Torrent restore / fast-resume
- See BUG-002 above
- Currently torrents are lost across restarts
- Need to store .torrent file bytes or use persistent anacrolix storage

### qBittorrent API v2 compatibility layer
- Migration path for existing Sonarr/Radarr users
- Map qBit API shapes to Haul's internal types
- Register on chi router under `/api/v2/`

### Tracker management UI
- No way to view/add/remove trackers per torrent in the UI
- Backend would need new endpoints since anacrolix's tracker API is limited

### Peer list UI
- No peer list view (IP, client, speed, flags)
- Would need new API endpoint to expose `t.Stats().ConnStats` per peer

### Integration tests for torrent lifecycle (Critical for stability)
- Every method that touches `mt.t` (the anacrolix torrent handle) can panic if called before metadata arrives (`mt.ready == false`)
- We've added `!mt.ready` guards to all known call sites, but this keeps regressing because there's no automated way to verify coverage
- **Need:** A test harness that creates a real `Session` with a real anacrolix client, adds a magnet (which stays in the metadata-resolving state), and then calls every periodic method (`CheckSeedLimits`, `CheckStalls`, `AdaptiveBandwidth`, `GetHealth`, `GetTransferStats`) to verify none of them panic
- **Challenge:** anacrolix client needs a listen port and takes time to initialize; tests would need to be tagged as integration tests (`go test -tags=integration`) so they don't slow down unit test runs
- **Alternative:** Extract the `ready` guard into a single wrapper method (`forEachReadyTorrent(fn)`) so there's only one place to get it right, instead of repeating the guard in every method
- **Impact:** Without this, any new method that iterates over torrents and calls handle methods is a crash waiting to happen

### Prometheus /metrics endpoint
- Planned but not implemented
- Use `prometheus/client_golang`, register gauges, mount at `/metrics`
