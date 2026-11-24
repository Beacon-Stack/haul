package torrent

import (
	"bytes"
	"fmt"
	"strings"

	lt "github.com/anacrolix/torrent"
)

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
	if !mt.ready.Load() {
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
	if !mt.ready.Load() {
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

// AddTrackers appends one or more announce URLs to the torrent at the
// given tier. Idempotent — duplicates are silently ignored. Empty URLs
// are skipped. The torrent kicks off an announce on the new trackers
// asynchronously; effect is visible on next Trackers() call.
func (s *Session) AddTrackers(hash string, urls []string, tier int) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}
	clean := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" {
			clean = append(clean, u)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	// anacrolix's AddTrackers takes [][]string (one slice per tier).
	// We collapse all URLs into a single tier; multi-tier add isn't
	// useful for the operator-facing flow ("add this URL").
	tieredAnnounce := [][]string{clean}
	if tier > 0 {
		// Pad earlier tiers with empty slices so anacrolix indexes correctly.
		tieredAnnounce = make([][]string, tier+1)
		tieredAnnounce[tier] = clean
	}
	mt.t.AddTrackers(tieredAnnounce)
	s.persistTrackerChange(hash, mt)
	return nil
}

// RemoveTracker rebuilds the announce list for the torrent without the
// given URL. anacrolix doesn't expose a "remove this tracker" call, so
// we collect the current list, filter out the removed URL, and reset
// the list via SetAnnounceList.
func (s *Session) RemoveTracker(hash, url string) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("empty tracker URL")
	}
	mi := mt.t.Metainfo()
	newList := make([][]string, 0, len(mi.AnnounceList))
	for _, tier := range mi.AnnounceList {
		filtered := make([]string, 0, len(tier))
		for _, u := range tier {
			if u != url {
				filtered = append(filtered, u)
			}
		}
		if len(filtered) > 0 {
			newList = append(newList, filtered)
		}
	}
	mt.t.ModifyTrackers(newList)
	s.persistTrackerChange(hash, mt)
	return nil
}

// persistTrackerChange re-serializes the torrent's metainfo (with the
// updated tracker list) and writes the new bytes back to torrents.torrent_data.
// Without this, tracker edits are lost on restart because restoreFromDB
// rebuilds from the persisted bytes.
func (s *Session) persistTrackerChange(hash string, mt *managedTorrent) {
	if s.db == nil {
		return
	}
	mi := mt.t.Metainfo()
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		s.logger.Error("failed to encode metainfo after tracker change", "hash", hash, "error", err)
		return
	}
	if _, err := s.db.Exec(`UPDATE torrents SET torrent_data = ? WHERE info_hash = ?`, buf.Bytes(), hash); err != nil {
		s.logger.Error("failed to persist tracker change", "hash", hash, "error", err)
	}
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
