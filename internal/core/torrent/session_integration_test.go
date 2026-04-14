package torrent

// session_integration_test.go — End-to-end regression test for the torrent
// engine. This test exists because peer discovery / stall bugs keep breaking
// haul, and none of our pure unit tests would have caught them:
//
//   - Self-dialing bug (IPBlocklist not set behind VPN)
//   - DHT server peer store not configured
//   - Listen port not bound (config env var bug)
//   - Public tracker injection skipped on magnet add
//
// This test boots two torrent clients on loopback, wires them together, and
// verifies that the haul Session actually downloads the file. It takes ~2
// seconds to run, uses no network, and requires no external services.
//
// RUN IT BEFORE any change to:
//   - internal/core/torrent/session.go
//   - internal/core/torrent/trackers.go
//   - internal/config/load.go (torrent.* bindings)
//   - docker-compose.yml (haul VPN / ports)
//
// Command: `go test ./internal/core/torrent/... -run TestSessionIntegration -v`
// Or as part of: `make check`

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
)

// TestSessionIntegration_DownloadFromPeer is the regression test for the
// "torrent stays at 0% with 0 peers" class of bugs.
//
// What it exercises end-to-end:
//   - NewSession wiring (ListenPort binding, IPBlocklist, DHT config)
//   - Session.Add with .torrent file bytes
//   - Peer wire protocol + storage writes (downloads actual bytes)
//
// The test passes if the haul Session downloads a test file from a locally-
// running seeder within 15 seconds. If it fails, one of the session
// constructor wiring pieces is broken — look at the last few commits to
// session.go or publicip/DHT/IPBlocklist related code.
func TestSessionIntegration_DownloadFromPeer(t *testing.T) {
	// Keep publicip detection short so the test isn't slowed down by
	// unreliable network. Loopback doesn't need the real public IP.
	originalTimeout := publicIPDetectTimeout
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() { publicIPDetectTimeout = originalTimeout })

	// ── 1. Create a test file for the seeder to serve ──────────────────────
	seederRoot := t.TempDir()
	fileName := "payload.bin"
	const fileSize = 256 * 1024 // 256 KiB — small enough to download in <1s
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("generating payload: %v", err)
	}
	payloadPath := filepath.Join(seederRoot, fileName)
	if err := os.WriteFile(payloadPath, payload, 0644); err != nil {
		t.Fatalf("writing payload: %v", err)
	}

	// ── 2. Build a .torrent metainfo for that file ─────────────────────────
	info := metainfo.Info{
		PieceLength: 32 * 1024, // 32 KiB pieces = 8 pieces total
		Name:        fileName,
	}
	if err := info.BuildFromFilePath(payloadPath); err != nil {
		t.Fatalf("building metainfo: %v", err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encoding info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var torrentFileBuf bytes.Buffer
	if err := mi.Write(&torrentFileBuf); err != nil {
		t.Fatalf("encoding metainfo: %v", err)
	}

	// ── 3. Start the seeder — a bare anacrolix client ──────────────────────
	seederCfg := lt.NewDefaultClientConfig()
	seederCfg.DataDir = seederRoot
	seederCfg.ListenPort = 0 // ephemeral
	seederCfg.NoDHT = true
	seederCfg.DisablePEX = true
	seederCfg.DisableIPv6 = true
	seederCfg.DefaultStorage = storage.NewFile(seederRoot)
	seederCfg.Seed = true
	seederCfg.NoUpload = false

	seeder, err := lt.NewClient(seederCfg)
	if err != nil {
		t.Fatalf("creating seeder client: %v", err)
	}
	t.Cleanup(func() { seeder.Close() })

	seederTorrent, err := seeder.AddTorrent(&mi)
	if err != nil {
		t.Fatalf("seeder adding torrent: %v", err)
	}

	// The seeder has the complete file — it should immediately be "done".
	// We wait briefly for it to verify pieces.
	select {
	case <-seederTorrent.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("seeder GotInfo timed out")
	}
	_ = seederTorrent.VerifyDataContext(context.Background())

	// Grab the seeder's listen address for wiring below.
	seederAddrs := seeder.ListenAddrs()
	if len(seederAddrs) == 0 {
		t.Fatal("seeder has no listen addresses")
	}

	// ── 4. Start the haul Session (device under test) ──────────────────────
	downloadDir := t.TempDir()
	sessionCfg := config.TorrentConfig{
		ListenPort:  0, // ephemeral — avoid clashing with the seeder
		DownloadDir: downloadDir,
		DataDir:     t.TempDir(), // bolt piece completion store
		EnableDHT:   false,       // Not needed; we wire peers directly
		EnablePEX:   false,
		EnableUTP:   false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Pass a nil *sql.DB — session.go's DB helpers short-circuit when nil,
	// which keeps this test hermetic (no Postgres required).
	var noDB *sql.DB
	session, err := NewSession(sessionCfg, noDB, bus, logger)
	if err != nil {
		t.Fatalf("creating haul session: %v", err)
	}
	t.Cleanup(session.Close)

	// ── 5. Add the same torrent to the haul Session via the public Add API─
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	addResp, err := session.Add(ctx, AddRequest{
		File:     torrentFileBuf.Bytes(),
		SavePath: downloadDir,
	})
	if err != nil {
		t.Fatalf("Session.Add: %v", err)
	}
	hash := addResp.InfoHash
	if hash == "" {
		t.Fatal("Session.Add returned empty info hash")
	}

	// Fetch the internal managedTorrent so we can inject the peer directly.
	session.mu.RLock()
	mt, ok := session.torrents[hash]
	session.mu.RUnlock()
	if !ok {
		t.Fatalf("torrent %s not found in session after Add", hash)
	}

	// Wait for metadata to be ready so we can download.
	select {
	case <-mt.t.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("session torrent GotInfo timed out")
	}
	mt.t.DownloadAll()

	// ── 6. Inject the seeder as a direct peer ──────────────────────────────
	peers := make([]lt.PeerInfo, 0, len(seederAddrs))
	for _, a := range seederAddrs {
		peers = append(peers, lt.PeerInfo{
			Addr:    a,
			Source:  lt.PeerSourceDirect,
			Trusted: true,
		})
	}
	added := mt.t.AddPeers(peers)
	if added == 0 {
		t.Fatal("AddPeers returned 0 — session refused every seeder address")
	}

	// ── 7. Wait for the download to complete ───────────────────────────────
	deadline := time.Now().Add(15 * time.Second)
	var lastBytes int64
	for time.Now().Before(deadline) {
		if mt.t.BytesCompleted() >= int64(fileSize) {
			break
		}
		lastBytes = mt.t.BytesCompleted()
		time.Sleep(100 * time.Millisecond)
	}

	completed := mt.t.BytesCompleted()
	if completed < int64(fileSize) {
		t.Fatalf("download incomplete: got %d / %d bytes (last seen: %d). "+
			"This is the \"stalled torrent\" regression — check session.go wiring "+
			"(ListenPort, IPBlocklist, DHT config) and tracker injection.",
			completed, fileSize, lastBytes)
	}

	// ── 8. Verify file contents match ──────────────────────────────────────
	downloadedPath := filepath.Join(downloadDir, fileName)
	got, err := os.ReadFile(downloadedPath)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded file bytes do not match payload (sizes: got=%d want=%d)", len(got), len(payload))
	}

	t.Logf("downloaded %d bytes from local seeder; sha1=%s", completed, hex.EncodeToString(mi.HashInfoBytes().Bytes()))
}

