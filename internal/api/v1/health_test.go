package v1

// health_test.go — regression suite for the /api/v1/stalls and
// /api/v1/torrents/{hash}/stall HTTP endpoints. Pilot's stallwatcher
// polls these every 60s and uses the response to populate its release
// blocklist (`stallwatcher.Service.Tick` → `blocklist.Service.AddFromStall`).
// Any drift in the response shape or status code gets noticed here
// before it manifests as silently-broken stall detection in Pilot.
//
// ⚠ Run this before touching:
//   - internal/api/v1/health.go (RegisterHealthRoutes)
//   - internal/core/torrent/stall.go (StallInfo / StalledTorrent shapes,
//     GetStallInfo / ListStalled classifiers)
//   - The stall classifier branches: "no peers ever" pre-metadata path
//     (mt.firstPeerAt == nil && now-addedAt > firstPeerTimeout)

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/events"

	"log/slog"
)

func init() {
	// Same trick the existing settings_test.go uses — short-circuit the
	// 10-second public-IP lookup so this whole file runs in <1s.
	torrent.SetPublicIPDetectTimeoutForTesting(200 * time.Millisecond)
}

func newSessionForHealthTest(t *testing.T) *torrent.Session {
	t.Helper()
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: t.TempDir(),
		DataDir:     t.TempDir(),
		EnableDHT:   false,
		EnablePEX:   false,
		EnableUTP:   false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	session, err := torrent.NewSession(cfg, nil, bus, logger)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(session.Close)
	return session
}

// newAPI builds a humatest API with the health routes registered against
// the supplied Session. Returned alongside is the session itself so the
// caller can seed state before issuing requests.
func newHealthAPI(t *testing.T) (*torrent.Session, humatest.TestAPI) {
	t.Helper()
	session := newSessionForHealthTest(t)
	_, api := humatest.New(t)
	RegisterHealthRoutes(api, session)
	return session, api
}

// ── /api/v1/torrents/{hash}/stall ─────────────────────────────────────────

// Unknown torrent hashes must 404, not 200-with-zero-shaped body.
// Pilot's stallwatcher relies on this distinction to skip torrents that
// have already been removed from Haul (e.g. stall-blocklisted last tick).
func TestGetTorrentStall_UnknownHashReturns404(t *testing.T) {
	_, api := newHealthAPI(t)

	resp := api.Get("/api/v1/torrents/0000000000000000000000000000000000000000/stall")
	if resp.Code != http.StatusNotFound {
		t.Errorf("Code = %d, want %d", resp.Code, http.StatusNotFound)
	}
}

// A torrent in the "added but never saw a peer" state past the
// firstPeerTimeout window must classify as no_peers_ever and the
// response JSON must carry the snake_case field names Pilot's stallwatcher
// expects (`stalled`, `level`, `inactive_secs`, `reason`).
func TestGetTorrentStall_NoPeersEverShape(t *testing.T) {
	prev1 := torrent.SetFirstPeerTimeoutForTesting(10 * time.Millisecond)
	t.Cleanup(func() { torrent.SetFirstPeerTimeoutForTesting(prev1) })

	session, api := newHealthAPI(t)
	hash := session.AddNoPeersTorrentForTesting("seed-no-peers-stall0", time.Now().Add(-1*time.Second))

	resp := api.Get("/api/v1/torrents/" + hash + "/stall")
	if resp.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}

	var body struct {
		Stalled      bool   `json:"stalled"`
		Level        int    `json:"level"`
		InactiveSecs int64  `json:"inactive_secs"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, resp.Body.String())
	}

	if !body.Stalled {
		t.Errorf("stalled = false, want true (no_peers_ever path)")
	}
	if body.Reason != torrent.ReasonNoPeersEver {
		t.Errorf("reason = %q, want %q", body.Reason, torrent.ReasonNoPeersEver)
	}
	if body.Level != int(torrent.StallNoPeersEver) {
		t.Errorf("level = %d, want %d (StallNoPeersEver)", body.Level, int(torrent.StallNoPeersEver))
	}
	if body.InactiveSecs < 0 {
		t.Errorf("inactive_secs = %d, want >= 0", body.InactiveSecs)
	}
}

// ── /api/v1/stalls ────────────────────────────────────────────────────────

// During the session-startup grace period the bulk endpoint must return
// an empty array — not nil and not 500. Pilot's stallwatcher decodes
// the response as `[]StalledTorrent`; nil-vs-empty matters for the
// JSON-array shape and the `len(stalls)` check on the receiving side.
func TestListStalls_EmptyDuringStartupGrace(t *testing.T) {
	prev := torrent.SetSessionStartupGraceForTesting(10 * time.Hour) // never expire
	t.Cleanup(func() { torrent.SetSessionStartupGraceForTesting(prev) })

	session, api := newHealthAPI(t)
	// Even with a clearly-stalled torrent, ListStalled must suppress
	// during the grace period.
	prev2 := torrent.SetFirstPeerTimeoutForTesting(10 * time.Millisecond)
	t.Cleanup(func() { torrent.SetFirstPeerTimeoutForTesting(prev2) })
	session.AddNoPeersTorrentForTesting("seed-grace-suppressionx", time.Now().Add(-1*time.Second))

	resp := api.Get("/api/v1/stalls")
	if resp.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}

	// The contract is `[]StalledTorrent`. Decoding into a slice and
	// asserting len==0 catches both nil-leakage and 500-with-empty-body.
	var body []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, resp.Body.String())
	}
	if len(body) != 0 {
		t.Errorf("len(body) = %d, want 0 during startup grace; got %v", len(body), body)
	}
}

// After the grace window expires and a torrent has been waiting past
// firstPeerTimeout, /api/v1/stalls must list it with the snake_case
// shape Pilot consumes (`info_hash`, `name`, `level`, `reason`,
// `inactive_secs`, `added_at`).
func TestListStalls_ContractShape(t *testing.T) {
	prev1 := torrent.SetSessionStartupGraceForTesting(1 * time.Millisecond)
	prev2 := torrent.SetFirstPeerTimeoutForTesting(10 * time.Millisecond)
	t.Cleanup(func() {
		torrent.SetSessionStartupGraceForTesting(prev1)
		torrent.SetFirstPeerTimeoutForTesting(prev2)
	})

	session, api := newHealthAPI(t)
	wantHash := session.AddNoPeersTorrentForTesting("seed-contract-stall0", time.Now().Add(-1*time.Second))

	// Wait past sessionStartupGrace AND firstPeerTimeout.
	time.Sleep(50 * time.Millisecond)

	resp := api.Get("/api/v1/stalls")
	if resp.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}

	// Decode as the contract Pilot's stallwatcher uses.
	var body []struct {
		InfoHash     string `json:"info_hash"`
		Name         string `json:"name"`
		Level        int    `json:"level"`
		Reason       string `json:"reason"`
		InactiveSecs int64  `json:"inactive_secs"`
		AddedAt      string `json:"added_at"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, resp.Body.String())
	}

	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1; body=%s", len(body), resp.Body.String())
	}
	got := body[0]
	if got.InfoHash != wantHash {
		t.Errorf("info_hash = %q, want %q", got.InfoHash, wantHash)
	}
	if got.Reason != torrent.ReasonNoPeersEver {
		t.Errorf("reason = %q, want %q", got.Reason, torrent.ReasonNoPeersEver)
	}
	if got.Level != int(torrent.StallNoPeersEver) {
		t.Errorf("level = %d, want %d (StallNoPeersEver)", got.Level, int(torrent.StallNoPeersEver))
	}
	if got.InactiveSecs <= 0 {
		t.Errorf("inactive_secs = %d, want > 0", got.InactiveSecs)
	}
	if got.AddedAt == "" {
		t.Errorf("added_at is empty; Pilot's stallwatcher needs this for grab-history correlation")
	}
}
