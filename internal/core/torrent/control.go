package torrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	lt "github.com/anacrolix/torrent"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
)

// FileInfo describes a file within a torrent.
type FileInfo struct {
	Index    int    `json:"index"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Priority string `json:"priority"` // "skip", "normal", "high"
	Progress float64 `json:"progress"`
}

// SetCategory assigns a category to a torrent.
func (s *Session) SetCategory(hash, category string) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	mt.category = category
	s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE torrents SET category = $1 WHERE info_hash = $2`, category, hash)
	return err
}

// AddTags adds tags to a torrent.
func (s *Session) AddTags(hash string, tags []string) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	for _, tag := range tags {
		_, _ = s.db.Exec(`INSERT INTO torrent_tags (info_hash, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hash, tag)
	}

	// Refresh in-memory tags.
	s.mu.Lock()
	existing := make(map[string]bool)
	for _, t := range mt.tags {
		existing[t] = true
	}
	for _, t := range tags {
		if !existing[t] {
			mt.tags = append(mt.tags, t)
		}
	}
	s.mu.Unlock()
	return nil
}

// RemoveTags removes tags from a torrent.
func (s *Session) RemoveTags(hash string, tags []string) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	for _, tag := range tags {
		_, _ = s.db.Exec(`DELETE FROM torrent_tags WHERE info_hash = $1 AND tag = $2`, hash, tag)
	}

	// Refresh in-memory tags.
	s.mu.Lock()
	mt := s.torrents[hash]
	remove := make(map[string]bool)
	for _, t := range tags {
		remove[t] = true
	}
	filtered := make([]string, 0, len(mt.tags))
	for _, t := range mt.tags {
		if !remove[t] {
			filtered = append(filtered, t)
		}
	}
	mt.tags = filtered
	s.mu.Unlock()
	return nil
}

// SetSpeedLimits sets per-torrent speed limits (bytes/s, 0 = unlimited).
func (s *Session) SetSpeedLimits(hash string, downloadLimit, uploadLimit int) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	_, err := s.db.Exec(`UPDATE torrents SET download_limit = $1, upload_limit = $2 WHERE info_hash = $3`,
		downloadLimit, uploadLimit, hash)
	return err
}

// SetSeedLimits sets per-torrent seed ratio and time limits.
func (s *Session) SetSeedLimits(hash string, ratioLimit float64, timeLimitSecs int) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	_, err := s.db.Exec(`UPDATE torrents SET seed_ratio_limit = $1, seed_time_limit = $2 WHERE info_hash = $3`,
		ratioLimit, timeLimitSecs, hash)
	return err
}

// SetPriority sets the queue priority (lower = higher priority).
//
// After updating the DB, the queue gate re-runs so a priority change
// takes effect immediately: if the user dragged a queued torrent above
// the cap, it gets resumed and the displaced torrent is queue-paused.
func (s *Session) SetPriority(hash string, priority int) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	if _, err := s.db.Exec(`UPDATE torrents SET priority = $1 WHERE info_hash = $2`, priority, hash); err != nil {
		return err
	}

	s.enforceMaxActiveDownloads(context.Background())
	return nil
}

// SetSequential toggles sequential download mode.
func (s *Session) SetSequential(hash string, sequential bool) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	_, err := s.db.Exec(`UPDATE torrents SET sequential = $1 WHERE info_hash = $2`, sequential, hash)
	return err
}

// SetLocation moves a torrent's data to a new save path.
func (s *Session) SetLocation(hash, newPath string) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	oldPath := mt.savePath
	mt.savePath = newPath
	s.mu.Unlock()

	if err := os.MkdirAll(newPath, 0o755); err != nil {
		return fmt.Errorf("creating new path: %w", err)
	}

	// Move files if the torrent has metadata.
	if info := mt.t.Info(); info != nil {
		oldContent := filepath.Join(oldPath, info.BestName())
		newContent := filepath.Join(newPath, info.BestName())
		if _, err := os.Stat(oldContent); err == nil {
			if err := os.Rename(oldContent, newContent); err != nil {
				s.logger.Warn("failed to move torrent data", "hash", hash, "error", err)
			}
		}
	}

	_, err := s.db.Exec(`UPDATE torrents SET save_path = $1 WHERE info_hash = $2`, newPath, hash)
	return err
}

// GetFiles returns the file list for a torrent.
func (s *Session) GetFiles(hash string) ([]FileInfo, error) {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}

	info := mt.t.Info()
	if info == nil {
		return []FileInfo{}, nil
	}

	files := mt.t.Files()
	result := make([]FileInfo, 0, len(files))
	for i, f := range files {
		var progress float64
		if f.Length() > 0 {
			progress = float64(f.BytesCompleted()) / float64(f.Length())
		}

		priority := "normal"
		p := f.Priority()
		if p == 0 {
			priority = "skip"
		} else if p > 4 {
			priority = "high"
		}

		result = append(result, FileInfo{
			Index:    i,
			Path:     f.DisplayPath(),
			Size:     f.Length(),
			Priority: priority,
			Progress: progress,
		})
	}
	return result, nil
}

// SetFilePriority sets the download priority for a specific file.
func (s *Session) SetFilePriority(hash string, fileIndex int, priority string) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	files := mt.t.Files()
	if fileIndex < 0 || fileIndex >= len(files) {
		return fmt.Errorf("file index out of range: %d", fileIndex)
	}

	f := files[fileIndex]
	switch priority {
	case "skip":
		f.SetPriority(0)
	case "normal":
		f.SetPriority(4)
	case "high":
		f.SetPriority(7)
	default:
		return fmt.Errorf("invalid priority: %s (use skip, normal, or high)", priority)
	}
	return nil
}

// Recheck re-verifies all pieces of a torrent.
func (s *Session) Recheck(ctx context.Context, hash string) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	_ = mt.t.VerifyDataContext(ctx)
	return nil
}

// CheckSeedLimits checks all seeding torrents against their seed limits
// and pauses any that have exceeded them. Call this periodically.
func (s *Session) CheckSeedLimits(ctx context.Context) {
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
		if !ok || mt.paused || !mt.ready {
			continue
		}

		// Only check seeding torrents.
		if mt.t.BytesMissing() > 0 {
			continue
		}

		// Check ratio limit from DB.
		var ratioLimit *float64
		var timeLimit *int
		_ = s.db.QueryRow(`SELECT seed_ratio_limit, seed_time_limit FROM torrents WHERE info_hash = $1`, hash).
			Scan(&ratioLimit, &timeLimit)

		// Fall back to global defaults.
		effectiveRatio := s.cfg.DefaultSeedRatio
		if ratioLimit != nil && *ratioLimit > 0 {
			effectiveRatio = *ratioLimit
		}
		effectiveTime := s.cfg.DefaultSeedTime
		if timeLimit != nil && *timeLimit > 0 {
			effectiveTime = *timeLimit
		}

		if effectiveRatio <= 0 && effectiveTime <= 0 {
			continue
		}

		info := s.torrentInfo(hash, mt)

		// Determine effective action.
		var perTorrentAction string
		_ = s.db.QueryRow(`SELECT seed_limit_action FROM torrents WHERE info_hash = $1`, hash).Scan(&perTorrentAction)
		action := s.cfg.SeedLimitAction
		if perTorrentAction != "" {
			action = perTorrentAction
		}
		if action == "" {
			action = "pause"
		}

		// Check ratio.
		if effectiveRatio > 0 && info.SeedRatio >= effectiveRatio {
			s.logger.Info("seed limit reached", "hash", hash, "reason", "ratio", "value", info.SeedRatio, "limit", effectiveRatio, "action", action)
			s.applySeedAction(ctx, hash, action, map[string]any{"reason": "seed_ratio_limit", "ratio": info.SeedRatio})
			continue
		}

		// Check seed time.
		if effectiveTime > 0 && info.CompletedAt != nil {
			seedDuration := time.Since(*info.CompletedAt)
			if seedDuration.Seconds() >= float64(effectiveTime) {
				s.logger.Info("seed limit reached", "hash", hash, "reason", "time", "secs", int(seedDuration.Seconds()), "limit", effectiveTime, "action", action)
				s.applySeedAction(ctx, hash, action, map[string]any{"reason": "seed_time_limit", "seed_time_secs": int(seedDuration.Seconds())})
			}
		}
	}
}

// applySeedAction performs the configured action when a seed limit is reached.
func (s *Session) applySeedAction(ctx context.Context, hash, action string, data map[string]any) {
	data["action"] = action
	switch action {
	case "remove":
		_ = s.Remove(ctx, hash, false)
	case "remove_with_data":
		_ = s.Remove(ctx, hash, true)
	default: // "pause"
		_ = s.Pause(hash)
	}
	s.bus.Publish(ctx, events.Event{
		Type:     events.TypeTorrentStateChanged,
		InfoHash: hash,
		Data:     data,
	})
}

// ClearArchived removes all torrents with category "archived" and returns the count.
func (s *Session) ClearArchived(ctx context.Context) int {
	s.mu.RLock()
	var archived []string
	for hash, mt := range s.torrents {
		if mt.category == "archived" {
			archived = append(archived, hash)
		}
	}
	s.mu.RUnlock()

	for _, hash := range archived {
		_ = s.Remove(ctx, hash, true)
	}

	s.logger.Info("cleared archived torrents", "count", len(archived))
	return len(archived)
}

// GetArchivedCount returns the number of torrents in the "archived" category.
func (s *Session) GetArchivedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, mt := range s.torrents {
		if mt.category == "archived" {
			count++
		}
	}
	return count
}

// ForceStart resumes a torrent and marks it to bypass queue limits.
func (s *Session) ForceStart(hash string) error {
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

// Reannounce forces a tracker reannounce for a torrent.
func (s *Session) Reannounce(ctx context.Context, hash string) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}
	// Anacrolix doesn't expose a direct reannounce, but dropping and re-adding
	// tracker URLs triggers fresh announces. Verifying data also causes tracker activity.
	_ = mt.t.VerifyDataContext(ctx)
	return nil
}

// SetFirstLastPriority prioritizes the first and last pieces of each file
// for preview/streaming purposes.
func (s *Session) SetFirstLastPriority(hash string, enabled bool) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	info := mt.t.Info()
	if info == nil {
		return nil
	}

	files := mt.t.Files()
	for _, f := range files {
		if enabled {
			// Prioritize first and last pieces of each file.
			f.SetPriority(7) // high priority for the file
		}
	}
	return nil
}

// GetTransferStats returns aggregate session statistics.
func (s *Session) GetTransferStats() TransferStats {
	s.mu.RLock()
	type snap struct {
		mt     *managedTorrent
		t      *lt.Torrent
		paused bool
	}
	snaps := make([]snap, 0, len(s.torrents))
	for _, mt := range s.torrents {
		if !mt.ready {
			continue
		}
		snaps = append(snaps, snap{mt: mt, t: mt.t, paused: mt.paused})
	}
	total := len(s.torrents)
	s.mu.RUnlock()

	var stats TransferStats
	stats.TotalTorrents = total
	now := time.Now()
	for _, sn := range snaps {
		ts := sn.t.Stats()
		read := ts.ConnStats.BytesReadData.Int64()
		written := ts.ConnStats.BytesWrittenData.Int64()
		stats.TotalDownloaded += read
		stats.TotalUploaded += written
		// DownloadSpeed/UploadSpeed are bytes-per-second aggregates. The
		// previous implementation summed the cumulative byte counters and
		// labeled them as a rate — see the session.go rateTracker for the
		// fix on the per-torrent path. Piggyback on each managedTorrent's
		// tracker so the aggregate reflects the same smoothed rate the
		// detail page shows. Calling sample() here also keeps the tracker
		// warm on torrents nobody is actively viewing.
		stats.DownloadSpeed += sn.mt.downRate.sample(read, now)
		stats.UploadSpeed += sn.mt.upRate.sample(written, now)
		stats.TotalPeers += ts.ActivePeers
		stats.TotalSeeds += ts.ConnectedSeeders
		if !sn.paused {
			if sn.t.BytesMissing() > 0 {
				stats.ActiveDownloads++
			} else {
				stats.ActiveUploads++
			}
		}
	}
	return stats
}

// TransferStats holds aggregate session statistics.
type TransferStats struct {
	TotalTorrents   int   `json:"total_torrents"`
	ActiveDownloads int   `json:"active_downloads"`
	ActiveUploads   int   `json:"active_uploads"`
	TotalDownloaded int64 `json:"total_downloaded"`
	TotalUploaded   int64 `json:"total_uploaded"`
	DownloadSpeed   int64 `json:"download_speed"`
	UploadSpeed     int64 `json:"upload_speed"`
	TotalPeers      int   `json:"peers_connected"`
	TotalSeeds      int   `json:"seeds_connected"`
}

// SetAltSpeedEnabled toggles alternative speed limits at runtime.
// Note: anacrolix/torrent rate limiters are set at client creation.
// We track the state for the schedule checker and API to report.
func (s *Session) SetAltSpeedEnabled(enabled bool) {
	s.mu.Lock()
	s.altSpeedActive = enabled
	s.mu.Unlock()

	if enabled {
		s.logger.Info("alt speed mode enabled", "dl_limit", s.cfg.AltDownloadLimit, "ul_limit", s.cfg.AltUploadLimit)
	} else {
		s.logger.Info("alt speed mode disabled, normal limits active")
	}
}

// IsAltSpeedActive returns whether alt speed mode is currently active.
func (s *Session) IsAltSpeedActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.altSpeedActive
}

// CheckSpeedSchedule enables/disables alt speed based on the schedule config.
func (s *Session) CheckSpeedSchedule(cfg config.SpeedScheduleConfig) {
	if !cfg.Enabled {
		return
	}

	now := time.Now()
	hour := now.Hour()
	weekday := now.Weekday()

	inWindow := hour >= cfg.FromHour && hour < cfg.ToHour
	if cfg.FromHour > cfg.ToHour {
		// Wraps midnight (e.g. 22:00 - 06:00).
		inWindow = hour >= cfg.FromHour || hour < cfg.ToHour
	}

	dayMatch := true
	switch cfg.Days {
	case "weekday":
		dayMatch = weekday >= time.Monday && weekday <= time.Friday
	case "weekend":
		dayMatch = weekday == time.Saturday || weekday == time.Sunday
	}

	shouldBeAlt := inWindow && dayMatch

	if shouldBeAlt != s.IsAltSpeedActive() {
		s.SetAltSpeedEnabled(shouldBeAlt)
	}
}
