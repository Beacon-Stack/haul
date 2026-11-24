package torrent

import (
	"fmt"
	"time"
)

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

// torrentInfo builds an Info struct from internal state.
// Returns a safe minimal Info if the torrent hasn't received metadata yet.
func (s *Session) torrentInfo(hash string, mt *managedTorrent) (result *Info) {
	// Guard against panics from anacrolix internals when metadata is missing.
	defer func() {
		if r := recover(); r != nil {
			s.logger.Warn("torrentInfo panic recovered", "hash", hash, "panic", r)
			result = &Info{
				InfoHash:  hash,
				Name:      hash,
				Status:    StatusDownloading,
				SavePath:  mt.savePath,
				Category:  mt.category,
				Tags:      mt.tags,
				AddedAt:   mt.addedAt,
				StalledAt: mt.stalledAt,
				Requester: mt.requester,
			}
		}
	}()

	t := mt.t

	// If metadata hasn't arrived yet, return minimal info. We still
	// classify stall here so the "no peers ever" pre-metadata case (a
	// magnet that never finds a single peer — the classic dead-torrent
	// signal) shows up as Stalled in the row response. Without this,
	// /api/v1/stalls would list the torrent but the dashboard's Stalled
	// filter pill would read 0 because that count derives from
	// Info.Stalled. StalledAt is the persistent auto-pause marker (set
	// by stall.go's level-3 path); both should surface here even with
	// no metadata.
	if !mt.ready.Load() {
		stallTimeoutSecs := s.cfg.StallTimeout
		if stallTimeoutSecs <= 0 {
			stallTimeoutSecs = 120
		}
		stalled := classifyStall(stallParams{
			now:              time.Now(),
			status:           StatusDownloading,
			hasInfo:          false,
			bytesMissing:     0,
			sessionStartedAt: s.startedAt,
			addedAt:          mt.addedAt,
			firstPeerAt:      mt.firstPeerAt,
			lastActivityAt:   mt.lastActivityAt,
			stallTimeout:     time.Duration(stallTimeoutSecs) * time.Second,
		}).Stalled
		return &Info{
			InfoHash:  hash,
			Name:      t.Name(),
			Status:    StatusDownloading,
			SavePath:  mt.savePath,
			Category:  mt.category,
			Tags:      mt.tags,
			AddedAt:   mt.addedAt,
			Stalled:   stalled,
			StalledAt: mt.stalledAt,
			Requester: mt.requester,
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
		// Queue-paused gets its own status so the UI can render queued
		// torrents distinctly from user-paused ones (the latter are
		// sticky; the former auto-resume when a slot frees).
		if mt.queuePaused {
			status = StatusQueued
		} else {
			status = StatusPaused
		}
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

	contentPath, _ := torrentContentPath(mt.savePath, t.Info())

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
	stalled := classifyStall(stallParams{
		now:              time.Now(),
		status:           status,
		hasInfo:          hasInfo,
		bytesMissing:     t.BytesMissing(),
		sessionStartedAt: s.startedAt,
		addedAt:          mt.addedAt,
		firstPeerAt:      mt.firstPeerAt,
		lastActivityAt:   mt.lastActivityAt,
		stallTimeout:     time.Duration(stallTimeoutSecs) * time.Second,
	}).Stalled

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
		StalledAt:    mt.stalledAt,
		Requester:    mt.requester,
	}
}
