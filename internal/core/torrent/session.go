// Package torrent owns the anacrolix/torrent client lifecycle and the
// Session type that exposes it to the rest of haul.
//
// The session's wiring (listen port, DHT, IP blocklist, trackers) is easy
// to break in ways that leave every torrent at zero peers.
// TestSessionIntegration_DownloadFromPeer downloads from a local seeder
// through that wiring in under a second; run it after changing any of it.
package torrent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/dht/v2"
	peer_store "github.com/anacrolix/dht/v2/peer-store"
	"github.com/anacrolix/publicip"
	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"

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

// Session manages the torrent engine and wraps anacrolix/torrent.
type Session struct {
	client *lt.Client
	db     *sql.DB
	bus    *events.Bus
	logger *slog.Logger
	cfg    config.TorrentConfig

	// startedAt is when NewSession returned successfully. Used as a grace
	// period for stall detection — we don't want to flag torrents as dead
	// during the first few minutes while anacrolix is bootstrapping DHT,
	// discovering its public IP, and warming up trackers.
	startedAt time.Time

	// runtimeMu guards the runtime-mutable settings below. These override
	// cfg.* at call time and are updated by the settings HTTP handler when
	// the user flips a toggle in the UI. Without this layer, UI toggles
	// are phantom writes that only touch the DB and never affect behavior.
	runtimeMu          sync.RWMutex
	pauseOnComplete    bool
	maxActiveDownloads int
	// downloadDir overrides cfg.DownloadDir when set from the settings
	// page. See DownloadDir.
	downloadDir string

	// pieceCompletion is shared by every per-torrent storage (see
	// storage.go); storages caches one storage per save folder.
	pieceCompletion storage.PieceCompletion
	storageMu       sync.Mutex
	storages        map[string]storage.ClientImpl

	mu             sync.RWMutex
	torrents       map[string]*managedTorrent
	altSpeedActive bool

	// Rate limiters held as Session fields (not just config) so the
	// runtime settings dispatcher can call SetLimit on the same
	// *rate.Limiter that's wired into anacrolix's client. We initialize
	// them in NewSession even when no limit is configured — at rate.Inf
	// they're effectively no-ops, but the pointers are stable so SetLimit
	// at runtime takes effect without rebuilding the client.
	downloadLimiter *rate.Limiter
	uploadLimiter   *rate.Limiter
}

// managedTorrent pairs the library torrent handle with our metadata.
type managedTorrent struct {
	t      *lt.Torrent
	paused bool
	// queuePaused is true when the torrent was paused by the
	// max-active-downloads gate, NOT by an explicit user action. The
	// queue gate may unpause it later when a slot frees; user-paused
	// torrents (queuePaused=false, paused=true) stay paused forever
	// until the user explicitly resumes them.
	queuePaused bool
	category    string
	// requester records which Beacon service requested this torrent
	// ("pilot", "prism", "manual", or ""). Set by SetMetadata and on
	// restore from DB. Surfaced on Info so the UI can gate features
	// like "Re-search via Pilot" without a separate API call.
	requester      string
	tags           []string
	addedAt        time.Time
	savePath       string
	lastBytesRead  int64      // for stall detection
	lastActivityAt time.Time  // last time data was received (bytesRead increased)
	firstPeerAt    *time.Time // first time we observed ActivePeers > 0 (nil = never)
	// stalledAt marks the moment the stall watcher escalated this
	// torrent past level 3 and auto-paused it. Non-nil ⇒ the torrent
	// needs user attention; the UI surfaces this as a red badge + a
	// dashboard "Needs attention" rail. Distinct from the transient
	// classifyStall() flag (which is recomputed every getInfo call
	// while a torrent is downloading): stalledAt persists until the
	// user explicitly resumes the torrent.
	stalledAt *time.Time
	// ready flips true once GotInfo() has fired. Accessed by
	// waitAndStart (write) and torrentInfo / stall.go (read), often
	// from different goroutines without holding s.mu — atomic.Bool
	// makes those accesses race-free without forcing every reader
	// to take the session lock.
	ready atomic.Bool

	// Rate trackers convert anacrolix's cumulative byte counters into
	// smoothed bytes-per-second values by sampling on every getInfo call.
	// See rateTracker for the math. Value types (not pointers) so the
	// zero value is a valid empty tracker — no init needed on Add.
	downRate rateTracker
	upRate   rateTracker
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
	// Use mmap-backed storage rather than the classic POSIX WriteAt path.
	// On a 5 GB sequential download benchmark (Ubuntu 26.04 desktop ISO),
	// the classic file backend was ~10–20% slower than mmap because the
	// kernel can coalesce writes through the page cache and avoid a
	// syscall per chunk. anacrolix's own cmd/torrent reference (the
	// library author's example client) uses NewMMap; we'd been on the
	// older default. Same on-disk layout — bolt-backed completion store
	// is unchanged, so existing torrents resume cleanly after the swap.
	ltCfg.DefaultStorage = storage.NewMMapWithCompletion(cfg.DownloadDir, pieceCompletion)
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
		// Per-torrent half-open cap — the rate at which we can burn
		// through TCP/uTP handshakes during ramp-up. anacrolix's default
		// is 25, which throttles cold-start peer recruitment well
		// below libtorrent. Keep a healthy floor of 50 even when the
		// operator sets a very low MaxConnectionsPerTorrent.
		halfOpen := cfg.MaxConnectionsPerTorrent / 2
		if halfOpen < 50 {
			halfOpen = 50
		}
		ltCfg.HalfOpenConnsPerTorrent = halfOpen
	}

	// Pipeline more requests by raising the unverified-bytes back-pressure
	// cap and the hash-verification worker count. The default
	// MaxUnverifiedBytes=64 MiB combined with PieceHashersPerTorrent=2
	// is the choke point on multi-GB sequential downloads behind a
	// 100+ Mbps VPN: hashers can't keep up with the wire, the queue fills,
	// and request issuance pauses. 256 MiB and one hasher per ~2 cores
	// keeps the pipeline saturated on modest hardware without ballooning
	// memory on small torrents (it's a cap, not a target).
	ltCfg.MaxUnverifiedBytes = 256 << 20
	hashers := runtime.NumCPU() / 2
	if hashers < 4 {
		hashers = 4
	}
	ltCfg.PieceHashersPerTorrent = hashers

	// Dial faster on cold start. The default 10 dials/sec, burst 10 is
	// the variance source we measured: behind a VPN with 50–80 tracker
	// peers, anacrolix takes 5–8s just to issue the dials while libtorrent
	// fires them all at once. 100/sec, burst 50 is well below any
	// reasonable kernel/conntrack ceiling and matches what real-world Go
	// torrent clients (Gopeed, exatorrent) ship.
	ltCfg.DialRateLimiter = rate.NewLimiter(rate.Limit(100), 50)

	// Build the rate limiters once and hold pointers on Session so the
	// runtime dispatcher can mutate them via SetLimit/SetBurst without
	// rebuilding the client. Zero/unset configs are stored as
	// rate.Inf (effectively no limit) — anacrolix sees a non-nil
	// limiter that always grants tokens immediately.
	downLimit := rate.Inf
	downBurst := 1 << 30 // big enough to never throttle
	if cfg.GlobalDownloadLimit > 0 {
		downLimit = rate.Limit(cfg.GlobalDownloadLimit)
		downBurst = cfg.GlobalDownloadLimit
	}
	upLimit := rate.Inf
	upBurst := 1 << 30
	if cfg.GlobalUploadLimit > 0 {
		upLimit = rate.Limit(cfg.GlobalUploadLimit)
		upBurst = cfg.GlobalUploadLimit
	}
	downloadLimiter := rate.NewLimiter(downLimit, downBurst)
	uploadLimiter := rate.NewLimiter(upLimit, upBurst)
	ltCfg.DownloadRateLimiter = downloadLimiter
	ltCfg.UploadRateLimiter = uploadLimiter

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
		client:             client,
		db:                 db,
		bus:                bus,
		logger:             logger,
		cfg:                cfg,
		torrents:           make(map[string]*managedTorrent),
		startedAt:          time.Now(),
		pieceCompletion:    pieceCompletion,
		pauseOnComplete:    cfg.PauseOnComplete,
		maxActiveDownloads: cfg.MaxActiveDownloads,
		downloadLimiter:    downloadLimiter,
		uploadLimiter:      uploadLimiter,
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
		var maxStr string
		if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'max_active_downloads'`).Scan(&maxStr); err == nil {
			if n, err := strconv.Atoi(maxStr); err == nil {
				s.maxActiveDownloads = n
				logger.Info("max_active_downloads loaded from settings table", "value", n)
			}
		}
		// The download folder set on the settings page wins over the
		// configured one (env/YAML), like the other UI settings.
		var dirStr string
		if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'download_dir'`).Scan(&dirStr); err == nil && dirStr != "" {
			if err := s.SetDownloadDir(dirStr); err != nil {
				logger.Warn("ignoring download_dir from settings table", "value", dirStr, "error", err)
			} else {
				logger.Info("download_dir loaded from settings table", "value", s.DownloadDir())
			}
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

// Remove removes a torrent. If deleteFiles is true, downloaded data is deleted.
//
// DB cleanup runs in a defer so it survives a panic from anacrolix/torrent's
// Drop(). Otherwise a crash inside the library mid-Drop would leave the
// torrents row behind, restoreFromDB would re-add it on the next start,
// and the same panic would repeat: a crash loop.
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
		// Best-effort deletion of downloaded content. Only ever delete the
		// torrent's own file or folder: if its (attacker-controlled) name
		// doesn't give it one, the path would be the whole save dir.
		if p, ok := torrentContentPath(mt.savePath, mt.t.Info()); ok {
			_ = os.RemoveAll(p)
		} else {
			s.logger.Warn("not deleting files: torrent has no folder of its own in its save path",
				"hash", hash, "save_path", mt.savePath)
		}
	}

	s.bus.Publish(ctx, events.Event{
		Type:     events.TypeTorrentRemoved,
		InfoHash: hash,
	})

	// Removal frees a download slot; promote the next queued torrent.
	s.enforceMaxActiveDownloads(ctx)
	return nil
}

// Pause pauses a torrent. This is the user-initiated pause path: it
// clears queuePaused so the queue gate will treat this torrent as
// sticky-paused and never auto-resume it.
func (s *Session) Pause(hash string) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	mt.paused = true
	mt.queuePaused = false
	s.mu.Unlock()

	// anacrolix doesn't have a native pause — we cancel all pieces.
	// nil guard keeps unit tests that construct synthetic managedTorrents
	// without a real handle exercising the state-flip + gate-trigger path.
	if mt.t != nil {
		mt.t.CancelPieces(0, mt.t.NumPieces())
	}

	// Pausing frees a download slot — let the queue gate promote a
	// queued torrent into the gap.
	s.enforceMaxActiveDownloads(context.Background())
	return nil
}

// Resume resumes a paused torrent. This is the user-initiated resume
// path: it clears queuePaused so subsequent state changes are clean.
// The queue gate runs immediately after; if Resume puts the active set
// over the cap, the lowest-priority active torrent (which may be the
// one just resumed) gets queue-paused.
func (s *Session) Resume(hash string) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	mt.paused = false
	mt.queuePaused = false
	// Resuming clears any "auto-stalled" state — the user is taking
	// responsibility, so the torrent comes off the dashboard's "needs
	// attention" rail and the auto-applied tag comes off the row. If
	// peers stay zero, the stall watcher will re-apply both on its
	// next escalation.
	wasStalled := mt.stalledAt != nil
	mt.stalledAt = nil
	if wasStalled {
		mt.tags = slices.DeleteFunc(mt.tags, func(t string) bool { return t == "stalled" })
	}
	s.mu.Unlock()

	if wasStalled && s.db != nil {
		if _, err := s.db.Exec(`UPDATE torrents SET stalled_at = NULL WHERE info_hash = ?`, hash); err != nil {
			s.logger.Warn("resume: clear stalled_at failed", "hash", hash, "error", err)
		}
		if _, err := s.db.Exec(`DELETE FROM torrent_tags WHERE info_hash = ? AND tag = 'stalled'`, hash); err != nil {
			s.logger.Warn("resume: drop stalled tag failed", "hash", hash, "error", err)
		}
	}

	if mt.t != nil {
		mt.t.DownloadAll()
	}

	s.enforceMaxActiveDownloads(context.Background())
	return nil
}

// Close shuts down the torrent engine. The piece-completion store is owned
// by anacrolix once handed to it in NewSession — fileClientImpl.Close()
// closes it, so we must not close it again here (a double-close errors with
// "database not open" on bolt).
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

// SetGlobalDownloadLimit updates the global download rate limit at
// runtime. n is in bytes per second; n == 0 means unlimited. Operates
// on the *rate.Limiter that was wired into anacrolix's client at
// startup, so the new limit applies immediately to every active
// torrent without rebuilding the engine.
func (s *Session) SetGlobalDownloadLimit(n int) {
	if s.downloadLimiter == nil {
		return // shouldn't happen — we always init in NewSession
	}
	if n <= 0 {
		s.logger.Info("download rate limit changed via settings API", "to", "unlimited")
		s.downloadLimiter.SetLimit(rate.Inf)
		s.downloadLimiter.SetBurst(1 << 30)
		return
	}
	s.logger.Info("download rate limit changed via settings API", "bytes_per_sec", n)
	s.downloadLimiter.SetLimit(rate.Limit(n))
	s.downloadLimiter.SetBurst(n)
}

// SetGlobalUploadLimit mirrors SetGlobalDownloadLimit for upload.
func (s *Session) SetGlobalUploadLimit(n int) {
	if s.uploadLimiter == nil {
		return
	}
	if n <= 0 {
		s.logger.Info("upload rate limit changed via settings API", "to", "unlimited")
		s.uploadLimiter.SetLimit(rate.Inf)
		s.uploadLimiter.SetBurst(1 << 30)
		return
	}
	s.logger.Info("upload rate limit changed via settings API", "bytes_per_sec", n)
	s.uploadLimiter.SetLimit(rate.Limit(n))
	s.uploadLimiter.SetBurst(n)
}
