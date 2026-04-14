package torrent

// persistence_test.go — Regression test for the "torrent restarts from 0%
// after container restart" bug. anacrolix's default file storage uses an
// in-memory piece-completion map that dies with the process. We must use
// storage.NewFileWithCompletion with a persistent BoltDB store so completion
// state survives restarts — otherwise the bytes on disk are ignored and
// anacrolix re-downloads the whole file.
//
// If this test ever fails, check NewSession in session.go — someone probably
// reverted the NewFileWithCompletion/NewBoltPieceCompletion wiring.

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
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

// TestPieceCompletionSurvivesRestart is the regression guard for the bug
// where Haul restarted torrents from 0% after every container recreate,
// overwriting bytes already downloaded.
//
// End-to-end flow:
//  1. Build a payload and seed it from a loopback anacrolix client.
//  2. Start Haul session #1 with a fresh tempdir DataDir. Download the
//     payload to completion.
//  3. Close session #1. The bolt completion store should flush completion
//     state to `<DataDir>/.torrent.bolt.db` before the client returns.
//  4. Start Haul session #2 pointed at the SAME DataDir and the SAME
//     DownloadDir. Re-add the torrent.
//  5. Assert that BytesCompleted reflects the full download IMMEDIATELY
//     after GotInfo — no re-hashing, no re-downloading. If the bolt store
//     was wired correctly, anacrolix reads the completion from the sidecar
//     DB and trusts it.
//
// If anyone reverts the NewFileWithCompletion wiring (back to naked
// storage.NewFile), BytesCompleted in step 5 will be 0 and this test will
// fail hard with a descriptive message.
func TestPieceCompletionSurvivesRestart(t *testing.T) {
	// Keep publicip detection short — loopback doesn't need real network.
	prev := publicIPDetectTimeout
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() { publicIPDetectTimeout = prev })

	// Persistent directories survive across the two Session lifecycles.
	dataDir := t.TempDir()
	downloadDir := t.TempDir()

	// ── 1. Build payload + metainfo ─────────────────────────────────────────
	seederRoot := t.TempDir()
	const fileName = "resume-payload.bin"
	const fileSize = 256 * 1024 // 256 KiB
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}
	payloadPath := filepath.Join(seederRoot, fileName)
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	info := metainfo.Info{PieceLength: 32 * 1024, Name: fileName}
	if err := info.BuildFromFilePath(payloadPath); err != nil {
		t.Fatalf("build info: %v", err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib}
	var miBuf bytes.Buffer
	if err := mi.Write(&miBuf); err != nil {
		t.Fatalf("encode metainfo: %v", err)
	}

	// ── 2. Start seeder ─────────────────────────────────────────────────────
	seederCfg := lt.NewDefaultClientConfig()
	seederCfg.DataDir = seederRoot
	seederCfg.ListenPort = 0
	seederCfg.NoDHT = true
	seederCfg.DisablePEX = true
	seederCfg.DisableIPv6 = true
	seederCfg.DefaultStorage = storage.NewFile(seederRoot)
	seederCfg.Seed = true
	seeder, err := lt.NewClient(seederCfg)
	if err != nil {
		t.Fatalf("seeder: %v", err)
	}
	defer seeder.Close()
	seederT, err := seeder.AddTorrent(&mi)
	if err != nil {
		t.Fatalf("seeder add: %v", err)
	}
	<-seederT.GotInfo()
	_ = seederT.VerifyDataContext(context.Background())
	seederAddrs := seeder.ListenAddrs()

	// ── 3. Session #1: download the payload to completion ──────────────────
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: downloadDir,
		DataDir:     dataDir, // persistent across restarts
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	var noDB *sql.DB

	sess1, err := NewSession(cfg, noDB, events.New(logger), logger)
	if err != nil {
		t.Fatalf("session #1: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := sess1.Add(ctx, AddRequest{File: miBuf.Bytes(), SavePath: downloadDir})
	if err != nil {
		t.Fatalf("session #1 add: %v", err)
	}
	hash := resp.InfoHash

	sess1.mu.RLock()
	mt1 := sess1.torrents[hash]
	sess1.mu.RUnlock()
	<-mt1.t.GotInfo()
	mt1.t.DownloadAll()

	// Wire the seeder as a direct peer so we don't need DHT/trackers.
	peerInfos := make([]lt.PeerInfo, 0, len(seederAddrs))
	for _, a := range seederAddrs {
		peerInfos = append(peerInfos, lt.PeerInfo{Addr: a, Source: lt.PeerSourceDirect, Trusted: true})
	}
	if added := mt1.t.AddPeers(peerInfos); added == 0 {
		sess1.Close()
		t.Fatal("AddPeers returned 0")
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if mt1.t.BytesCompleted() >= int64(fileSize) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if mt1.t.BytesCompleted() < int64(fileSize) {
		sess1.Close()
		t.Fatalf("session #1 download incomplete: got %d / %d", mt1.t.BytesCompleted(), fileSize)
	}

	// ── 4. Close session #1. The bolt completion store must flush to disk
	//    before Close returns, otherwise session #2 won't see the pieces.
	sess1.Close()

	// Sanity-check that the bolt file actually exists on disk where we
	// expect it. If this is missing, the wiring broke somewhere else.
	boltPath := filepath.Join(dataDir, ".torrent.bolt.db")
	if _, err := os.Stat(boltPath); err != nil {
		t.Fatalf("piece completion DB missing after session close (%s): %v — "+
			"the NewBoltPieceCompletion wiring in NewSession was skipped", boltPath, err)
	}

	// ── 5. Session #2: re-add the SAME torrent with the SAME data dir ──────
	sess2, err := NewSession(cfg, noDB, events.New(logger), logger)
	if err != nil {
		t.Fatalf("session #2: %v", err)
	}
	defer sess2.Close()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	resp2, err := sess2.Add(ctx2, AddRequest{File: miBuf.Bytes(), SavePath: downloadDir})
	if err != nil {
		t.Fatalf("session #2 add: %v", err)
	}
	if resp2.InfoHash != hash {
		t.Fatalf("hash mismatch: %q vs %q", resp2.InfoHash, hash)
	}

	sess2.mu.RLock()
	mt2 := sess2.torrents[hash]
	sess2.mu.RUnlock()
	<-mt2.t.GotInfo()

	// Give the completion-read pass a moment to propagate. anacrolix reads
	// the bolt store lazily via the storage layer; BytesCompleted should
	// reflect the persisted state within a few hundred ms.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mt2.t.BytesCompleted() >= int64(fileSize) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	got := mt2.t.BytesCompleted()
	if got < int64(fileSize) {
		t.Fatalf(
			"REGRESSION: torrent did NOT resume after Haul restart — got %d bytes, "+
				"want %d. This means anacrolix's piece-completion store isn't "+
				"persisting across sessions. Check NewSession in session.go — "+
				"somebody reverted NewFileWithCompletion/NewBoltPieceCompletion "+
				"back to naked storage.NewFile.",
			got, fileSize,
		)
	}
}
