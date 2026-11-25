package torrent

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

func runs(states ...string) []PieceStateRun {
	out := make([]PieceStateRun, len(states))
	for i, s := range states {
		out[i] = PieceStateRun{Length: 1, State: s}
	}
	return out
}

func TestPieceTracker_AnnouncesAPieceOnceWhenItFinishes(t *testing.T) {
	p := newPieceTracker()
	frac := func(int) float64 { return 0.5 }

	// Piece 0 was finished before watching began: not announced.
	if got := p.update(runs("complete", "missing", "partial"), frac, false); len(got.Done) != 0 {
		t.Fatalf("first update Done = %v, want none", got.Done)
	}
	if got, want := p.update(runs("complete", "complete", "complete"), frac, false).Done, []int{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second update Done = %v, want %v", got, want)
	}
	if got := p.update(runs("complete", "complete", "complete"), frac, false).Done; len(got) != 0 {
		t.Fatalf("third update Done = %v, want none", got)
	}
}

func TestPieceTracker_ProgressOnlyWhenChanged(t *testing.T) {
	p := newPieceTracker()
	levels := map[int]float64{0: 0.25, 2: 0.5}
	frac := func(i int) float64 { return levels[i] }
	state := runs("partial", "missing", "checking")

	want := []PieceProgress{{Index: 0, Fraction: 0.25}, {Index: 2, Fraction: 0.5}}
	if got := p.update(state, frac, true).Progress; !reflect.DeepEqual(got, want) {
		t.Fatalf("first Progress = %v, want %v", got, want)
	}
	if got := p.update(state, frac, true).Progress; len(got) != 0 {
		t.Fatalf("unchanged Progress = %v, want none", got)
	}
	levels[2] = 0.75
	if got := p.update(state, frac, false).Progress; len(got) != 0 {
		t.Fatalf("Progress without withProgress = %v, want none", got)
	}
	want = []PieceProgress{{Index: 2, Fraction: 0.75}}
	if got := p.update(state, frac, true).Progress; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed Progress = %v, want %v", got, want)
	}
}

// A piece that fails its hash check goes back to missing, and is
// announced again when it is downloaded a second time.
func TestPieceTracker_FailedPieceIsAnnouncedWhenRedownloaded(t *testing.T) {
	p := newPieceTracker()
	frac := func(int) float64 { return 0.9 }
	p.update(runs("missing"), frac, true)
	if got, want := p.update(runs("complete"), frac, true).Done, []int{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Done = %v, want %v", got, want)
	}
	if got := p.update(runs("missing"), frac, true); len(got.Done) != 0 || len(got.Progress) != 0 {
		t.Fatalf("after failure = %+v, want nothing", got)
	}
	if got, want := p.update(runs("complete"), frac, true).Done, []int{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after redownload Done = %v, want %v", got, want)
	}
}

// TestWatchPieces_ReportsEachPieceOfARealDownload downloads a small
// torrent from a throttled local seeder and checks the live stream names
// every piece exactly once and reports in-progress pieces filling up.
func TestWatchPieces_ReportsEachPieceOfARealDownload(t *testing.T) {
	s := newTestSession(t)

	// Pieces of four blocks each, so a piece can be seen part-way through.
	const pieceLength = 64 * 1024
	const numPieces = 6
	seederRoot := t.TempDir()
	payload := make([]byte, numPieces*pieceLength)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand: %v", err)
	}
	payloadPath := filepath.Join(seederRoot, "payload.bin")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	info := metainfo.Info{PieceLength: pieceLength, Name: "payload.bin"}
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
	// Slow enough that pieces are seen part-way through: about four seconds
	// for the whole torrent.
	seederCfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(96*1024), 16*1024)
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

	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("encode metainfo: %v", err)
	}
	addCtx, cancelAdd := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAdd()
	resp, err := s.Add(addCtx, AddRequest{File: buf.Bytes(), SavePath: t.TempDir()})
	if err != nil {
		t.Fatalf("Session.Add: %v", err)
	}
	s.mu.RLock()
	mt := s.torrents[resp.InfoHash]
	s.mu.RUnlock()
	<-mt.t.GotInfo()

	// Watch before the download starts, as the page does.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var mu sync.Mutex
	done := map[int]int{}
	var fractions []float64
	watchErr := make(chan error, 1)
	go func() {
		watchErr <- s.WatchPieces(ctx, resp.InfoHash, func(u PieceUpdate) error {
			mu.Lock()
			defer mu.Unlock()
			for _, i := range u.Done {
				done[i]++
			}
			for _, p := range u.Progress {
				fractions = append(fractions, p.Fraction)
			}
			if len(done) == numPieces {
				cancel()
			}
			return nil
		})
	}()

	// Give the watcher its first look before any piece arrives; pieces
	// finished before then are, rightly, not announced.
	time.Sleep(300 * time.Millisecond)

	mt.t.DownloadAll()
	var peers []lt.PeerInfo
	for _, a := range seeder.ListenAddrs() {
		peers = append(peers, lt.PeerInfo{Addr: a, Source: lt.PeerSourceDirect, Trusted: true})
	}
	if mt.t.AddPeers(peers) == 0 {
		t.Fatal("AddPeers returned 0")
	}

	if err := <-watchErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("WatchPieces ended with %v; pieces reported: %v; bytes completed: %d of %d; states: %+v",
			err, done, mt.t.BytesCompleted(), len(payload), mt.t.PieceStateRuns())
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < numPieces; i++ {
		if done[i] != 1 {
			t.Errorf("piece %d reported %d times, want once (all: %v)", i, done[i], done)
		}
	}
	partway := 0
	for _, f := range fractions {
		if f < 0 || f > 1 {
			t.Errorf("fraction %v out of range", f)
		}
		if f > 0 && f < 1 {
			partway++
		}
	}
	if partway == 0 {
		t.Errorf("no in-progress piece was reported part-way through; fractions: %v", fractions)
	}
}
