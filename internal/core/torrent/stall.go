package torrent

import (
	"context"
	"fmt"
	"time"

	"github.com/beacon-stack/haul/internal/events"
)

// StallLevel classifies the severity of a stall.
type StallLevel int

const (
	StallNone          StallLevel = 0
	StallLevel1        StallLevel = 1 // No activity for stall_timeout — reannounce
	StallLevel2        StallLevel = 2 // No activity for 2x stall_timeout — force DHT
	StallLevel3        StallLevel = 3 // No activity for 5x stall_timeout — notify ecosystem
	StallNoPeersEver   StallLevel = 4 // Never got a single peer — classic "dead torrent" signal
)

// Stall reason strings. These show up in the event bus and in the HTTP
// API response for /torrents/{hash}/stall — Pilot/Prism use them to decide
// whether to blocklist the release.
const (
	ReasonNoPeersEver   = "no_peers_ever"  // pre-metadata, no peer in the firstPeerTimeout window
	ReasonNoPeers       = "no_peers"       // had peers at some point, now has none + no data for stall_timeout
	ReasonNoSeeders     = "no_seeders"     // has peers but no seeds, no data for stall_timeout
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

	stallTimeout := s.cfg.StallTimeout
	if stallTimeout <= 0 {
		stallTimeout = 120
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

		// ── Class 1: no peers ever ─────────────────────────────────────
		// This is the primary dead-torrent signal. It works for both
		// metadata-less magnets (mt.ready == false, BytesMissing == 0)
		// AND for .torrent files that arrived but found no peers.
		if mt.firstPeerAt == nil && now.Sub(mt.addedAt) > firstPeerTimeout {
			ageSecs := int64(now.Sub(mt.addedAt).Seconds())
			s.logger.Warn("stall: no peers ever observed, classifying as dead",
				"hash", hash, "age_secs", ageSecs, "ready", mt.ready)

			s.bus.Publish(ctx, events.Event{
				Type:     events.TypeTorrentStalled,
				InfoHash: hash,
				Data: map[string]any{
					"name":          nameOrEmpty(mt),
					"inactive_secs": ageSecs,
					"reason":        ReasonNoPeersEver,
					"level":         int(StallNoPeersEver),
					"peers":         0,
					"seeders":       0,
					"ready":         mt.ready,
					"archived":      false,
				},
			})
			// Don't auto-archive here. Let the downstream consumer
			// (Pilot's stallwatcher) decide whether to remove + blocklist.
			// Continuing to publish the event on each tick is fine — Pilot
			// dedups by info_hash in its blocklist.
			continue
		}

		// ── Class 2: activity-based escalation (needs metadata) ────────
		// Everything below here requires the torrent to have metadata,
		// because it relies on BytesMissing() and bytes-received progress.
		if !mt.ready || mt.t.BytesMissing() == 0 {
			continue
		}

		bytesRead := stats.ConnStats.BytesReadData.Int64()

		// Update last activity if data was received.
		s.mu.Lock()
		if mt.lastBytesRead != bytesRead {
			mt.lastBytesRead = bytesRead
			mt.lastActivityAt = now
		}
		lastActivity := mt.lastActivityAt
		s.mu.Unlock()

		// A torrent that has metadata but never received a byte yet falls
		// through the class-1 check above (firstPeerAt != nil but bytesRead
		// never ticked). Use the later of firstPeerAt and addedAt as the
		// reference for "when did you last have activity" so we don't
		// penalize freshly-added torrents that are mid-handshake.
		if lastActivity.IsZero() {
			lastActivity = mt.addedAt
			if mt.firstPeerAt != nil && mt.firstPeerAt.After(lastActivity) {
				lastActivity = *mt.firstPeerAt
			}
		}

		inactiveSecs := int64(now.Sub(lastActivity).Seconds())
		if inactiveSecs < int64(stallTimeout) {
			continue
		}

		// Determine stall level.
		level := StallLevel1
		if inactiveSecs >= 300 {
			level = StallLevel3
		} else if inactiveSecs >= int64(stallTimeout*2) {
			level = StallLevel2
		}

		reason := ReasonNoDataReceived
		if stats.ActivePeers == 0 {
			reason = ReasonNoPeers
		} else if stats.ConnectedSeeders == 0 {
			reason = ReasonNoSeeders
		}

		switch level {
		case StallLevel1:
			s.logger.Debug("stall level 1: reannouncing", "hash", hash, "inactive_secs", inactiveSecs)

		case StallLevel2:
			s.logger.Info("stall level 2: force DHT re-query", "hash", hash, "inactive_secs", inactiveSecs)

		case StallLevel3:
			s.logger.Warn("stall level 3: archiving stalled torrent", "hash", hash, "inactive_secs", inactiveSecs, "reason", reason)

			// Auto-archive: drop from engine entirely to free resources,
			// update DB category, and remove from in-memory map.
			mt.t.Drop()
			s.mu.Lock()
			if m, ok := s.torrents[hash]; ok {
				m.category = "archived"
				m.paused = true
			}
			delete(s.torrents, hash)
			s.mu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`UPDATE torrents SET category = 'archived' WHERE info_hash = $1`, hash)
			}

			s.bus.Publish(ctx, events.Event{
				Type:     events.TypeTorrentStalled,
				InfoHash: hash,
				Data: map[string]any{
					"name":          nameOrEmpty(mt),
					"inactive_secs": inactiveSecs,
					"reason":        reason,
					"level":         int(level),
					"peers":         stats.ActivePeers,
					"seeders":       stats.ConnectedSeeders,
					"archived":      true,
				},
			})
		}
	}
}

// GetStallInfo returns stall information for a specific torrent.
func (s *Session) GetStallInfo(hash string) (*StallInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}

	now := time.Now()

	// Pre-metadata "no peers ever" path.
	if mt.firstPeerAt == nil && now.Sub(mt.addedAt) > firstPeerTimeout {
		ageSecs := int64(now.Sub(mt.addedAt).Seconds())
		return &StallInfo{
			Stalled:      true,
			Level:        StallNoPeersEver,
			InactiveSecs: ageSecs,
			Reason:       ReasonNoPeersEver,
		}, nil
	}

	// Pre-activity / pre-metadata with no stall yet.
	if !mt.ready || mt.t.BytesMissing() == 0 {
		return &StallInfo{Stalled: false}, nil
	}

	stallTimeout := s.cfg.StallTimeout
	if stallTimeout <= 0 {
		stallTimeout = 120
	}

	lastActivity := mt.lastActivityAt
	if lastActivity.IsZero() {
		lastActivity = mt.addedAt
		if mt.firstPeerAt != nil && mt.firstPeerAt.After(lastActivity) {
			lastActivity = *mt.firstPeerAt
		}
	}

	inactiveSecs := int64(now.Sub(lastActivity).Seconds())
	if inactiveSecs < int64(stallTimeout) {
		return &StallInfo{
			Stalled:      false,
			InactiveSecs: inactiveSecs,
			LastActivity: &lastActivity,
		}, nil
	}

	level := StallLevel1
	if inactiveSecs >= int64(stallTimeout*5) {
		level = StallLevel3
	} else if inactiveSecs >= int64(stallTimeout*2) {
		level = StallLevel2
	}

	reason := ReasonNoDataReceived
	stats := mt.t.Stats()
	if stats.ActivePeers == 0 {
		reason = ReasonNoPeers
	} else if stats.ConnectedSeeders == 0 {
		reason = ReasonNoSeeders
	}

	return &StallInfo{
		Stalled:      true,
		Level:        level,
		InactiveSecs: inactiveSecs,
		LastActivity: &lastActivity,
		Reason:       reason,
	}, nil
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

		// Pre-metadata "no peers ever" path.
		if mt.firstPeerAt == nil && now.Sub(mt.addedAt) > firstPeerTimeout {
			out = append(out, StalledTorrent{
				InfoHash:     hash,
				Name:         nameOrEmpty(mt),
				Level:        StallNoPeersEver,
				Reason:       ReasonNoPeersEver,
				InactiveSecs: int64(now.Sub(mt.addedAt).Seconds()),
				AddedAt:      mt.addedAt,
			})
			continue
		}

		// Activity-based: require metadata and at least one undownloaded piece.
		if !mt.ready || mt.t.BytesMissing() == 0 {
			continue
		}
		lastActivity := mt.lastActivityAt
		if lastActivity.IsZero() {
			lastActivity = mt.addedAt
			if mt.firstPeerAt != nil && mt.firstPeerAt.After(lastActivity) {
				lastActivity = *mt.firstPeerAt
			}
		}
		stallTimeout := s.cfg.StallTimeout
		if stallTimeout <= 0 {
			stallTimeout = 120
		}
		inactive := int64(now.Sub(lastActivity).Seconds())
		if inactive < int64(stallTimeout) {
			continue
		}
		level := StallLevel1
		if inactive >= 300 {
			level = StallLevel3
		} else if inactive >= int64(stallTimeout*2) {
			level = StallLevel2
		}
		reason := ReasonNoDataReceived
		stats := mt.t.Stats()
		if stats.ActivePeers == 0 {
			reason = ReasonNoPeers
		} else if stats.ConnectedSeeders == 0 {
			reason = ReasonNoSeeders
		}
		out = append(out, StalledTorrent{
			InfoHash:     hash,
			Name:         nameOrEmpty(mt),
			Level:        level,
			Reason:       reason,
			InactiveSecs: inactive,
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
