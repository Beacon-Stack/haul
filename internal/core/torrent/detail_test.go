package torrent

// detail_test.go — Regression tests for the per-torrent detail APIs
// (Peers / Pieces / Trackers) added for the torrent detail page.
//
// These guard the contract that the frontend's canvas piece bar and peer
// list rely on. See plans/haul-torrent-detail-enhancements.md for the full
// design context. If these tests fail, something in anacrolix's API shape
// changed or the Session helper drifted away from what the plan assumed.

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

// addTestTorrent builds a metainfo for a random payload and adds it to the
// session. Returns the info hash and the raw .torrent bytes (callers may
// need the metainfo for peer wiring).
func addTestTorrent(t *testing.T, s *Session, trackers [][]string) (hash string, miBytes []byte) {
	t.Helper()

	payloadDir := t.TempDir()
	payload := make([]byte, 64*1024) // 64 KiB
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}
	payloadPath := filepath.Join(payloadDir, "payload.bin")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	info := metainfo.Info{
		PieceLength: 16 * 1024, // 16 KiB pieces → 4 pieces total for a 64 KiB file
		Name:        "payload.bin",
	}
	if err := info.BuildFromFilePath(payloadPath); err != nil {
		t.Fatalf("build info: %v", err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib, AnnounceList: trackers}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("encode metainfo: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := s.Add(ctx, AddRequest{File: buf.Bytes(), SavePath: t.TempDir()})
	if err != nil {
		t.Fatalf("Session.Add: %v", err)
	}

	// Wait for metadata to be ready so piece state is queryable.
	s.mu.RLock()
	mt := s.torrents[resp.InfoHash]
	s.mu.RUnlock()
	if mt == nil {
		t.Fatalf("torrent not in session map after Add")
	}
	select {
	case <-mt.t.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("GotInfo timed out")
	}
	// Give the session loop one tick to mark the torrent ready.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		ready := mt.ready
		s.mu.RUnlock()
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return resp.InfoHash, buf.Bytes()
}

// ── Peers ─────────────────────────────────────────────────────────────────────

// TestSession_Peers_UnknownHash asserts Peers returns an error for a hash
// that isn't in the session (matches the Get() behaviour and lets the HTTP
// handler return a clean 404).
func TestSession_Peers_UnknownHash(t *testing.T) {
	s := newTestSession(t)
	_, err := s.Peers("0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for unknown hash, got nil")
	}
}

// TestSession_Peers_EmptyWhenNoConnections is the baseline case: a torrent
// added but with no peers wired up returns an empty slice (not nil, not
// error) so the frontend always gets a stable shape.
func TestSession_Peers_EmptyWhenNoConnections(t *testing.T) {
	s := newTestSession(t)
	hash, _ := addTestTorrent(t, s, nil)
	peers, err := s.Peers(hash)
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	if peers == nil {
		t.Error("expected empty slice, got nil — frontend depends on stable shape")
	}
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}

// TestSession_Peers_IncludesConnectedPeer wires a second anacrolix client
// as a direct peer and asserts Peers() reports it. This is the end-to-end
// test of the PeerConns → PeerInfo translation path.
func TestSession_Peers_IncludesConnectedPeer(t *testing.T) {
	s := newTestSession(t)

	// Build a seeder.
	seederRoot := t.TempDir()
	payload := make([]byte, 64*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}
	payloadPath := filepath.Join(seederRoot, "payload.bin")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	info := metainfo.Info{PieceLength: 16 * 1024, Name: "payload.bin"}
	if err := info.BuildFromFilePath(payloadPath); err != nil {
		t.Fatalf("build info: %v", err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib}

	seederCfg := lt.NewDefaultClientConfig()
	seederCfg.DataDir = seederRoot
	seederCfg.ListenPort = 0
	seederCfg.NoDHT = true
	seederCfg.DisablePEX = true
	seederCfg.DisableIPv6 = true
	seederCfg.DefaultStorage = storage.NewFile(seederRoot)
	seederCfg.Seed = true
	// Throttle the seeder's upload hard so the download takes multiple
	// seconds instead of completing in <10ms on loopback. This keeps the
	// peer connection alive long enough for our poll loop to observe it.
	// 4 KB/s limit × 64 KB payload = ~16s wall clock — plenty of time.
	seederCfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(4*1024), 8*1024)
	seeder, err := lt.NewClient(seederCfg)
	if err != nil {
		t.Fatalf("seeder client: %v", err)
	}
	defer seeder.Close()
	seederT, err := seeder.AddTorrent(&mi)
	if err != nil {
		t.Fatalf("seeder add: %v", err)
	}
	<-seederT.GotInfo()
	_ = seederT.VerifyDataContext(context.Background())

	// Add same torrent to the haul session.
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("encode metainfo: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := s.Add(ctx, AddRequest{File: buf.Bytes(), SavePath: t.TempDir()})
	if err != nil {
		t.Fatalf("Session.Add: %v", err)
	}
	hash := resp.InfoHash

	s.mu.RLock()
	mt := s.torrents[hash]
	s.mu.RUnlock()
	<-mt.t.GotInfo()
	mt.t.DownloadAll()

	// Inject seeder as direct peer.
	seederAddrs := seeder.ListenAddrs()
	peerInfo := make([]lt.PeerInfo, 0, len(seederAddrs))
	for _, a := range seederAddrs {
		peerInfo = append(peerInfo, lt.PeerInfo{
			Addr:    a,
			Source:  lt.PeerSourceDirect,
			Trusted: true,
		})
	}
	if added := mt.t.AddPeers(peerInfo); added == 0 {
		t.Fatal("AddPeers returned 0")
	}

	// Poll until we see at least one peer connection established. We wait
	// up to 15s — same envelope as TestSessionIntegration_DownloadFromPeer,
	// which reliably completes in well under that. Anything less is flaky
	// on slower CI runners.
	var peers []PeerInfo
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		peers, err = s.Peers(hash)
		if err != nil {
			t.Fatalf("Peers: %v", err)
		}
		if len(peers) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(peers) == 0 {
		t.Fatalf("expected at least one connected peer, got none within 15s (bytes completed: %d)", mt.t.BytesCompleted())
	}

	// Sanity-check that fields we promised are populated on the first peer.
	p := peers[0]
	if p.Addr == "" {
		t.Error("peer.Addr is empty")
	}
	if p.Network == "" {
		t.Error("peer.Network is empty")
	}
	// Progress should be non-zero — the seeder has 100% of pieces and will
	// have sent its bitfield almost immediately.
	if p.Progress <= 0 {
		t.Errorf("peer.Progress = %f, want > 0 (seeder should report full bitfield)", p.Progress)
	}
}

// ── Pieces ────────────────────────────────────────────────────────────────────

// TestSession_Pieces_UnknownHash asserts the error path matches Peers.
func TestSession_Pieces_UnknownHash(t *testing.T) {
	s := newTestSession(t)
	_, err := s.Pieces("0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
}

// TestSession_Pieces_ReturnsRunsForReadyTorrent asserts the contract that
// the frontend canvas bar relies on: numPieces matches the metainfo, piece
// size is real, and the runs sum to numPieces.
//
// If this ever fails, the canvas widget's bucketing math (runsToColumns in
// pieceBarGeometry.ts) will silently over- or under-fill the bar. Keep this
// strict.
func TestSession_Pieces_ReturnsRunsForReadyTorrent(t *testing.T) {
	s := newTestSession(t)
	hash, _ := addTestTorrent(t, s, nil)

	pieces, err := s.Pieces(hash)
	if err != nil {
		t.Fatalf("Pieces: %v", err)
	}
	if pieces == nil {
		t.Fatal("expected non-nil PiecesInfo for a ready torrent")
	}
	if pieces.NumPieces != 4 {
		t.Errorf("NumPieces = %d, want 4 (64 KiB file / 16 KiB pieces)", pieces.NumPieces)
	}
	if pieces.PieceSize != 16*1024 {
		t.Errorf("PieceSize = %d, want 16384", pieces.PieceSize)
	}
	if len(pieces.Runs) == 0 {
		t.Fatal("Runs is empty")
	}

	// Every piece must be accounted for exactly once.
	sum := 0
	for _, r := range pieces.Runs {
		sum += r.Length
		switch r.State {
		case "complete", "partial", "checking", "missing":
		default:
			t.Errorf("unexpected state %q in run", r.State)
		}
	}
	if sum != pieces.NumPieces {
		t.Errorf("runs sum to %d, want %d — the canvas bar would render wrong", sum, pieces.NumPieces)
	}
}

// ── Trackers ──────────────────────────────────────────────────────────────────

// TestSession_Trackers_FromMetainfo asserts AnnounceList is flattened with
// tier numbers preserved. This is the data the frontend TrackerList renders.
func TestSession_Trackers_FromMetainfo(t *testing.T) {
	s := newTestSession(t)
	announceList := [][]string{
		{"udp://tracker.example.com:1337", "udp://backup.example.com:1337"}, // tier 0
		{"http://fallback.example.com/announce"},                             // tier 1
	}
	hash, _ := addTestTorrent(t, s, announceList)

	trackers, err := s.Trackers(hash)
	if err != nil {
		t.Fatalf("Trackers: %v", err)
	}
	if len(trackers) != 3 {
		t.Fatalf("expected 3 trackers, got %d", len(trackers))
	}

	// Tier 0 entries come first, tier 1 after.
	tier0Count := 0
	tier1Count := 0
	for _, tr := range trackers {
		switch tr.Tier {
		case 0:
			tier0Count++
		case 1:
			tier1Count++
		default:
			t.Errorf("unexpected tier %d for %q", tr.Tier, tr.URL)
		}
	}
	if tier0Count != 2 {
		t.Errorf("tier 0 count = %d, want 2", tier0Count)
	}
	if tier1Count != 1 {
		t.Errorf("tier 1 count = %d, want 1", tier1Count)
	}
}

// TestSession_Trackers_UnknownHash asserts the error path.
func TestSession_Trackers_UnknownHash(t *testing.T) {
	s := newTestSession(t)
	_, err := s.Trackers("0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
}
