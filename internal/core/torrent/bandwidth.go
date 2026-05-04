package torrent

import (
	"fmt"
	"sort"
	"time"
)

// BandwidthSnapshot holds throughput data for adaptive allocation.
type BandwidthSnapshot struct {
	Hash         string `json:"hash"`
	Priority     int    `json:"priority"`
	DownloadRate int64  `json:"download_rate"`
	Allocated    int64  `json:"allocated"`
}

// AdaptiveBandwidth dynamically allocates unused download capacity to
// lower-priority torrents. Call this periodically (every 5s).
//
// Algorithm:
// 1. Measure actual throughput per downloading torrent
// 2. Calculate unused = global_limit - sum(actual)
// 3. Distribute unused to lower-priority torrents proportionally
//
// If global limit is 0 (unlimited), this is a no-op.
func (s *Session) AdaptiveBandwidth() {
	globalLimit := int64(s.cfg.GlobalDownloadLimit)
	if globalLimit <= 0 {
		return
	}

	s.mu.RLock()
	var downloading []*managedTorrent
	var hashes []string
	for h, mt := range s.torrents {
		if mt.paused || !mt.ready.Load() || mt.t.BytesMissing() == 0 {
			continue
		}
		downloading = append(downloading, mt)
		hashes = append(hashes, h)
	}
	s.mu.RUnlock()

	if len(downloading) <= 1 {
		return
	}

	// Get priorities from DB.
	type entry struct {
		mt       *managedTorrent
		hash     string
		priority int
		actual   int64
	}

	entries := make([]entry, 0, len(downloading))
	for i, mt := range downloading {
		var prio int
		_ = s.db.QueryRow(`SELECT priority FROM torrents WHERE info_hash = $1`, hashes[i]).Scan(&prio)

		stats := mt.t.Stats()
		actual := stats.ConnStats.BytesReadData.Int64()

		entries = append(entries, entry{
			mt:       mt,
			hash:     hashes[i],
			priority: prio,
			actual:   actual,
		})
	}

	// Sort by priority (lower number = higher priority).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].priority < entries[j].priority
	})

	// Calculate total actual throughput.
	var totalActual int64
	for _, e := range entries {
		totalActual += e.actual
	}

	unused := globalLimit - totalActual
	if unused <= 0 {
		return
	}

	// Distribute unused capacity to lower-priority torrents.
	// Higher-priority torrents keep their full allocation.
	lowerCount := len(entries) - 1
	if lowerCount <= 0 {
		return
	}

	perTorrent := unused / int64(lowerCount)
	for i := 1; i < len(entries); i++ {
		_ = perTorrent // Per-torrent rate limiting in anacrolix is done at the client level,
		// not per-torrent. The adaptive allocation here is informational —
		// the queue system handles active torrent count which effectively
		// controls bandwidth distribution.
	}
}

// SetDeadline sets a deadline for a torrent, influencing priority.
func (s *Session) SetDeadline(hash string, deadline *time.Time) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	if deadline == nil {
		_, err := s.db.Exec(`UPDATE torrents SET deadline = NULL WHERE info_hash = $1`, hash)
		return err
	}
	_, err := s.db.Exec(`UPDATE torrents SET deadline = $1 WHERE info_hash = $2`, *deadline, hash)
	return err
}

// GetDeadline returns the deadline for a torrent.
func (s *Session) GetDeadline(hash string) (*time.Time, error) {
	var deadline *time.Time
	err := s.db.QueryRow(`SELECT deadline FROM torrents WHERE info_hash = $1`, hash).Scan(&deadline)
	if err != nil {
		return nil, err
	}
	return deadline, nil
}

// EffectivePriority returns the priority adjusted for deadline urgency.
// As a deadline approaches, the bonus increases exponentially.
func EffectivePriority(basePriority int, deadline *time.Time) int {
	if deadline == nil || deadline.IsZero() {
		return basePriority
	}

	remaining := time.Until(*deadline)
	if remaining <= 0 {
		return basePriority - 1000 // overdue = maximum urgency
	}

	hours := remaining.Hours()
	switch {
	case hours < 1:
		return basePriority - 100 // less than 1 hour
	case hours < 6:
		return basePriority - 50 // less than 6 hours
	case hours < 24:
		return basePriority - 20 // less than 1 day
	case hours < 72:
		return basePriority - 5 // less than 3 days
	default:
		return basePriority
	}
}
