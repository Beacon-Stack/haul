package torrent

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/beacon-stack/haul/internal/events"
)

// StallLevel classifies the severity of a stall.
type StallLevel int

const (
	StallNone        StallLevel = 0
	StallLevel1      StallLevel = 1 // No activity for stall_timeout — reannounce
	StallLevel2      StallLevel = 2 // No activity for 2x stall_timeout — force DHT
	StallLevel3      StallLevel = 3 // No activity for 5x stall_timeout — notify ecosystem
	StallNoPeersEver StallLevel = 4 // Never got a single peer — classic "dead torrent" signal
)

// Stall reason strings. These show up in the event bus and in the HTTP
// API response for /torrents/{hash}/stall — Pilot/Prism use them to decide
// whether to blocklist the release.
const (
	ReasonNoPeersEver    = "no_peers_ever" // pre-metadata, no peer in the firstPeerTimeout window
	ReasonNoPeers        = "no_peers"      // had peers at some point, now has none + no data for stall_timeout
	ReasonNoSeeders      = "no_seeders"    // has peers but no seeds, no data for stall_timeout
	ReasonNoDataReceived = "no_data_received"
)

// firstPeerTimeout is how long we wait for a torrent to see its first peer
// before we classify it as "no peers ever" (a.k.a. the dead-torrent case).
// 180s (3 min) is the default — tracker announce intervals are typically
// 60-120s and DHT bootstrap is fast in steady state, so a healthy torrent
// almost always sees its first peer well under 3 min.
//
// Exposed as a package variable (not a constant) so tests can lower it to
// ~100ms to exercise the stall path without waiting 3 minutes.
var firstPeerTimeout = 180 * time.Second

// sessionStartupGrace is how long after Session creation we refuse to fire
// "no peers ever" stalls. During this window anacrolix is still warming up
// its DHT routing table, discovering its external IP, and opening tracker
// connections — blaming individual torrents for that is a false positive.
//
// Exposed as a package variable (not a constant) so tests can lower it to
// ~50ms, and future runtime config can override it for slow-starting VPN
// environments where 10 min isn't enough.
var sessionStartupGrace = 10 * time.Minute

// SetFirstPeerTimeoutForTesting lets cross-package tests (e.g. api/v1
// stall handler tests) shrink the no-peers-ever stall threshold so a
// fresh torrent crosses it in milliseconds. Returns the previous value
// so the caller can restore it on cleanup. Production code must NOT
// call this — it changes the stall-detection contract globally.
func SetFirstPeerTimeoutForTesting(d time.Duration) time.Duration {
	prev := firstPeerTimeout
	firstPeerTimeout = d
	return prev
}

// SetSessionStartupGraceForTesting lets cross-package tests bypass the
// 10-minute warm-up suppression window so ListStalled actually surfaces
// the seeded torrent. Returns the previous value so the caller can
// restore it. Production code must NOT call this.
func SetSessionStartupGraceForTesting(d time.Duration) time.Duration {
	prev := sessionStartupGrace
	sessionStartupGrace = d
	return prev
}

// AddNoPeersTorrentForTesting registers a torrent in the session's
// internal map with `addedAt = past`, no peers, no metadata — the exact
// shape both GetStallInfo and ListStalled classify as no_peers_ever
// once firstPeerTimeout elapses. The hash is derived from `seed` so
// each test can predict the resulting info_hash.
//
// Cross-package callers (api/v1 stall handler tests) use this to seed
// state that the production add-torrent path would otherwise require
// real anacrolix peers to produce. The torrent is NOT registered with
// the anacrolix client — the stall classifiers only read managedTorrent
// fields, so the missing client-side handle is safe for the tested
// no-peers-ever path. Returns the info-hash hex.
//
// Production code MUST NOT call this — it bypasses Add, the session DB,
// and the lifecycle hooks.
func (s *Session) AddNoPeersTorrentForTesting(seed string, addedAt time.Time) string {
	var h [20]byte
	for i := 0; i < len(seed) && i < 20; i++ {
		h[i] = seed[i]
	}
	hashHex := fmt.Sprintf("%x", h)
	s.mu.Lock()
	s.torrents[hashHex] = &managedTorrent{
		addedAt:  addedAt,
		savePath: s.cfg.DownloadDir,
	}
	s.mu.Unlock()
	return hashHex
}

// stallTimeoutDuration returns the configured stall timeout,
// defaulting to 120s.
func (s *Session) stallTimeoutDuration() time.Duration {
	t := s.cfg.StallTimeout
	if t <= 0 {
		t = 120
	}
	return time.Duration(t) * time.Second
}

// stallFor classifies one managed torrent at time now via classifyStall.
// mt.t may be nil for test-seeded torrents; engine stats are only read
// once metadata is ready, which is also when they become meaningful.
func (s *Session) stallFor(mt *managedTorrent, now time.Time) StallInfo {
	status := StatusDownloading
	if mt.paused {
		status = StatusPaused
	}
	p := stallParams{
		now:              now,
		status:           status,
		sessionStartedAt: s.startedAt,
		addedAt:          mt.addedAt,
		firstPeerAt:      mt.firstPeerAt,
		lastActivityAt:   mt.lastActivityAt,
		stallTimeout:     s.stallTimeoutDuration(),
	}
	if mt.ready.Load() && mt.t != nil {
		stats := mt.t.Stats()
		p.hasInfo = true
		p.bytesMissing = mt.t.BytesMissing()
		p.activePeers = stats.ActivePeers
		p.connectedSeeders = stats.ConnectedSeeders
	}
	return classifyStall(p)
}

// StallInfo holds stall detection data for a torrent.
type StallInfo struct {
	Stalled      bool       `json:"stalled"`
	Level        StallLevel `json:"level"`
	InactiveSecs int64      `json:"inactive_secs"`
	LastActivity *time.Time `json:"last_activity,omitempty"`
	Reason       string     `json:"reason"`
}

// CheckStalls inspects all torrents for stall conditions and publishes
// events for the severe cases. There are two distinct stall classes:
//
//  1. **No peers ever**: the torrent was added more than firstPeerTimeout
//     ago but has never observed a single peer. Typically means the release
//     is dead (stale indexer data, trackers have no alive seeders), or
//     Haul's networking is misconfigured. Pilot/Prism subscribe to this
//     event and use it to blocklist the release.
//
//  2. **Activity-based escalation**: the torrent had peers at some point,
//     started downloading, but lost all activity. Progressive remediation:
//     Level 1 reannounce → Level 2 force DHT → Level 3 archive.
//
// Both classes are suppressed during the sessionStartupGrace window
// (default 10 min) because anacrolix itself is still warming up.
func (s *Session) CheckStalls(ctx context.Context) {
	// Skip the entire pass during the session startup grace window.
	if time.Since(s.startedAt) < sessionStartupGrace {
		return
	}

	s.mu.RLock()
	hashes := make([]string, 0, len(s.torrents))
	for h := range s.torrents {
		hashes = append(hashes, h)
	}
	s.mu.RUnlock()

	for _, hash := range hashes {
		s.mu.RLock()
		mt, ok := s.torrents[hash]
		s.mu.RUnlock()
		if !ok || mt.paused {
			continue
		}

		now := time.Now()

		// ── First-peer observation (all torrents, pre-metadata included) ──
		// If the torrent handle has active peers, timestamp that. The
		// managedTorrent.t handle is always non-nil after Add(), even
		// before metadata arrives — stats are accessible.
		stats := mt.t.Stats()
		if stats.ActivePeers > 0 && mt.firstPeerAt == nil {
			s.mu.Lock()
			if m, ok := s.torrents[hash]; ok && m.firstPeerAt == nil {
				t := now
				m.firstPeerAt = &t
			}
			s.mu.Unlock()
		}

		// ── Activity bookkeeping ───────────────────────────────────────
		// CheckStalls is the writer of lastActivityAt: record download
		// progress before classifying. Only meaningful once metadata has
		// arrived (BytesReadData needs a real transfer).
		if mt.ready.Load() {
			bytesRead := stats.ConnStats.BytesReadData.Int64()
			s.mu.Lock()
			if mt.lastBytesRead != bytesRead {
				mt.lastBytesRead = bytesRead
				mt.lastActivityAt = now
			}
			s.mu.Unlock()
		}

		st := s.stallFor(mt, now)
		if !st.Stalled {
			continue
		}

		switch st.Level {
		case StallNoPeersEver:
			// The primary dead-torrent signal: added more than
			// firstPeerTimeout ago, never observed a single peer. Works
			// for metadata-less magnets AND .torrent files with no swarm.
			s.logger.Warn("stall: no peers ever observed, classifying as dead",
				"hash", hash, "age_secs", st.InactiveSecs, "ready", mt.ready.Load())

			s.bus.Publish(ctx, events.Event{
				Type:     events.TypeTorrentStalled,
				InfoHash: hash,
				Data: map[string]any{
					"name":          nameOrEmpty(mt),
					"inactive_secs": st.InactiveSecs,
					"reason":        st.Reason,
					"level":         int(st.Level),
					"peers":         0,
					"seeders":       0,
					"ready":         mt.ready.Load(),
					"archived":      false,
				},
			})
			// Don't auto-archive here. Let the downstream consumer
			// (Pilot's stallwatcher) decide whether to remove + blocklist.
			// Continuing to publish the event on each tick is fine — Pilot
			// dedups by info_hash in its blocklist.

		case StallLevel1:
			s.logger.Debug("stall level 1: reannouncing", "hash", hash, "inactive_secs", st.InactiveSecs)

		case StallLevel2:
			s.logger.Info("stall level 2: force DHT re-query", "hash", hash, "inactive_secs", st.InactiveSecs)

		case StallLevel3:
			// Auto-pause (not drop): keep the torrent in the engine + the
			// /api/v1/torrents response so the user can see what's
			// stranded and decide what to do. Stamp stalledAt + add the
			// 'stalled' tag so the existing tag-filter UI can group and
			// surface them. Idempotent: a torrent already past level 3
			// stays paused without re-firing the bus event every tick.
			s.mu.Lock()
			alreadyStalled := mt.stalledAt != nil
			s.mu.Unlock()
			if alreadyStalled {
				continue
			}

			s.logger.Warn("stall level 3: pausing stalled torrent", "hash", hash, "inactive_secs", st.InactiveSecs, "reason", st.Reason)

			if err := s.Pause(hash); err != nil {
				s.logger.Warn("stall level 3: pause failed", "hash", hash, "error", err)
			}

			stalledAt := time.Now().UTC()
			s.mu.Lock()
			if m, ok := s.torrents[hash]; ok {
				m.stalledAt = &stalledAt
				// Auto-add the 'stalled' tag if it isn't already present.
				// The tag is the user-facing label for the dashboard
				// rail + the existing tag filter chip.
				if !slices.Contains(m.tags, "stalled") {
					m.tags = append(m.tags, "stalled")
				}
			}
			s.mu.Unlock()

			if s.db != nil {
				if _, err := s.db.Exec(`UPDATE torrents SET stalled_at = ? WHERE info_hash = ?`, stalledAt, hash); err != nil {
					s.logger.Warn("stall level 3: persist stalled_at failed", "hash", hash, "error", err)
				}
				if _, err := s.db.Exec(`INSERT INTO torrent_tags (info_hash, tag) VALUES (?, 'stalled') ON CONFLICT DO NOTHING`, hash); err != nil {
					s.logger.Warn("stall level 3: persist stalled tag failed", "hash", hash, "error", err)
				}
			}

			s.bus.Publish(ctx, events.Event{
				Type:     events.TypeTorrentStalled,
				InfoHash: hash,
				Data: map[string]any{
					"name":          nameOrEmpty(mt),
					"inactive_secs": st.InactiveSecs,
					"reason":        st.Reason,
					"level":         int(st.Level),
					"peers":         stats.ActivePeers,
					"seeders":       stats.ConnectedSeeders,
					// archived flag retained for compatibility with the
					// Pilot stallwatcher contract — true now means
					// "auto-paused" (semantically the same: needs
					// attention) rather than "dropped from engine".
					"archived":   true,
					"stalled_at": stalledAt.Format(time.RFC3339),
				},
			})
		}
	}
}

// GetStallInfo returns stall information for a specific torrent. Unlike
// the bulk ListStalled it also carries the inactivity counters for
// torrents that haven't crossed the stall threshold yet.
func (s *Session) GetStallInfo(hash string) (*StallInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}

	st := s.stallFor(mt, time.Now())
	return &st, nil
}

// StalledTorrent pairs a torrent's identity with its current stall status.
// Used by the HTTP /api/v1/stalls endpoint so a consumer (Pilot's stall
// watcher) can get all stalled torrents in one call instead of N+1.
type StalledTorrent struct {
	InfoHash     string     `json:"info_hash"`
	Name         string     `json:"name"`
	Level        StallLevel `json:"level"`
	Reason       string     `json:"reason"`
	InactiveSecs int64      `json:"inactive_secs"`
	AddedAt      time.Time  `json:"added_at"`
}

// ListStalled iterates all managed torrents and returns those currently
// classified as stalled, filtering out the session startup grace period.
// Semantics match CheckStalls exactly; callers get the same decisions
// without having to re-implement the heuristic client-side.
func (s *Session) ListStalled() []StalledTorrent {
	if time.Since(s.startedAt) < sessionStartupGrace {
		return nil
	}

	s.mu.RLock()
	hashes := make([]string, 0, len(s.torrents))
	for h := range s.torrents {
		hashes = append(hashes, h)
	}
	s.mu.RUnlock()

	out := make([]StalledTorrent, 0, len(hashes))
	now := time.Now()

	for _, hash := range hashes {
		s.mu.RLock()
		mt, ok := s.torrents[hash]
		s.mu.RUnlock()
		if !ok || mt.paused {
			continue
		}

		st := s.stallFor(mt, now)
		if !st.Stalled {
			continue
		}
		out = append(out, StalledTorrent{
			InfoHash:     hash,
			Name:         nameOrEmpty(mt),
			Level:        st.Level,
			Reason:       st.Reason,
			InactiveSecs: st.InactiveSecs,
			AddedAt:      mt.addedAt,
		})
	}

	return out
}

// nameOrEmpty returns the torrent's name, tolerating the pre-metadata case
// where calling mt.t.Name() might panic on an empty metainfo.
func nameOrEmpty(mt *managedTorrent) string {
	defer func() { _ = recover() }()
	if mt == nil || mt.t == nil {
		return ""
	}
	return mt.t.Name()
}
