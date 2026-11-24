package torrent

import (
	"context"

	"github.com/beacon-stack/haul/internal/events"
)

// MaxActiveDownloads returns the runtime-effective cap on concurrently
// downloading torrents. Zero or negative means unlimited.
func (s *Session) MaxActiveDownloads() int {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.maxActiveDownloads
}

// SetMaxActiveDownloads updates the runtime cap on concurrently
// downloading torrents and immediately re-runs the queue gate so the
// new value takes effect now (not just on next add/complete). Called by
// the settings HTTP handler.
func (s *Session) SetMaxActiveDownloads(n int) {
	s.runtimeMu.Lock()
	if s.maxActiveDownloads != n {
		s.logger.Info("max_active_downloads changed via settings API",
			"from", s.maxActiveDownloads, "to", n)
	}
	s.maxActiveDownloads = n
	s.runtimeMu.Unlock()

	s.enforceMaxActiveDownloads(context.Background())
}

// queueCandidate is one entry in the queue-gate decision input.
// Already filtered: user-paused, not-ready, and seeding torrents are
// excluded by the caller.
type queueCandidate struct {
	hash   string
	paused bool
	prio   int
}

// queueGateDecision is the pure math behind enforceMaxActiveDownloads.
// Input must be sorted ASC by `prio` (highest priority first); the top
// `max` entries are designated active, the rest queued. Returns hashes
// that need to be resumed (paused → active) and hashes that need to be
// queue-paused (active → paused).
//
// max <= 0 means "unlimited" — all candidates are designated active and
// any currently-paused ones get resumed.
//
// Pure function: no Session state, no anacrolix calls. Tested via
// TestQueueGateDecision_*.
func queueGateDecision(candidates []queueCandidate, max int) (resume, queue []string) {
	for i, c := range candidates {
		shouldRun := max <= 0 || i < max
		if shouldRun && c.paused {
			resume = append(resume, c.hash)
		} else if !shouldRun && !c.paused {
			queue = append(queue, c.hash)
		}
	}
	return resume, queue
}

// enforceMaxActiveDownloads pauses/resumes torrents to keep the count of
// actively-downloading torrents at or below MaxActiveDownloads.
//
// Torrents are ordered by the `priority` column ASC (lower value = higher
// priority); the top N still-incomplete torrents stay running, the rest
// are queue-paused with paused=queuePaused=true.
//
// Skipped entirely:
//   - User-paused torrents (paused=true, queuePaused=false). Sticky:
//     the user's explicit pause is preserved across gate runs.
//   - Torrents whose metadata hasn't arrived yet (ready=false). They'll
//     be reconsidered on a later run after waitAndStart marks them ready.
//   - Seeders (BytesMissing == 0). They've finished downloading, don't
//     compete for download slots.
//
// MaxActiveDownloads <= 0 disables the cap: all queue-paused torrents
// are released and nothing new gets queue-paused.
//
// Caller must NOT hold s.mu — this method takes the lock internally and
// also calls anacrolix's CancelPieces / DownloadAll which can be slow.
func (s *Session) enforceMaxActiveDownloads(ctx context.Context) {
	max := s.MaxActiveDownloads()

	// Read priority order from DB so the gate's choice of "who runs"
	// matches what the user sees in the UI list. List() also reads
	// `priority ASC` — keep the two in sync.
	priorityIdx := make(map[string]int)
	if s.db != nil {
		rows, err := s.db.Query(`SELECT info_hash, priority FROM torrents ORDER BY priority ASC`)
		if err == nil {
			i := 0
			for rows.Next() {
				var hash string
				var prio int
				if rows.Scan(&hash, &prio) == nil {
					priorityIdx[hash] = i
					i++
				}
			}
			rows.Close()
		}
	}

	type entry struct {
		hash string
		mt   *managedTorrent
		prio int
	}

	s.mu.RLock()
	entries := make([]entry, 0, len(s.torrents))
	for hash, mt := range s.torrents {
		if mt.paused && !mt.queuePaused {
			continue // sticky user pause
		}
		if !mt.ready.Load() {
			continue // metadata not loaded yet
		}
		if mt.t.BytesMissing() == 0 {
			continue // seeder, doesn't use a download slot
		}
		p, ok := priorityIdx[hash]
		if !ok {
			p = len(priorityIdx) + 1 // unknown → push to end
		}
		entries = append(entries, entry{hash: hash, mt: mt, prio: p})
	}
	s.mu.RUnlock()

	// Stable sort by DB priority (ASC).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].prio > entries[j].prio; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}

	cands := make([]queueCandidate, len(entries))
	byHash := make(map[string]*entry, len(entries))
	for i := range entries {
		cands[i] = queueCandidate{hash: entries[i].hash, paused: entries[i].mt.paused, prio: entries[i].prio}
		byHash[entries[i].hash] = &entries[i]
	}

	resume, queue := queueGateDecision(cands, max)

	if len(resume) == 0 && len(queue) == 0 {
		return
	}

	for _, hash := range resume {
		e := byHash[hash]
		s.mu.Lock()
		e.mt.paused = false
		e.mt.queuePaused = false
		s.mu.Unlock()
		e.mt.t.DownloadAll()
		s.bus.Publish(ctx, events.Event{
			Type:     events.TypeTorrentStateChanged,
			InfoHash: hash,
			Data:     map[string]any{"queue_action": "resumed"},
		})
	}
	for _, hash := range queue {
		e := byHash[hash]
		s.mu.Lock()
		e.mt.paused = true
		e.mt.queuePaused = true
		s.mu.Unlock()
		e.mt.t.CancelPieces(0, e.mt.t.NumPieces())
		s.bus.Publish(ctx, events.Event{
			Type:     events.TypeTorrentStateChanged,
			InfoHash: hash,
			Data:     map[string]any{"queue_action": "queued"},
		})
	}
}
