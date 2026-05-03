package torrent

// stall_test.go — regression tests for the dead-torrent stall detection
// bug. Three stacked code-level holes in the old CheckStalls skipped any
// torrent that never received its first peer (the `!mt.ready` guard, the
// `BytesMissing == 0` guard, and the `lastActivity.IsZero()` guard). The
// user's 5-year-old dead torrent would sit forever at 0% with nobody
// noticing.
//
// Both tests here run in <1s, use no network, and piggyback on the same
// session test harness as session_integration_test.go (nil DB, in-process
// anacrolix client on loopback, public IP detection short-circuited).

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
)

// TestCheckStalls_NoPeersEver_FiresAfterTimeout is the regression test for
// the exact 847-seed-but-actually-dead failure: a torrent is added, never
// sees a peer, and must be flagged as stalled with reason "no_peers_ever"
// after firstPeerTimeout elapses.
//
// If this test fails, one of the three guards in CheckStalls has come back
// (`!mt.ready`, `BytesMissing == 0`, `lastActivity.IsZero()`), and dead
// torrents are silently invisible to stall detection again.
func TestCheckStalls_NoPeersEver_FiresAfterTimeout(t *testing.T) {
	// Shrink timeouts so the test runs in <1s instead of >3 minutes.
	saved1 := firstPeerTimeout
	saved2 := sessionStartupGrace
	saved3 := publicIPDetectTimeout
	firstPeerTimeout = 50 * time.Millisecond
	sessionStartupGrace = 0
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		firstPeerTimeout = saved1
		sessionStartupGrace = saved2
		publicIPDetectTimeout = saved3
	})

	session := newTestSession(t)

	// Build an info_hash that definitely has no peers anywhere — random
	// 20 bytes, no trackers, no DHT nodes will know it.
	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("no-peers-ever-testhashX"))

	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		// No trackers. No DHT nodes (session has DHT disabled in test setup).
		DisplayName: "dead-torrent-test",
	}
	_, _, err := session.client.AddTorrentSpec(spec)
	if err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}

	// Register it in managedTorrents the way Session.Add does, minus the DB
	// persistence. We can't use Session.Add because it waits for metadata.
	session.mu.Lock()
	_, existed := session.torrents[randomHash.HexString()]
	if existed {
		session.mu.Unlock()
		t.Fatal("torrent already in session map (unexpected)")
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now().Add(-1 * time.Second), // already 1s old at test start
	}
	session.mu.Unlock()

	// Subscribe to stall events. The bus handlers run asynchronously so we
	// use an atomic counter plus a reason buffer to capture the firing.
	var eventCount int32
	var gotReason atomic.Value
	session.bus.Subscribe(func(_ context.Context, e events.Event) {
		if e.Type != events.TypeTorrentStalled {
			return
		}
		atomic.AddInt32(&eventCount, 1)
		if reason, ok := e.Data["reason"].(string); ok {
			gotReason.Store(reason)
		}
	})

	// Wait past firstPeerTimeout, then call CheckStalls.
	time.Sleep(120 * time.Millisecond)
	session.CheckStalls(context.Background())

	// Handlers run in goroutines — give them a moment.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&eventCount) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&eventCount) == 0 {
		t.Fatal("CheckStalls did not fire a stalled event for a torrent with no peers ever. " +
			"This is the \"dead torrent silently invisible\" regression — check that CheckStalls " +
			"no longer bails early on !mt.ready / BytesMissing == 0 / lastActivity.IsZero().")
	}

	reason, _ := gotReason.Load().(string)
	if reason != ReasonNoPeersEver {
		t.Fatalf("expected reason %q, got %q", ReasonNoPeersEver, reason)
	}
}

// TestCheckStalls_SessionStartupGrace verifies that "no peers ever" stalls
// are suppressed during the first few seconds after Session creation, so
// that cold-start false positives don't blocklist everything.
func TestCheckStalls_SessionStartupGrace(t *testing.T) {
	saved1 := firstPeerTimeout
	saved2 := sessionStartupGrace
	saved3 := publicIPDetectTimeout
	firstPeerTimeout = 10 * time.Millisecond
	sessionStartupGrace = 10 * time.Minute // big enough that our fresh session is definitely in grace
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		firstPeerTimeout = saved1
		sessionStartupGrace = saved2
		publicIPDetectTimeout = saved3
	})

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("grace-period-testhash0"))

	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "grace-period-test",
	}
	_, _, err := session.client.AddTorrentSpec(spec)
	if err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}

	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now().Add(-1 * time.Minute), // pretend it's been sitting for a minute
	}
	session.mu.Unlock()

	var eventCount int32
	session.bus.Subscribe(func(_ context.Context, e events.Event) {
		if e.Type == events.TypeTorrentStalled {
			atomic.AddInt32(&eventCount, 1)
		}
	})

	time.Sleep(50 * time.Millisecond)
	session.CheckStalls(context.Background())
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&eventCount) != 0 {
		t.Fatal("CheckStalls fired a stall event during the session startup grace period. " +
			"This is a false-positive regression — torrents added while Haul is still warming " +
			"up (DHT bootstrap, IP detection) should not be flagged as dead.")
	}
}

// TestListStalled_ReturnsNoPeersEverTorrents is the regression test for
// the Haul side of the Pilot stall watcher: a dead torrent must appear in
// the GET /api/v1/stalls endpoint so Pilot can correlate it with a grab
// and blocklist the release. This test covers both the classification
// (CheckStalls path) and the bulk API path (ListStalled) — they use
// slightly different code paths but must agree on the answer.
//
// ⚠ DO NOT delete or weaken this test. The Pilot stallwatcher assumes
// ListStalled returns every "no_peers_ever" torrent. If that contract
// breaks silently, Pilot stops blocklisting dead releases and we're back
// to the 847-seeders-stuck-at-0% bug.
func TestListStalled_ReturnsNoPeersEverTorrents(t *testing.T) {
	saved1 := firstPeerTimeout
	saved2 := sessionStartupGrace
	saved3 := publicIPDetectTimeout
	firstPeerTimeout = 50 * time.Millisecond
	sessionStartupGrace = 0
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		firstPeerTimeout = saved1
		sessionStartupGrace = saved2
		publicIPDetectTimeout = saved3
	})

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("list-stalled-testhashXY"))

	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "list-stalled-test",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now(),
	}
	session.mu.Unlock()

	// Before firstPeerTimeout elapses, ListStalled should return nothing.
	if got := session.ListStalled(); len(got) != 0 {
		t.Fatalf("expected 0 stalled torrents before timeout, got %d", len(got))
	}

	time.Sleep(120 * time.Millisecond)

	got := session.ListStalled()
	if len(got) != 1 {
		t.Fatalf("expected 1 stalled torrent, got %d", len(got))
	}
	if got[0].Reason != ReasonNoPeersEver {
		t.Errorf("expected reason %q, got %q", ReasonNoPeersEver, got[0].Reason)
	}
	if got[0].InfoHash != randomHash.HexString() {
		t.Errorf("wrong info_hash: got %q want %q", got[0].InfoHash, randomHash.HexString())
	}
	if got[0].Level != StallNoPeersEver {
		t.Errorf("expected level StallNoPeersEver, got %d", got[0].Level)
	}
	if got[0].InactiveSecs < 0 {
		t.Errorf("expected inactive_secs >= 0, got %d", got[0].InactiveSecs)
	}
}

// TestListStalled_SkipsGracePeriod verifies that ListStalled respects
// the session startup grace period, matching CheckStalls' behavior.
// If these two functions diverge, Pilot sees false positives during
// Haul warm-up that CheckStalls correctly suppresses — confusing.
func TestListStalled_SkipsGracePeriod(t *testing.T) {
	saved1 := firstPeerTimeout
	saved2 := sessionStartupGrace
	saved3 := publicIPDetectTimeout
	firstPeerTimeout = 10 * time.Millisecond
	sessionStartupGrace = 10 * time.Minute
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		firstPeerTimeout = saved1
		sessionStartupGrace = saved2
		publicIPDetectTimeout = saved3
	})

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("grace-list-testhash000"))

	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "grace-list-test",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now().Add(-1 * time.Minute),
	}
	session.mu.Unlock()

	time.Sleep(50 * time.Millisecond)

	if got := session.ListStalled(); len(got) != 0 {
		t.Fatalf("ListStalled returned %d stalls during grace period; should be 0", len(got))
	}
}

// TestFirstPeerAt_SetOnFirstObservation locks down the invariant that
// firstPeerAt is set exactly once, on the first CheckStalls tick that
// observes ActivePeers > 0. If this regresses, a torrent that briefly
// saw a peer then lost it would still be classified as "no_peers_ever"
// after the 3-minute window — false positive, content loss.
//
// Note: we can't easily simulate "active peers > 0" in an in-process
// test without a second client handshaking over TCP. This test runs
// the happy path (0 peers never seen) and documents the invariant; the
// positive case is covered by TestSessionIntegration_DownloadFromPeer
// in session_integration_test.go which uses a real seeder on loopback.
func TestFirstPeerAt_NilUntilFirstPeer(t *testing.T) {
	saved1 := firstPeerTimeout
	saved2 := sessionStartupGrace
	saved3 := publicIPDetectTimeout
	firstPeerTimeout = 100 * time.Millisecond
	sessionStartupGrace = 0
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		firstPeerTimeout = saved1
		sessionStartupGrace = saved2
		publicIPDetectTimeout = saved3
	})

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("firstpeer-testhash00000"))

	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "firstpeer-test",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now(),
	}
	session.mu.Unlock()

	// Initially nil.
	session.mu.RLock()
	mt := session.torrents[randomHash.HexString()]
	session.mu.RUnlock()
	if mt.firstPeerAt != nil {
		t.Fatal("firstPeerAt should be nil initially")
	}

	// After a few ticks with no peers (ActivePeers stays 0), still nil.
	for i := 0; i < 3; i++ {
		session.CheckStalls(context.Background())
		time.Sleep(10 * time.Millisecond)
	}
	session.mu.RLock()
	if mt.firstPeerAt != nil {
		t.Fatal("firstPeerAt should stay nil when ActivePeers never > 0")
	}
	session.mu.RUnlock()
}

// TestPauseOnComplete_RuntimeToggle is the regression test for the
// "Stop seeding when complete" toggle bug. Two stacked bugs caused it:
//
//  1. The env var HAUL_TORRENT_PAUSE_ON_COMPLETE was silently dropped
//     by the Viper AutomaticEnv gotcha (covered by config_test.go's
//     TestLoadFromEnv_Critical/torrent.pause_on_complete subtest).
//
//  2. Even if cfg.PauseOnComplete were set correctly, the UI toggle
//     wrote to a DB settings table that nothing in the torrent engine
//     ever read. The completion handler hit `s.cfg.PauseOnComplete`
//     which was a value-type frozen at Session construction. So
//     toggling in the UI was a phantom write — never took effect.
//
// This test pins the contract for bug #2: the runtime setter MUST
// affect the live value that monitorCompletion reads. If it breaks,
// the toggle stops working again.
//
// The test doesn't run a full torrent completion flow — that would
// require a real peer + disk I/O. Instead it locks down the
// Get/Set/state-transition behavior directly, which is the surface
// the settings handler writes against.
func TestPauseOnComplete_RuntimeToggle(t *testing.T) {
	saved := publicIPDetectTimeout
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() { publicIPDetectTimeout = saved })

	session := newTestSession(t)

	// Default is false (cfg value propagates through NewSession).
	if session.PauseOnComplete() {
		t.Fatal("PauseOnComplete() should default to false for a fresh test session")
	}

	// Flipping it on should take effect immediately.
	session.SetPauseOnComplete(true)
	if !session.PauseOnComplete() {
		t.Fatal("SetPauseOnComplete(true) did not update the live value — this is " +
			"the phantom-write regression. Check that completion handler reads via " +
			"PauseOnComplete() not s.cfg.PauseOnComplete.")
	}

	// And flipping off.
	session.SetPauseOnComplete(false)
	if session.PauseOnComplete() {
		t.Fatal("SetPauseOnComplete(false) did not take effect")
	}

	// Setter is safe to call concurrently with the reader.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			session.SetPauseOnComplete(i%2 == 0)
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		_ = session.PauseOnComplete()
	}
	<-done
}

// TestPauseOnComplete_InitializesFromCfg verifies the happy path for the
// cfg → Session propagation. A cfg value of true must appear in the live
// PauseOnComplete() reader.
func TestPauseOnComplete_InitializesFromCfg(t *testing.T) {
	saved := publicIPDetectTimeout
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() { publicIPDetectTimeout = saved })

	cfg := config.TorrentConfig{
		ListenPort:      0,
		DownloadDir:     t.TempDir(),
		DataDir:         t.TempDir(),
		EnableDHT:       false,
		EnablePEX:       false,
		EnableUTP:       false,
		PauseOnComplete: true, // <— must propagate
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var noDB *sql.DB
	session, err := NewSession(cfg, noDB, bus, logger)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(session.Close)

	if !session.PauseOnComplete() {
		t.Fatal("cfg.PauseOnComplete=true did not propagate to Session.PauseOnComplete(). " +
			"Check the NewSession initialization — s.pauseOnComplete must be set from cfg.")
	}
}

// newTestSession creates a Session with nil DB and bare essentials. Safe to
// use in tests that just need the Session struct + anacrolix client, not
// the full DB persistence flow.
func newTestSession(t *testing.T) *Session {
	t.Helper()
	tempDir := t.TempDir()
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: tempDir,
		// DataDir must be a writable tempdir — the default `/config` isn't
		// accessible in test envs and NewBoltPieceCompletion would fall
		// back to in-memory, triggering the "will restart from 0%" error
		// log on every test run. Using a tempdir keeps tests quiet and
		// also exercises the bolt path so we catch regressions there.
		DataDir:   t.TempDir(),
		EnableDHT: false,
		EnablePEX: false,
		EnableUTP: false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var noDB *sql.DB
	session, err := NewSession(cfg, noDB, bus, logger)
	if err != nil {
		t.Fatalf("creating test session: %v", err)
	}
	t.Cleanup(session.Close)
	return session
}

// ── GetStallInfo direct unit tests ─────────────────────────────────────────
//
// These guard the per-torrent stall surface (`/api/v1/torrents/{hash}/stall`)
// against drifting from the bulk surface (CheckStalls / ListStalled). All
// three classifiers must agree — a previous incident left GetStallInfo
// reporting "stalled" while ListStalled said "ok," and the fix lives in
// stall.go's three near-duplicate decision blocks staying in sync.

// Unknown hash must return an explanatory error so the HTTP handler
// can map it to a 404 instead of a 500. The exact error text is part
// of the contract the api/v1 handler relies on (line 113 in health.go
// uses the message verbatim in huma.Error404NotFound).
func TestGetStallInfo_UnknownHashReturnsError(t *testing.T) {
	session := newTestSession(t)
	_, err := session.GetStallInfo("0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for unknown hash, got nil")
	}
	if !strings.Contains(err.Error(), "torrent not found") {
		t.Errorf("expected error to mention 'torrent not found', got %q", err.Error())
	}
}

// A torrent past firstPeerTimeout with `firstPeerAt == nil` must
// classify as no_peers_ever — same answer the bulk endpoint gives.
// Both the api/v1 handler and Pilot's stallwatcher rely on this
// being the FIRST branch the classifier hits, before the
// `mt.t.BytesMissing()` access (which would panic for pre-metadata).
func TestGetStallInfo_NoPeersEverPath(t *testing.T) {
	saved1 := firstPeerTimeout
	firstPeerTimeout = 10 * time.Millisecond
	t.Cleanup(func() { firstPeerTimeout = saved1 })

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("getstall-no-peers-1"))
	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "getstall-no-peers",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now().Add(-1 * time.Second), // already past timeout
	}
	session.mu.Unlock()

	info, err := session.GetStallInfo(randomHash.HexString())
	if err != nil {
		t.Fatalf("GetStallInfo: %v", err)
	}
	if !info.Stalled {
		t.Errorf("Stalled = false, want true (no_peers_ever path)")
	}
	if info.Level != StallNoPeersEver {
		t.Errorf("Level = %d, want %d (StallNoPeersEver)", info.Level, StallNoPeersEver)
	}
	if info.Reason != ReasonNoPeersEver {
		t.Errorf("Reason = %q, want %q", info.Reason, ReasonNoPeersEver)
	}
	if info.InactiveSecs <= 0 {
		t.Errorf("InactiveSecs = %d, want > 0", info.InactiveSecs)
	}
}

// Inside the firstPeerTimeout window AND pre-metadata, GetStallInfo
// must return Stalled=false — not a stall, just a brand-new torrent
// still warming up. If this branch breaks, the UI badge falsely
// reports "stalled" the moment a torrent is added.
func TestGetStallInfo_PreMetadataWithinTimeoutNotStalled(t *testing.T) {
	saved1 := firstPeerTimeout
	firstPeerTimeout = 5 * time.Minute // big enough that addedAt=now is well within
	t.Cleanup(func() { firstPeerTimeout = saved1 })

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("getstall-warming-up0"))
	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "getstall-warming-up",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now(), // just added
	}
	session.mu.Unlock()

	info, err := session.GetStallInfo(randomHash.HexString())
	if err != nil {
		t.Fatalf("GetStallInfo: %v", err)
	}
	if info.Stalled {
		t.Errorf("Stalled = true, want false (still within firstPeerTimeout window)")
	}
}

// ── Info.Stalled per-row exposure ─────────────────────────────────────────
//
// The dashboard's `Stalled` filter pill counts torrents where Info.Stalled
// is true. Before this fix, a pre-metadata magnet that never observed a
// peer (the headline "dead torrent" case) was returned as Stalled=false
// from Session.Get / Session.List even though /api/v1/stalls listed it as
// no_peers_ever. The pill silently read 0 while the bulk endpoint reported
// the torrent — the contradiction between two surfaces of the same logic.
//
// This test pins the per-row Info.Stalled to agree with ListStalled for
// the pre-metadata no_peers_ever path. If it fails, the dashboard's
// Stalled counter is broken again.
func TestGetInfo_NoPeersEverPreMetadata_StalledTrue(t *testing.T) {
	saved1 := firstPeerTimeout
	saved2 := sessionStartupGrace
	firstPeerTimeout = 10 * time.Millisecond
	sessionStartupGrace = 0
	t.Cleanup(func() {
		firstPeerTimeout = saved1
		sessionStartupGrace = saved2
	})

	session := newTestSession(t)

	var randomHash metainfo.Hash
	copy(randomHash[:], []byte("getinfo-no-peers-stld"))
	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: randomHash},
		DisplayName:    "getinfo-no-peers-test",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(randomHash)
	session.mu.Lock()
	session.torrents[randomHash.HexString()] = &managedTorrent{
		t:       tHandle,
		addedAt: time.Now().Add(-1 * time.Second), // already past firstPeerTimeout (10ms)
		ready:   false,                             // pre-metadata: no metainfo
	}
	session.mu.Unlock()

	// Wait past firstPeerTimeout so the no-peers-ever rule fires.
	time.Sleep(50 * time.Millisecond)

	info, err := session.Get(randomHash.HexString())
	if err != nil {
		t.Fatalf("Session.Get: %v", err)
	}
	if !info.Stalled {
		t.Fatal("REGRESSION: Info.Stalled = false for a pre-metadata torrent past firstPeerTimeout. " +
			"The dashboard's Stalled filter pill will silently read 0 again while " +
			"/api/v1/stalls lists the torrent — the original visibility bug. " +
			"Check classifyStalled in session.go: rule 3 (no peers ever) must run before " +
			"the !hasInfo guard, matching ListStalled and CheckStalls.")
	}

	// Sanity-check that ListStalled agrees so the two surfaces stay in sync.
	stalls := session.ListStalled()
	if len(stalls) != 1 {
		t.Fatalf("ListStalled() = %d entries, want 1; the per-row Stalled=true must match the bulk surface", len(stalls))
	}
	if stalls[0].Reason != ReasonNoPeersEver {
		t.Errorf("stalls[0].Reason = %q, want %q", stalls[0].Reason, ReasonNoPeersEver)
	}
}
