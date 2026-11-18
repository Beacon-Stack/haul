package torrent

// savepath_integration_test.go — regression suite for "the torrent's
// files aren't where Haul says they are".
//
// Haul used to add every torrent with the client's default storage,
// rooted at the startup download dir. The save path (explicit, per
// category, or <download dir>/<category>) was recorded and reported to
// Pilot/Prism as the content path, but the bytes always landed in the
// download dir root, so imports failed. TestSessionIntegration_DownloadFromPeer
// never caught it because it passes the download dir itself as the save
// path. These tests download from a real local seeder and check where the
// file actually is.

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"log/slog"
	"net"
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

const spFile = "payload.bin"

type spSeed struct {
	torrent []byte
	payload []byte
	addrs   []net.Addr
}

// newSpSeeder starts a bare anacrolix client seeding a random 128 KiB file.
func newSpSeeder(t *testing.T) spSeed {
	t.Helper()
	root := t.TempDir()
	payload := make([]byte, 128*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, spFile)
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 32 * 1024, Name: spFile}
	if err := info.BuildFromFilePath(p); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatal(err)
	}

	cfg := lt.NewDefaultClientConfig()
	cfg.DataDir = root
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.DisablePEX = true
	cfg.DisableIPv6 = true
	cfg.DefaultStorage = storage.NewFile(root)
	cfg.Seed = true
	seeder, err := lt.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { seeder.Close() })
	st, err := seeder.AddTorrent(&mi)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-st.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("seeder GotInfo timed out")
	}
	_ = st.VerifyDataContext(context.Background())
	return spSeed{torrent: buf.Bytes(), payload: payload, addrs: seeder.ListenAddrs()}
}

func newSpSession(t *testing.T, downloadDir string) *Session {
	t.Helper()
	orig := publicIPDetectTimeout
	publicIPDetectTimeout = 200 * time.Millisecond
	t.Cleanup(func() { publicIPDetectTimeout = orig })
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var noDB *sql.DB
	s, err := NewSession(config.TorrentConfig{
		DownloadDir: downloadDir,
		DataDir:     t.TempDir(),
	}, noDB, events.New(slog.Default()), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// download adds the seed's torrent with req, feeds it the seeder and waits
// for it to finish. Returns the torrent's hash.
func (seed spSeed) download(t *testing.T, s *Session, req AddRequest) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req.File = seed.torrent
	res, err := s.Add(ctx, req)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.mu.RLock()
	mt := s.torrents[res.InfoHash]
	s.mu.RUnlock()
	seed.feed(t, mt.t)
	return res.InfoHash
}

func (seed spSeed) feed(t *testing.T, tor *lt.Torrent) {
	t.Helper()
	select {
	case <-tor.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("GotInfo timed out")
	}
	tor.DownloadAll()
	peers := make([]lt.PeerInfo, 0, len(seed.addrs))
	for _, a := range seed.addrs {
		peers = append(peers, lt.PeerInfo{Addr: a, Source: lt.PeerSourceDirect, Trusted: true})
	}
	tor.AddPeers(peers)
	deadline := time.Now().Add(15 * time.Second)
	for tor.BytesCompleted() < int64(len(seed.payload)) {
		if time.Now().After(deadline) {
			t.Fatalf("download incomplete: %d/%d bytes", tor.BytesCompleted(), len(seed.payload))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// assertFileAt checks the payload is at dir/payload.bin and nowhere in
// the other dirs.
func (seed spSeed) assertFileAt(t *testing.T, dir string, notIn ...string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, spFile))
	if err != nil {
		t.Fatalf("file is not in its save path %s: %v", dir, err)
	}
	if !bytes.Equal(got, seed.payload) {
		t.Fatalf("file in %s has the wrong contents", dir)
	}
	for _, d := range notIn {
		if _, err := os.Stat(filepath.Join(d, spFile)); err == nil {
			t.Fatalf("file was also written to %s", d)
		}
	}
}

func TestSavePath_CategoryWithoutSavePathGetsSubfolder(t *testing.T) {
	seed := newSpSeeder(t)
	root := t.TempDir()
	s := newSpSession(t, root)

	hash := seed.download(t, s, AddRequest{Category: "tv"})

	want := filepath.Join(root, "tv")
	seed.assertFileAt(t, want, root)
	info, err := s.Get(hash)
	if err != nil {
		t.Fatal(err)
	}
	if info.SavePath != want || info.ContentPath != filepath.Join(want, spFile) {
		t.Fatalf("reported save_path=%q content_path=%q, want %q and %q",
			info.SavePath, info.ContentPath, want, filepath.Join(want, spFile))
	}
}

func TestSavePath_ExplicitSavePathElsewhere(t *testing.T) {
	seed := newSpSeeder(t)
	root := t.TempDir()
	nas := filepath.Join(t.TempDir(), "nas", "torrents")
	s := newSpSession(t, root)

	seed.download(t, s, AddRequest{SavePath: nas})

	seed.assertFileAt(t, nas, root)
}

func TestSavePath_RuntimeDownloadDirIsUsed(t *testing.T) {
	seed := newSpSeeder(t)
	root := t.TempDir()
	changed := filepath.Join(t.TempDir(), "changed")
	s := newSpSession(t, root)
	if err := s.SetDownloadDir(changed); err != nil {
		t.Fatal(err)
	}

	seed.download(t, s, AddRequest{Category: "movies"})

	seed.assertFileAt(t, filepath.Join(changed, "movies"), root, filepath.Join(root, "movies"))
}

func TestSavePath_SetLocationMovesAndKeepsSeeding(t *testing.T) {
	seed := newSpSeeder(t)
	root := t.TempDir()
	s := newSpSession(t, root)
	hash := seed.download(t, s, AddRequest{Category: "tv"})

	dest := filepath.Join(root, "moved")
	if err := s.SetLocation(hash, dest); err != nil {
		t.Fatalf("SetLocation: %v", err)
	}
	seed.assertFileAt(t, dest, filepath.Join(root, "tv"))

	// The reopened torrent reads its data from the new folder: after the
	// verify that reopening triggers, it is still complete.
	s.mu.RLock()
	mt := s.torrents[hash]
	s.mu.RUnlock()
	select {
	case <-mt.t.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("reopened torrent GotInfo timed out")
	}
	_ = mt.t.VerifyDataContext(context.Background())
	if got := mt.t.BytesCompleted(); got != int64(len(seed.payload)) {
		t.Fatalf("after moving, the torrent sees %d/%d bytes: it isn't reading the moved data", got, len(seed.payload))
	}
	info, _ := s.Get(hash)
	if info.SavePath != dest {
		t.Fatalf("reported save_path = %q, want %q", info.SavePath, dest)
	}
}

func TestRestoreDir_LegacyDataInRootIsKept(t *testing.T) {
	root := t.TempDir()
	s := &Session{cfg: config.TorrentConfig{DownloadDir: root}}
	info := metainfo.Info{Name: "Show.S01E01.mkv", PieceLength: 16384, Length: 1, Pieces: make([]byte, 20)}
	infoBytes, _ := bencode.Marshal(info)
	mi := &metainfo.MetaInfo{InfoBytes: infoBytes}
	tv := filepath.Join(root, "tv")

	// Data only in the root (written before per-torrent storage): use root.
	if err := os.WriteFile(filepath.Join(root, info.Name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.restoreDir(tv, mi); got != root {
		t.Fatalf("legacy torrent restored from %q, want the root %q", got, root)
	}

	// Data in the save path: use the save path.
	if err := os.MkdirAll(tv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tv, info.Name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.restoreDir(tv, mi); got != tv {
		t.Fatalf("torrent restored from %q, want its save path %q", got, tv)
	}

	// Nothing on disk anywhere yet (still downloading): use the save path.
	if err := os.Remove(filepath.Join(root, info.Name)); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "movies")
	if got := s.restoreDir(other, mi); got != other {
		t.Fatalf("new torrent restored from %q, want its save path %q", got, other)
	}
}
