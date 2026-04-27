// Package torrent owns the anacrolix/torrent client lifecycle and the
// Session type that exposes it to the rest of haul.
//
// ⚠  Before changing anything in this file, run:
//
//	go test ./internal/core/torrent/... -run TestSessionIntegration_DownloadFromPeer
//
// This test spins up a local seeder and verifies the Session can actually
// download through its configured peer-wire / DHT / IPBlocklist wiring.
// The "torrent stalls at 0 peers" bug has regressed three times — the
// test catches it in <1s. See haul/CLAUDE.md for the full list of files
// guarded by this regression suite.
package torrent

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	peer_store "github.com/anacrolix/dht/v2/peer-store"
	"github.com/anacrolix/publicip"
	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
	"github.com/beacon-stack/haul/internal/version"
)

// publicIPDetectTimeout bounds the blocking public-IP lookup in NewSession.
// Exposed as a package variable so tests can lower it (detection is only
// needed for DHT security extension and self-dial filtering; loopback tests
// don't need either).
//
// Cross-package tests can override via SetPublicIPDetectTimeoutForTesting.
var publicIPDetectTimeout = 10 * time.Second

// SetPublicIPDetectTimeoutForTesting lets tests in other packages (e.g.
// api/v1) short-circuit the 10-second public-IP lookup when constructing
// a real Session. Returns the previous value so the caller can restore it.
// Production code must NOT call this.
func SetPublicIPDetectTimeoutForTesting(d time.Duration) time.Duration {
	prev := publicIPDetectTimeout
	publicIPDetectTimeout = d
	return prev
}

// Status represents the current state of a torrent.
type Status string

const (
	StatusDownloading Status = "downloading"
	StatusSeeding     Status = "seeding"
	StatusPaused      Status = "paused"
	StatusChecking    Status = "checking"
	StatusQueued      Status = "queued"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
)

// Info is the external representation of a torrent.
type Info struct {
	InfoHash     string    `json:"info_hash"`
	Name         string    `json:"name"`
	Status       Status    `json:"status"`
	SavePath     string    `json:"save_path"`
	Category     string    `json:"category"`
	Tags         []string  `json:"tags"`
	Size         int64     `json:"size"`
	Downloaded   int64     `json:"downloaded"`
	Uploaded     int64     `json:"uploaded"`
	Progress     float64   `json:"progress"`
	DownloadRate int64     `json:"download_rate"`
	UploadRate   int64     `json:"upload_rate"`
	Seeds        int       `json:"seeds"`
	Peers        int       `json:"peers"`
	SeedRatio    float64   `json:"seed_ratio"`
	ETA          int64     `json:"eta"`
	AddedAt      time.Time `json:"added_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ContentPath  string    `json:"content_path"`
	Sequential   bool      `json:"sequential"`

	// Stalled is true when the backend's stall detector classifies this
	// torrent as inactive — see internal/core/torrent/stall.go for the
	// thresholds. The frontend uses this to render a distinct "Stalled"
	// status badge and color, replacing the old "download_rate == 0"
	// frontend-side heuristic which flipped on every brief connection blip.
	// Always false for non-downloading statuses.
	Stalled bool `json:"stalled"`
}

// PeerInfo is the external representation of a single connected peer.
// Built on demand by Session.Peers — not part of the bulk torrent list to
// keep the hot-path response small.
type PeerInfo struct {
	Addr         string  `json:"addr"`          // "1.2.3.4:54321"
	Client       string  `json:"client"`        // "qBittorrent 4.5.0", "unknown" if the peer hasn't sent a client name
	Network      string  `json:"network"`       // "tcp" or "utp"
	Encrypted    bool    `json:"encrypted"`     // the peer prefers / supports encryption
	Progress     float64 `json:"progress"`      // 0..1 — fraction of pieces the peer has
	DownloadRate int64   `json:"download_rate"` // bytes/sec we're receiving from them
	UploadRate   int64   `json:"upload_rate"`   // bytes/sec we're sending them (best-effort; 0 until anacrolix exposes per-peer upload rate)
	Downloaded   int64   `json:"downloaded"`    // total useful data bytes read from this peer
	Uploaded     int64   `json:"uploaded"`      // total data bytes written to this peer
}

// PieceStateRun is a run-length-encoded entry describing a series of
// consecutive pieces in the same state. Mirrors anacrolix's PieceStateRuns
// output but serialised as plain JSON.
type PieceStateRun struct {
	Length int    `json:"length"`
	State  string `json:"state"` // "complete" | "partial" | "checking" | "missing"
}

// PiecesInfo is a snapshot of a torrent's piece-level state. See
// plans/haul-torrent-detail-enhancements.md §4 for how the frontend consumes
// this (canvas-rendered piece bar with per-piece arrival flashes).
type PiecesInfo struct {
	NumPieces int             `json:"num_pieces"`
	PieceSize int64           `json:"piece_size"`
	Runs      []PieceStateRun `json:"runs"`
}

// TrackerInfo is a single configured tracker from the torrent's metainfo.
// v1 does NOT include live announce state (last announce time, reported
// peers/seeds, errors) — see plans/haul-torrent-detail-enhancements.md §6.1.
type TrackerInfo struct {
	Tier int    `json:"tier"`
	URL  string `json:"url"`
}

// SwarmInfo surfaces anacrolix's TorrentGauges so callers can tell the
// difference between "the swarm has 50 seeders but we only connected to 8"
// (peer-discovery / dial-success problem) and "the swarm only has 8
// seeders" (small swarm). The Info struct's `seeds`/`peers` fields are
// only the connected slice; without these gauges, every download looks
// like the swarm is small.
type SwarmInfo struct {
	// TotalPeers — every peer anacrolix has heard about across all sources
	// (tracker announces, DHT, PEX). Includes peers we never actually
	// dialed.
	TotalPeers int `json:"total_peers"`
	// PendingPeers — known but not yet dialed. If this stays large while
	// ActivePeers stays tiny, anacrolix is rate-limiting outbound dials.
	PendingPeers int `json:"pending_peers"`
	// HalfOpenPeers — TCP/uTP handshake in flight. If this is the cap
	// (TotalHalfOpenConns / 2 of MaxConnections), we're limited by the
	// dial concurrency setting.
	HalfOpenPeers int `json:"half_open_peers"`
	// ActivePeers — fully connected, exchanging messages. Equal to
	// len(PeerConns()) for v1.61.
	ActivePeers int `json:"active_peers"`
	// ConnectedSeeders — subset of ActivePeers with the full file.
	ConnectedSeeders int `json:"connected_seeders"`
}

// AddRequest is the input for adding a new torrent.
type AddRequest struct {
	// URI is a magnet link, HTTP URL to a .torrent file, or empty if File is set.
	URI string `json:"uri"`
	// File is raw .torrent file bytes. Mutually exclusive with URI.
	File []byte `json:"-"`
	// Category to assign.
	Category string `json:"category"`
	// SavePath overrides the default download directory.
	SavePath string `json:"save_path"`
	// Tags to assign.
	Tags []string `json:"tags"`
	// Paused starts the torrent in paused state.
	Paused bool `json:"paused"`
	// Sequential enables sequential download mode.
	Sequential bool `json:"sequential"`
	// Metadata holds optional media context from the requesting service (Pilot/Prism).
	Metadata *RequesterMetadata `json:"metadata,omitempty"`
}

// Session manages the torrent engine and wraps anacrolix/torrent.
type Session struct {
	client *lt.Client
	db     *sql.DB
	bus    *events.Bus
	logger *slog.Logger
	cfg    config.TorrentConfig

	// pieceCompletion is the persistent BoltDB-backed completion tracker.
	// Without this, torrents restart from 0% on every container restart
	// because anacrolix's default in-memory map doesn't survive restarts
	// even though the downloaded bytes are still on disk.
	// Closed by Session.Close().
	pieceCompletion storage.PieceCompletion

	// startedAt is when NewSession returned successfully. Used as a grace
	// period for stall detection — we don't want to flag torrents as dead
	// during the first few minutes while anacrolix is bootstrapping DHT,
	// discovering its public IP, and warming up trackers.
	startedAt time.Time

	// runtimeMu guards the runtime-mutable settings below. These override
	// cfg.* at call time and are updated by the settings HTTP handler when
	// the user flips a toggle in the UI. Without this layer, UI toggles
	// are phantom writes that only touch the DB and never affect behavior.
	runtimeMu       sync.RWMutex
	pauseOnComplete bool

	mu             sync.RWMutex
	torrents       map[string]*managedTorrent
	altSpeedActive bool
}

// managedTorrent pairs the library torrent handle with our metadata.
type managedTorrent struct {
	t              *lt.Torrent
	paused         bool
	category       string
	tags           []string
	addedAt        time.Time
	savePath       string
	lastBytesRead  int64      // for stall detection
	lastActivityAt time.Time  // last time data was received (bytesRead increased)
	firstPeerAt    *time.Time // first time we observed ActivePeers > 0 (nil = never)
	ready          bool       // true once GotInfo() has fired

	// Rate trackers convert anacrolix's cumulative byte counters into
	// smoothed bytes-per-second values by sampling on every getInfo call.
	// See rateTracker for the math. Value types (not pointers) so the
	// zero value is a valid empty tracker — no init needed on Add.
	downRate rateTracker
	upRate   rateTracker
}

// rateTracker converts a monotonically-increasing byte counter into a
// smoothed bytes-per-second rate using an exponential moving average. It
// is sampled on every getInfo call; no background ticker is required.
//
// The EMA uses a 5-second time constant so that α depends on the wall-clock
// gap between samples: short gaps barely nudge the displayed value (matches
// qBittorrent's "doesn't flicker" UX), large gaps overwrite most of the
// stored rate, and gaps over 30s reset the tracker entirely rather than
// extrapolating from stale data.
//
// This type exists because anacrolix/torrent's Stats() only exposes
// cumulative counters — treating those as a rate (as the previous
// implementation did) produces nonsense ETAs. See haul/plans or git blame
// on getInfo for the original bug.
type rateTracker struct {
	mu        sync.Mutex
	lastBytes int64
	lastAt    time.Time
	ema       float64 // bytes per second
}

const (
	rateTimeConstantSecs = 5.0  // τ — responsiveness vs smoothing trade-off
	rateStaleGapSecs     = 30.0 // sampling gap beyond which we reset the tracker
)

// sample records a new cumulative byte count at the given wall-clock time
// and returns the current smoothed rate in bytes/sec.
//
// The first call after construction or a reset returns 0 (we need at least
// two samples to measure an interval). Subsequent calls blend the new
// instant rate into the EMA using α = 1 - exp(-Δt/τ).
func (r *rateTracker) sample(bytes int64, now time.Time) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// First sample or long gap — seed state and report zero. Long gaps
	// reset so we don't carry over a stale rate from hours ago.
	if r.lastAt.IsZero() || now.Sub(r.lastAt).Seconds() > rateStaleGapSecs {
		r.lastBytes = bytes
		r.lastAt = now
		r.ema = 0
		return 0
	}

	delta := now.Sub(r.lastAt).Seconds()
	if delta <= 0 {
		// Same millisecond or clock went backwards — keep existing EMA,
		// don't divide by zero.
		return int64(r.ema)
	}

	// Byte counter should only grow. Guard against resets or going
	// backwards (e.g. torrent removed and re-added) by treating negative
	// deltas as a reset.
	if bytes < r.lastBytes {
		r.lastBytes = bytes
		r.lastAt = now
		r.ema = 0
		return 0
	}

	instant := float64(bytes-r.lastBytes) / delta
	alpha := 1 - math.Exp(-delta/rateTimeConstantSecs)
	r.ema = alpha*instant + (1-alpha)*r.ema
	r.lastBytes = bytes
	r.lastAt = now
	return int64(r.ema)
}

// NewSession creates a new torrent session.
func NewSession(cfg config.TorrentConfig, db *sql.DB, bus *events.Bus, logger *slog.Logger) (*Session, error) {
	ltCfg := lt.NewDefaultClientConfig()
	ltCfg.ListenPort = cfg.ListenPort
	ltCfg.Seed = true
	ltCfg.NoUpload = false
	ltCfg.DisableIPv6 = true // IPv6 is disabled at sysctl level in VPN containers
	ltCfg.NoDHT = !cfg.EnableDHT
	ltCfg.DisablePEX = !cfg.EnablePEX
	ltCfg.DisableUTP = !cfg.EnableUTP

	// Persistent piece-completion store — this is what makes torrents
	// actually resume after a container restart instead of re-downloading
	// from scratch. anacrolix's default file storage uses an in-memory
	// completion map that dies with the process. BoltDB writes to
	// `<DataDir>/.torrent.bolt.db` so the map survives restarts.
	//
	// If bolt init fails (disk full, permission denied, etc) fall back to
	// the in-memory map — logs a loud warning so operators know resume is
	// broken, but the session still starts up and new torrents work.
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "/config"
	}
	var pieceCompletion storage.PieceCompletion
	if pc, err := storage.NewBoltPieceCompletion(dataDir); err == nil {
		pieceCompletion = pc
		logger.Info("piece completion store opened", "path", dataDir+"/.torrent.bolt.db")
	} else {
		logger.Error("piece completion store init FAILED — torrents will restart from 0% on container restart",
			"path", dataDir, "error", err)
		// Explicitly use the in-memory map so storage still works for
		// current-session downloads. Persistence is broken but the
		// process still functions.
		pieceCompletion = storage.NewMapPieceCompletion()
	}
	ltCfg.DefaultStorage = storage.NewFileWithCompletion(cfg.DownloadDir, pieceCompletion)
	ltCfg.HTTPUserAgent = version.AppName + "/" + version.Version
	ltCfg.NoDefaultPortForwarding = true // UPnP doesn't work through VPN tunnels

	// Wire anacrolix's logger to ours. Without this, anacrolix's rich
	// announce diagnostics ("announced", "announce err", "peers added by
	// source X", per-tracker NumPeers) silently disappear into anacrolix's
	// default discard logger. With it, we get visibility into "tracker
	// returned 85 peers but we only ingested 3" mismatches at the source.
	// Using a child logger so anacrolix lines are tagged "anacrolix=true".
	ltCfg.Slogger = logger.With("subsystem", "anacrolix")

	// Detect our public IP for DHT and self-connection filtering.
	// Behind a VPN, the container's local IP differs from the tunnel's external IP.
	// We need the external IP for two things:
	// 1. DHT security extension — node ID must match public IP for peers to trust us
	// 2. Self-dial prevention — block our own IP so we don't waste connections on ourselves
	ctx, cancel := context.WithTimeout(context.Background(), publicIPDetectTimeout)
	extIP, ipErr := publicip.Get4(ctx)
	cancel()
	if ipErr != nil {
		logger.Warn("could not detect public IPv4, DHT may be less effective", "error", ipErr)
	} else {
		logger.Info("detected public IPv4", "ip", extIP)
		ltCfg.PublicIp4 = extIP

		// Block our own external IP to prevent self-dialing.
		// Behind a VPN, trackers and DHT return our own IP as a peer. Without this,
		// anacrolix wastes all outgoing connections dialing itself. libtorrent
		// filters self-connections automatically; anacrolix requires IPBlocklist.
		ltCfg.IPBlocklist = iplist.New([]iplist.Range{{
			First:       extIP,
			Last:        extIP,
			Description: "self (VPN external IP)",
		}})
	}

	// Configure the DHT server with an explicit node ID and in-memory peer store.
	// This matches what anacrolix/confluence (the library author's own client) does.
	// Without this, the default DHT server may not properly store discovered peers,
	// which causes "finds peers via DHT but never dials them" behind VPNs.
	ltCfg.ConfigureAnacrolixDhtServer = func(sc *dht.ServerConfig) {
		if extIP != nil {
			sc.PublicIP = extIP
		}
		sc.InitNodeId()
		sc.PeerStore = &peer_store.InMemory{}
	}

	// Connection limits.
	if cfg.MaxConnections > 0 {
		ltCfg.EstablishedConnsPerTorrent = cfg.MaxConnectionsPerTorrent
		ltCfg.TotalHalfOpenConns = cfg.MaxConnections / 2
	}

	if cfg.GlobalDownloadLimit > 0 {
		ltCfg.DownloadRateLimiter = newRateLimiter(cfg.GlobalDownloadLimit)
	}
	if cfg.GlobalUploadLimit > 0 {
		ltCfg.UploadRateLimiter = newRateLimiter(cfg.GlobalUploadLimit)
	}

	client, err := lt.NewClient(ltCfg)
	if err != nil && cfg.ListenPort != 0 {
		// Port likely in use — retry with random port.
		logger.Warn("listen port in use, falling back to random port", "port", cfg.ListenPort, "error", err)
		ltCfg.ListenPort = 0
		client, err = lt.NewClient(ltCfg)
	}
	if err != nil {
		return nil, fmt.Errorf("creating torrent client: %w", err)
	}

	s := &Session{
		client:          client,
		db:              db,
		bus:             bus,
		logger:          logger,
		cfg:             cfg,
		torrents:        make(map[string]*managedTorrent),
		startedAt:       time.Now(),
		pauseOnComplete: cfg.PauseOnComplete,
		pieceCompletion: pieceCompletion,
	}

	// Runtime settings overlay: the `settings` DB table is the persistent
	// source of truth for UI-toggled runtime values. If the user has
	// previously toggled "pause on complete" via the settings page, that
	// value overrides whatever came from cfg (which is only populated from
	// startup YAML + env vars). Without this, the UI toggle would silently
	// reset on every Haul restart.
	//
	// The DB read is best-effort: if the table is missing, the row isn't
	// set, or the DB is nil (tests), we fall back to cfg.
	if db != nil {
		var v string
		err := db.QueryRow(`SELECT value FROM settings WHERE key = 'pause_on_complete'`).Scan(&v)
		if err == nil {
			s.pauseOnComplete = v == "true" || v == "1"
			logger.Info("pause_on_complete loaded from settings table", "value", s.pauseOnComplete)
		}
	}

	// Restore torrents from the previous session. Torrents with saved .torrent
	// data are re-added via AddTorrent (fast-resume, no metadata fetch needed).
	// Legacy rows without .torrent data are cleaned up.
	if err := s.restoreFromDB(); err != nil {
		logger.Warn("torrent restore failed", "error", err)
	}

	return s, nil
}

// Add adds a new torrent from a magnet link, URL, or .torrent file bytes.
func (s *Session) Add(ctx context.Context, req AddRequest) (result *Info, resultErr error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in Add", "panic", r)
			result = nil
			resultErr = fmt.Errorf("internal error adding torrent: %v", r)
		}
	}()
	savePath := req.SavePath
	if savePath == "" {
		savePath = s.cfg.DownloadDir
	}

	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return nil, fmt.Errorf("creating save path: %w", err)
	}

	var t *lt.Torrent
	var err error

	if len(req.File) > 0 {
		mi, parseErr := metainfo.Load(bytes_reader(req.File))
		if parseErr != nil {
			return nil, fmt.Errorf("parsing torrent file: %w", parseErr)
		}
		t, err = s.client.AddTorrent(mi)
	} else if req.URI != "" {
		if strings.HasPrefix(req.URI, "magnet:") {
			t, err = s.client.AddMagnet(req.URI)
			if err == nil && t != nil {
				// Bare magnets from public indexers often have no tracker URLs.
				// Without trackers, metadata resolution relies entirely on DHT,
				// which is slow/unreliable behind VPNs. Inject public trackers
				// to speed up peer discovery — same pattern as Gopeed/exatorrent.
				t.AddTrackers(DefaultPublicTrackers)
			}
		} else if strings.HasPrefix(req.URI, "data:application/x-bittorrent;base64,") {
			// Base64-encoded .torrent file from Prism/Pilot plugin.
			b64 := req.URI[len("data:application/x-bittorrent;base64,"):]
			torrentBytes, decErr := base64Decode(b64)
			if decErr != nil {
				return nil, fmt.Errorf("decoding base64 torrent: %w", decErr)
			}
			mi, parseErr := metainfo.Load(bytes_reader(torrentBytes))
			if parseErr != nil {
				return nil, fmt.Errorf("parsing torrent from base64 data: %w", parseErr)
			}
			t, err = s.client.AddTorrent(mi)
		} else if strings.HasPrefix(req.URI, "http://") || strings.HasPrefix(req.URI, "https://") {
			// HTTP/HTTPS URL — download the .torrent file first.
			t, err = s.addFromURL(ctx, req.URI)
		} else {
			return nil, fmt.Errorf("unsupported URI scheme: %s", req.URI)
		}
	} else {
		return nil, fmt.Errorf("either uri or file must be provided")
	}

	if err != nil {
		return nil, fmt.Errorf("adding torrent: %w", err)
	}
	if t == nil {
		return nil, fmt.Errorf("torrent handle is nil (duplicate or invalid)")
	}

	hash := t.InfoHash().HexString()

	mt := &managedTorrent{
		t:        t,
		paused:   req.Paused,
		category: req.Category,
		tags:     req.Tags,
		addedAt:  time.Now().UTC(),
		savePath: savePath,
	}

	if req.Sequential {
		t.SetDisplayName(t.Name())
	}

	s.mu.Lock()
	s.torrents[hash] = mt
	s.mu.Unlock()

	// Persist to database.
	s.persistTorrent(hash, mt)

	// Store requester metadata if provided.
	if req.Metadata != nil {
		_ = s.SetMetadata(hash, *req.Metadata)
	}

	s.bus.Publish(ctx, events.Event{
		Type:     events.TypeTorrentAdded,
		InfoHash: hash,
		Data:     map[string]any{"name": t.Name()},
	})

	// Wait for metadata in background, then start.
	go s.waitAndStart(mt, hash, req.Paused, req.Sequential, false /* verifyOnStart — fresh add, nothing to verify */)

	return s.torrentInfo(hash, mt), nil
}

// Get returns info about a specific torrent.
func (s *Session) Get(hash string) (*Info, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}
	return s.torrentInfo(hash, mt), nil
}

// Peers returns a snapshot of currently-connected peers for a torrent.
// Empty slice if the torrent has no peers or metadata isn't ready.
// Error only for unknown hash.
func (s *Session) Peers(hash string) ([]PeerInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}
	if !mt.ready {
		return []PeerInfo{}, nil
	}

	numPieces := mt.t.NumPieces()
	conns := mt.t.PeerConns()
	result := make([]PeerInfo, 0, len(conns))
	for _, pc := range conns {
		stats := pc.Stats()

		// Peer's progress — what fraction of pieces they have. anacrolix
		// exposes the peer's bitmap via PeerPieces(); we read the
		// cardinality once per poll, which is cheap.
		progress := 0.0
		if numPieces > 0 {
			progress = float64(pc.PeerPieces().GetCardinality()) / float64(numPieces)
			if progress > 1 {
				progress = 1
			}
		}

		// Client name is stored as an atomic.Value; Load() returns nil
		// before the extension handshake completes.
		client := "unknown"
		if v := pc.PeerClientName.Load(); v != nil {
			if s, ok := v.(string); ok && s != "" {
				client = s
			}
		}

		result = append(result, PeerInfo{
			Addr:         pc.RemoteAddr.String(),
			Client:       client,
			Network:      pc.Network,
			Encrypted:    pc.PeerPrefersEncryption,
			Progress:     progress,
			DownloadRate: int64(stats.DownloadRate),
			// Per-peer upload rate isn't directly exposed by anacrolix; the
			// LastWriteUploadRate field is internal. Best-effort: leave 0
			// for now — the totals below still convey activity direction.
			UploadRate: 0,
			Downloaded: stats.BytesReadUsefulData.Int64(),
			Uploaded:   stats.BytesWrittenData.Int64(),
		})
	}
	return result, nil
}

// Pieces returns a run-length-encoded snapshot of the torrent's piece state.
// Returns (nil, nil) if metadata hasn't been received yet — the frontend
// renders "Waiting for metadata…" in that case.
func (s *Session) Pieces(hash string) (*PiecesInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}
	if !mt.ready {
		return nil, nil
	}
	info := mt.t.Info()
	if info == nil {
		return nil, nil
	}

	numPieces := mt.t.NumPieces()
	runs := mt.t.PieceStateRuns()
	out := make([]PieceStateRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, PieceStateRun{
			Length: r.Length,
			State:  classifyPieceState(r.PieceState),
		})
	}

	return &PiecesInfo{
		NumPieces: numPieces,
		PieceSize: info.PieceLength,
		Runs:      out,
	}, nil
}

// Trackers returns the configured tracker list from the torrent's metainfo,
// flattened across tiers with the tier index preserved on each entry.
// Does NOT include live announce state — see plan §6.1.
func (s *Session) Trackers(hash string) ([]TrackerInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}

	mi := mt.t.Metainfo()
	result := make([]TrackerInfo, 0)
	for tierIdx, tier := range mi.AnnounceList {
		for _, url := range tier {
			if url == "" {
				continue
			}
			result = append(result, TrackerInfo{Tier: tierIdx, URL: url})
		}
	}
	// If the metainfo has only the legacy single-announce field and no
	// announce-list, surface it as tier 0.
	if len(result) == 0 && mi.Announce != "" {
		result = append(result, TrackerInfo{Tier: 0, URL: mi.Announce})
	}
	return result, nil
}

// Swarm returns anacrolix's swarm-level gauges: how many peers it knows
// about vs how many it has fully connected. Used to diagnose
// "swarm reports N seeders but we only connected to a few" cases — the
// gap usually lives between PendingPeers (discovered, not dialed) and
// HalfOpenPeers (dial in flight). Returns ("torrent not found") for an
// unknown hash.
func (s *Session) Swarm(hash string) (*SwarmInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}
	g := mt.t.Stats().TorrentGauges
	return &SwarmInfo{
		TotalPeers:       g.TotalPeers,
		PendingPeers:     g.PendingPeers,
		HalfOpenPeers:    g.HalfOpenPeers,
		ActivePeers:      g.ActivePeers,
		ConnectedSeeders: g.ConnectedSeeders,
	}, nil
}

// classifyPieceState flattens anacrolix's PieceState (which has overlapping
// flags) to a single string per run. Priority-ordered: complete beats
// checking beats partial beats missing. See plan §2.4.
func classifyPieceState(ps lt.PieceState) string {
	if ps.Completion.Ok && ps.Completion.Complete {
		return "complete"
	}
	if ps.Checking || ps.Hashing || ps.QueuedForHash || ps.Marking {
		return "checking"
	}
	if ps.Partial {
		return "partial"
	}
	return "missing"
}

// List returns info about all torrents.
func (s *Session) List() []Info {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Query priority order from DB.
	type hashPrio struct {
		hash string
		prio int
	}
	var ordered []hashPrio
	if s.db != nil {
		rows, err := s.db.Query(`SELECT info_hash, priority FROM torrents ORDER BY priority ASC`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var hp hashPrio
				if rows.Scan(&hp.hash, &hp.prio) == nil {
					ordered = append(ordered, hp)
				}
			}
		}
	}

	// Build result in priority order, then append any not in DB.
	seen := make(map[string]bool)
	result := make([]Info, 0, len(s.torrents))
	for _, hp := range ordered {
		if mt, ok := s.torrents[hp.hash]; ok {
			result = append(result, *s.torrentInfo(hp.hash, mt))
			seen[hp.hash] = true
		}
	}
	for hash, mt := range s.torrents {
		if !seen[hash] {
			result = append(result, *s.torrentInfo(hash, mt))
		}
	}
	return result
}

// Remove removes a torrent. If deleteFiles is true, downloaded data is deleted.
//
// DB cleanup runs in a defer so it survives a panic from anacrolix/torrent's
// Drop() — without that, a library-internal crash mid-Drop would leave an
// orphan torrents row that restoreFromDB would resurrect on next startup,
// re-triggering the same panic. Real bug observed in the field with John
// Wick (info hash ec5086c1c…): library panic during tracker announce
// dispatcher → DB DELETE never ran → permanent crashloop on restart.
func (s *Session) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	delete(s.torrents, hash)
	s.mu.Unlock()

	// DB cleanup must run regardless of what Drop() does. Defer first.
	defer s.deleteTorrent(hash)

	mt.t.Drop()

	if deleteFiles {
		// Best-effort deletion of downloaded content.
		contentPath := mt.savePath
		if info := mt.t.Info(); info != nil {
			contentPath = fmt.Sprintf("%s/%s", mt.savePath, info.BestName())
		}
		_ = os.RemoveAll(contentPath)
	}

	s.bus.Publish(ctx, events.Event{
		Type:     events.TypeTorrentRemoved,
		InfoHash: hash,
	})

	return nil
}

// Pause pauses a torrent.
func (s *Session) Pause(hash string) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	mt.paused = true
	s.mu.Unlock()

	// anacrolix doesn't have a native pause — we cancel all pieces.
	mt.t.CancelPieces(0, mt.t.NumPieces())
	return nil
}

// Resume resumes a paused torrent.
func (s *Session) Resume(hash string) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	mt.paused = false
	s.mu.Unlock()

	mt.t.DownloadAll()
	return nil
}

// Close shuts down the torrent engine. anacrolix's fileClientImpl.Close()
// already closes the piece-completion store (see anacrolix storage/
// file-client.go Close), so we do NOT close s.pieceCompletion separately
// here — that would be a double-close and the second one errors with
// "database not open" on bolt. The pieceCompletion field on Session exists
// for the NewSession fallback path where we need to pass the store into
// NewFileWithCompletion; once it's handed off to anacrolix, anacrolix owns
// its lifecycle.
func (s *Session) Close() {
	s.client.Close()
}

// PauseOnComplete returns the runtime-effective value of the pause-on-
// complete setting. Initialized from cfg.PauseOnComplete at Session
// creation, optionally overridden by the `settings` DB table at startup,
// and mutated at runtime by SetPauseOnComplete (which the settings HTTP
// handler calls on UI toggle).
func (s *Session) PauseOnComplete() bool {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.pauseOnComplete
}

// SetPauseOnComplete updates the runtime pause-on-complete setting. Called
// by the settings HTTP handler when the user flips the "Stop seeding when
// complete" toggle in the UI. This is the only method that affects
// runtime behavior — writing to the `settings` DB table alone does NOT
// take effect until this is called.
func (s *Session) SetPauseOnComplete(v bool) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.pauseOnComplete != v {
		s.logger.Info("pause_on_complete changed via settings API",
			"from", s.pauseOnComplete, "to", v)
	}
	s.pauseOnComplete = v
}

// addFromURL downloads a .torrent file from an HTTP/HTTPS URL and adds it.
func (s *Session) addFromURL(ctx context.Context, torrentURL string) (*lt.Torrent, error) {
	// Use a client that doesn't follow redirects — we need to intercept
	// magnet: URI redirects from torznab proxies.
	noRedirectClient := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", torrentURL, err)
	}
	req.Header.Set("User-Agent", version.AppName+"/"+version.Version)

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching torrent from %s: %w", torrentURL, err)
	}
	defer resp.Body.Close()

	// Check for redirect to magnet link.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if strings.HasPrefix(location, "magnet:") {
			s.logger.Info("torrent URL redirected to magnet", "url", torrentURL)
			return s.client.AddMagnet(location)
		}
		// Follow non-magnet redirects manually.
		return s.addFromURL(ctx, location)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching torrent from %s: HTTP %d", torrentURL, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MiB limit
	if err != nil {
		return nil, fmt.Errorf("reading torrent from %s: %w", torrentURL, err)
	}

	// Check if the response body is actually a magnet link (some sites return
	// magnet URIs as the response body instead of redirecting).
	if len(data) < 2048 && strings.HasPrefix(strings.TrimSpace(string(data)), "magnet:") {
		return s.client.AddMagnet(strings.TrimSpace(string(data)))
	}

	mi, err := metainfo.Load(bytes_reader(data))
	if err != nil {
		return nil, fmt.Errorf("parsing torrent from %s: %w", torrentURL, err)
	}

	return s.client.AddTorrent(mi)
}

// torrentInfo builds an Info struct from internal state.
// Returns a safe minimal Info if the torrent hasn't received metadata yet.
func (s *Session) torrentInfo(hash string, mt *managedTorrent) (result *Info) {
	// Guard against panics from anacrolix internals when metadata is missing.
	defer func() {
		if r := recover(); r != nil {
			s.logger.Warn("torrentInfo panic recovered", "hash", hash, "panic", r)
			result = &Info{
				InfoHash: hash,
				Name:     hash,
				Status:   StatusDownloading,
				SavePath: mt.savePath,
				Category: mt.category,
				Tags:     mt.tags,
				AddedAt:  mt.addedAt,
			}
		}
	}()

	t := mt.t

	// If metadata hasn't arrived yet, return minimal info.
	if !mt.ready {
		return &Info{
			InfoHash: hash,
			Name:     t.Name(),
			Status:   StatusDownloading,
			SavePath: mt.savePath,
			Category: mt.category,
			Tags:     mt.tags,
			AddedAt:  mt.addedAt,
		}
	}

	hasInfo := t.Info() != nil
	stats := t.Stats()

	var size int64
	if hasInfo {
		size = t.Info().TotalLength()
	}

	var downloaded int64
	if hasInfo {
		downloaded = t.BytesCompleted()
	}

	status := StatusDownloading
	if hasInfo && t.BytesMissing() == 0 && size > 0 {
		// Completed downloads report as completed even when paused,
		// so the download client plugin returns the right status to Prism/Pilot.
		if mt.paused {
			status = StatusCompleted
		} else {
			status = StatusSeeding
		}
	} else if mt.paused {
		status = StatusPaused
	}

	var progress float64
	if size > 0 {
		progress = float64(downloaded) / float64(size)
	}

	// Smooth the cumulative byte counters into bytes-per-second via the
	// per-torrent EMA tracker. See rateTracker for the math. Without this,
	// ETA is computed as remaining/cumulative-bytes, which is nonsense.
	nowForRate := time.Now()
	downloadRate := mt.downRate.sample(stats.ConnStats.BytesReadData.Int64(), nowForRate)
	uploadRate := mt.upRate.sample(stats.ConnStats.BytesWrittenData.Int64(), nowForRate)

	var seedRatio float64
	if downloaded > 0 {
		seedRatio = float64(stats.ConnStats.BytesWrittenData.Int64()) / float64(downloaded)
	}

	var eta int64
	remaining := size - downloaded
	if downloadRate > 0 && remaining > 0 {
		eta = remaining / downloadRate
	}

	contentPath := mt.savePath
	if info := t.Info(); info != nil {
		contentPath = fmt.Sprintf("%s/%s", mt.savePath, info.BestName())
	}

	// Count peers from PeerConns() instead of stats.ActivePeers / stats.
	// ConnectedSeeders so the torrent list row, the detail page facts grid,
	// AND the Peers widget all show the same number. The gauges use a
	// stricter "active" definition (connected AND exchanging data) that
	// excludes peers mid-handshake or in the choked/idle state, which
	// caused a user-visible mismatch: the widget would show 28 rows while
	// the facts grid said "Peers: 22". len(PeerConns()) is the set the
	// widget renders, so that's the canonical count.
	conns := t.PeerConns()
	peerCount := len(conns)
	seedCount := 0
	if hasInfo {
		np := int64(t.NumPieces())
		for _, pc := range conns {
			if pc.PeerPieces().GetCardinality() == uint64(np) {
				seedCount++
			}
		}
	}

	// Compute "stalled" via the pure helper so it's unit-testable. This is
	// the same logic CheckStalls / ListStalled use, but surfaced on every
	// torrent list response so the frontend doesn't have to re-derive it.
	stallTimeoutSecs := s.cfg.StallTimeout
	if stallTimeoutSecs <= 0 {
		stallTimeoutSecs = 120
	}
	stalled := classifyStalled(stallParams{
		now:              time.Now(),
		status:           status,
		hasInfo:          hasInfo,
		bytesMissing:     t.BytesMissing(),
		sessionStartedAt: s.startedAt,
		addedAt:          mt.addedAt,
		firstPeerAt:      mt.firstPeerAt,
		lastActivityAt:   mt.lastActivityAt,
		stallTimeout:     time.Duration(stallTimeoutSecs) * time.Second,
	})

	return &Info{
		InfoHash:     hash,
		Name:         t.Name(),
		Status:       status,
		SavePath:     mt.savePath,
		Category:     mt.category,
		Tags:         mt.tags,
		Size:         size,
		Downloaded:   downloaded,
		Uploaded:     stats.ConnStats.BytesWrittenData.Int64(),
		Progress:     progress,
		DownloadRate: downloadRate,
		UploadRate:   uploadRate,
		Seeds:        seedCount,
		Peers:        peerCount,
		SeedRatio:    seedRatio,
		ETA:          eta,
		AddedAt:      mt.addedAt,
		ContentPath:  contentPath,
		Stalled:      stalled,
	}
}

// stallParams is the pure-function input to classifyStalled. All fields
// are primitives so the logic can be unit-tested without a real anacrolix
// Torrent, managedTorrent, or Session.
type stallParams struct {
	now              time.Time
	status           Status
	hasInfo          bool
	bytesMissing     int64
	sessionStartedAt time.Time
	addedAt          time.Time
	firstPeerAt      *time.Time
	lastActivityAt   time.Time
	stallTimeout     time.Duration
}

// classifyStalled returns true when a torrent should be reported as stalled
// on the Info API. This is the canonical definition — CheckStalls and
// ListStalled in stall.go use overlapping logic; keep them in sync if you
// change anything here.
//
// Rules (in order):
//  1. Non-downloading torrents are never stalled (seeding, paused, completed,
//     etc. — the stalled state only makes sense while trying to make progress).
//  2. Pre-metadata torrents are never stalled (hasInfo == false). CheckStalls
//     has a separate "no peers ever" path for pre-metadata, but for the Info
//     API we just report them as downloading until metadata arrives.
//  3. Fully-downloaded torrents (bytesMissing == 0) are never stalled.
//  4. **Session-startup grace**: during the first sessionStartupGrace window
//     after the Session was created, never report stalled. This is the fix
//     for the "everything is red after a container restart" false positive —
//     on restart, firstPeerAt is nil and lastActivityAt is zero (in-memory
//     state is fresh), while addedAt is the ORIGINAL add time from the DB
//     (possibly hours ago). Without the grace, every restored torrent looks
//     stalled for a minute or two until peers reconnect. CheckStalls and
//     ListStalled both honor this guard; so must we.
//  5. "No peers ever" path: if we've never observed a peer AND the torrent
//     has been around longer than firstPeerTimeout, call it stalled.
//  6. "No recent activity" path: if lastActivityAt is older than
//     stallTimeout, call it stalled. Use addedAt (or firstPeerAt if later)
//     as the baseline when lastActivityAt is zero — matches GetStallInfo.
func classifyStalled(p stallParams) bool {
	// Rules 1–3: non-downloading / pre-metadata / complete.
	if p.status != StatusDownloading || !p.hasInfo || p.bytesMissing <= 0 {
		return false
	}

	// Rule 4: session-startup grace.
	if p.now.Sub(p.sessionStartedAt) < sessionStartupGrace {
		return false
	}

	// Rule 5: no peers ever observed, past the first-peer window.
	if p.firstPeerAt == nil && p.now.Sub(p.addedAt) > firstPeerTimeout {
		return true
	}

	// Rule 6: no recent data activity.
	lastActivity := p.lastActivityAt
	if lastActivity.IsZero() {
		lastActivity = p.addedAt
		if p.firstPeerAt != nil && p.firstPeerAt.After(lastActivity) {
			lastActivity = *p.firstPeerAt
		}
	}
	return p.now.Sub(lastActivity) >= p.stallTimeout
}

// waitAndStart waits for metadata then begins downloading.
func (s *Session) waitAndStart(mt *managedTorrent, hash string, paused, sequential, verifyOnStart bool) {
	<-mt.t.GotInfo()

	// Mark as ready — safe to call Stats(), BytesMissing(), etc.
	s.mu.Lock()
	mt.ready = true
	s.mu.Unlock()

	s.logger.Info("torrent metadata received",
		"hash", hash,
		"name", mt.t.Name(),
		"size", mt.t.Info().TotalLength(),
	)

	// Re-hash the file on disk against the piece hashes in the metainfo.
	// This is how we rebuild completion state after a restart when bolt
	// might be stale, missing, or corrupted. It runs asynchronously in
	// anacrolix — VerifyData returns immediately and the hash pass happens
	// in a background goroutine. Pieces are marked complete/missing in
	// the bolt store as the pass progresses. Progress stays at 0% briefly
	// and then jumps up as pieces verify.
	//
	// Cost: ~3-5 minutes for a 50 GB file on a fast disk. Acceptable on
	// restart (rare event) in exchange for guaranteed-correct state that
	// can't drift away from what's actually on disk.
	//
	// Only done on RESTORE, not on fresh Add — new torrents have no data
	// on disk to verify. Callers pass verifyOnStart=true when they know
	// the torrent is being re-added from the DB (see restoreFromDB).
	if verifyOnStart {
		s.logger.Info("verifying existing file data — this may take a few minutes",
			"hash", hash, "name", mt.t.Name())
		// Background context: waitAndStart runs as a goroutine tied to
		// the session lifetime, not to any request. VerifyDataContext only
		// uses the context to short-circuit the hash pass on cancel — which
		// we don't do here; the pass runs to completion on startup.
		_ = mt.t.VerifyDataContext(context.Background())
	}

	if sequential {
		mt.t.SetDisplayName(mt.t.Name())
	}

	if !paused {
		mt.t.DownloadAll()
	}

	// Update DB with name, size, and .torrent bytes now that we have metadata.
	// Saving the torrent bytes enables fast-resume on restart without re-fetching
	// metadata from peers.
	s.updateTorrentMeta(hash, mt.t.Name(), mt.t.Info().TotalLength())
	s.saveTorrentData(hash, mt.t)

	// Monitor for completion.
	go s.monitorCompletion(mt, hash)
}

// monitorCompletion watches for torrent completion.
func (s *Session) monitorCompletion(mt *managedTorrent, hash string) {
	if mt.t.Info() == nil {
		return
	}

	// Poll completion state.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if mt.t.BytesMissing() == 0 {
			s.logger.Info("torrent completed", "hash", hash, "name", mt.t.Name())
			now := time.Now().UTC()

			s.mu.Lock()
			if m, ok := s.torrents[hash]; ok {
				_ = m // completed_at tracked via DB
			}
			s.mu.Unlock()

			s.markCompleted(hash, now)

			// Rename files if configured and metadata is available.
			if s.cfg.RenameOnComplete {
				s.renameCompleted(hash, mt)
			}

			// Publish completion event BEFORE pausing — the queue poller
			// needs to see "completed" status to trigger the import pipeline.
			contentPath := mt.savePath
			if info := mt.t.Info(); info != nil {
				contentPath = fmt.Sprintf("%s/%s", mt.savePath, info.BestName())
			}
			s.bus.Publish(context.Background(), events.Event{
				Type:     events.TypeTorrentCompleted,
				InfoHash: hash,
				Data:     map[string]any{"name": mt.t.Name(), "path": contentPath},
			})

			// Immediately pause if configured to not seed. Read the
			// runtime-mutable value, not cfg.PauseOnComplete — UI
			// toggles go through SetPauseOnComplete and must be
			// respected here. See the settings handler for the write
			// path.
			if s.PauseOnComplete() {
				s.logger.Info("pause on complete enabled, pausing", "hash", hash)
				_ = s.Pause(hash)
			}
			return
		}

		// Check if torrent was removed.
		s.mu.RLock()
		_, exists := s.torrents[hash]
		s.mu.RUnlock()
		if !exists {
			return
		}
	}
}

// persistTorrent saves torrent metadata to the database.
//
// Tests may construct a Session with db=nil to exercise the torrent engine
// without a Postgres dependency; in that case persistence is a no-op.
func (s *Session) persistTorrent(hash string, mt *managedTorrent) {
	if s.db == nil {
		return
	}
	_, err := s.db.Exec(`
		INSERT INTO torrents (info_hash, name, save_path, category, added_at, sequential)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (info_hash) DO UPDATE SET
			name = EXCLUDED.name,
			save_path = EXCLUDED.save_path,
			category = EXCLUDED.category,
			added_at = EXCLUDED.added_at,
			sequential = EXCLUDED.sequential`,
		hash, mt.t.Name(), mt.savePath, mt.category, mt.addedAt, false,
	)
	if err != nil {
		s.logger.Error("failed to persist torrent", "hash", hash, "error", err)
	}

	// Persist tags.
	for _, tag := range mt.tags {
		_, _ = s.db.Exec(`INSERT INTO torrent_tags (info_hash, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hash, tag)
	}
}

// updateTorrentMeta updates name and size after metadata is received.
func (s *Session) updateTorrentMeta(hash, name string, size int64) {
	if s.db == nil {
		return
	}
	_, err := s.db.Exec(`UPDATE torrents SET name = $1, size_bytes = $2 WHERE info_hash = $3`, name, size, hash)
	if err != nil {
		s.logger.Error("failed to update torrent meta", "hash", hash, "error", err)
	}
}

// saveTorrentData persists the .torrent file bytes so the torrent can be
// restored on restart without needing to re-fetch metadata from peers.
func (s *Session) saveTorrentData(hash string, t *lt.Torrent) {
	if s.db == nil {
		return
	}
	mi := t.Metainfo()
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		s.logger.Error("failed to encode torrent metainfo", "hash", hash, "error", err)
		return
	}
	_, err := s.db.Exec(`UPDATE torrents SET torrent_data = $1 WHERE info_hash = $2`, buf.Bytes(), hash)
	if err != nil {
		s.logger.Error("failed to save torrent data", "hash", hash, "error", err)
	}
}

// markCompleted records the completion time.
func (s *Session) markCompleted(hash string, completedAt time.Time) {
	if s.db == nil {
		return
	}
	_, err := s.db.Exec(`UPDATE torrents SET completed_at = $1 WHERE info_hash = $2`, completedAt, hash)
	if err != nil {
		s.logger.Error("failed to mark torrent completed", "hash", hash, "error", err)
	}
}

// deleteTorrent removes a torrent from the database.
func (s *Session) deleteTorrent(hash string) {
	if s.db == nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM torrent_tags WHERE info_hash = $1`, hash)
	_, _ = s.db.Exec(`DELETE FROM torrents WHERE info_hash = $1`, hash)
}

// restoreFromDB loads previously saved torrents from the database.
// Torrents with saved .torrent data are restored via AddTorrent (fast-resume).
// Torrents without .torrent data (legacy rows) are cleaned up.
func (s *Session) restoreFromDB() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT info_hash, name, save_path, category, added_at, torrent_data, completed_at FROM torrents`)
	if err != nil {
		return fmt.Errorf("querying torrents: %w", err)
	}
	defer rows.Close()

	var restored, cleaned int
	for rows.Next() {
		var hash, name, savePath, category string
		var addedAt time.Time
		var torrentData []byte
		var completedAt *time.Time
		if err := rows.Scan(&hash, &name, &savePath, &category, &addedAt, &torrentData, &completedAt); err != nil {
			s.logger.Warn("skipping torrent row", "error", err)
			continue
		}

		// No .torrent data saved — can't restore without re-fetching metadata.
		if len(torrentData) == 0 {
			s.logger.Info("removing torrent without resume data", "hash", hash, "name", name)
			s.deleteTorrent(hash)
			cleaned++
			continue
		}

		// Already completed and paused — clean up unless files are still present.
		if completedAt != nil {
			s.logger.Info("skipping completed torrent", "hash", hash, "name", name)
			continue
		}

		// Restore from saved .torrent data — no peer/DHT metadata fetch needed.
		mi, parseErr := metainfo.Load(bytes_reader(torrentData))
		if parseErr != nil {
			s.logger.Warn("failed to parse saved torrent data, removing", "hash", hash, "error", parseErr)
			s.deleteTorrent(hash)
			cleaned++
			continue
		}

		t, addErr := s.client.AddTorrent(mi)
		if addErr != nil {
			s.logger.Warn("failed to re-add torrent, removing", "hash", hash, "error", addErr)
			s.deleteTorrent(hash)
			cleaned++
			continue
		}

		t.AddTrackers(DefaultPublicTrackers)

		mt := &managedTorrent{
			t:        t,
			paused:   false,
			category: category,
			savePath: savePath,
			tags:     nil,
			addedAt:  addedAt,
		}

		s.mu.Lock()
		s.torrents[hash] = mt
		s.mu.Unlock()

		// verifyOnStart=true — re-hash the .part file on disk so we
		// trust the actual bytes, not the (possibly stale or corrupted)
		// bolt completion store. Protects against the "everything
		// restarts from 0% after a restart" class of bugs where bolt
		// state gets out of sync with the file on disk.
		go s.waitAndStart(mt, hash, false, false, true)
		restored++
		s.logger.Info("restored torrent", "hash", hash, "name", name)
	}

	if restored > 0 || cleaned > 0 {
		s.logger.Info("torrent restore complete", "restored", restored, "cleaned", cleaned)
	}
	return rows.Err()
}
