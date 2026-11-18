package torrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
)

// FileInfo describes a file within a torrent.
type FileInfo struct {
	Index    int     `json:"index"`
	Path     string  `json:"path"`
	Size     int64   `json:"size"`
	Priority string  `json:"priority"` // "skip", "normal", "high"
	Progress float64 `json:"progress"`
}

// categorySavePath returns the category's configured save path, or ""
// when the category doesn't exist, has no save path, or the session has
// no DB (tests).
func (s *Session) categorySavePath(category string) string {
	if s.db == nil || category == "" {
		return ""
	}
	var p string
	if err := s.db.QueryRow(`SELECT save_path FROM categories WHERE name = ?`, category).Scan(&p); err != nil {
		return ""
	}
	return p
}

// resolveSavePath picks where a new torrent is saved:
//
//  1. an explicit save path from the request,
//  2. the category's configured save path,
//  3. <download dir>/<category> for any other category, and
//  4. the download dir when there is no category.
//
// Step 3 is what lets a fresh install work with no setup: Pilot sends
// category "tv" and Prism "movies", so downloads land in
// /data/torrents/tv and /data/torrents/movies, the layout Sonarr and
// Radarr users know (qBittorrent's automatic torrent management does
// the same). Category names that aren't a single safe folder name fall
// back to the download dir rather than escaping it.
func (s *Session) resolveSavePath(explicit, category string) string {
	if explicit != "" {
		return explicit
	}
	if p := s.categorySavePath(category); p != "" {
		return p
	}
	if dir := categoryFolder(category); dir != "" {
		return filepath.Join(s.DownloadDir(), dir)
	}
	return s.DownloadDir()
}

// categoryFolder returns category as a folder name, or "" when it is
// empty or not a single plain path element.
func categoryFolder(category string) string {
	c := strings.TrimSpace(category)
	if c == "" || c == "." || c == ".." || strings.ContainsAny(c, `/\`+"\x00") {
		return ""
	}
	return c
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

	_, err := s.db.Exec(`UPDATE torrents SET category = ? WHERE info_hash = ?`, category, hash)
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
		_, _ = s.db.Exec(`INSERT INTO torrent_tags (info_hash, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, hash, tag)
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
		_, _ = s.db.Exec(`DELETE FROM torrent_tags WHERE info_hash = ? AND tag = ?`, hash, tag)
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

// SetSeedLimits sets per-torrent seed ratio and time limits.
func (s *Session) SetSeedLimits(hash string, ratioLimit float64, timeLimitSecs int) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	_, err := s.db.Exec(`UPDATE torrents SET seed_ratio_limit = ?, seed_time_limit = ? WHERE info_hash = ?`,
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

	if _, err := s.db.Exec(`UPDATE torrents SET priority = ? WHERE info_hash = ?`, priority, hash); err != nil {
		return err
	}

	s.enforceMaxActiveDownloads(context.Background())
	return nil
}

// SetLocation moves a torrent's data to a new save path and reopens the
// torrent there, so it keeps downloading and seeding from the new folder.
// (Moving the files alone left the torrent writing to the old folder.)
// The move is a rename, so newPath must be on the same filesystem.
func (s *Session) SetLocation(hash, newPath string) error {
	if newPath == "" || !filepath.IsAbs(newPath) {
		return fmt.Errorf("location must be an absolute path")
	}
	newPath = filepath.Clean(newPath)

	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("torrent not found: %s", hash)
	}
	oldPath := mt.savePath
	old := mt.t
	paused := mt.paused
	s.mu.Unlock()

	if filepath.Clean(oldPath) == newPath {
		return nil
	}
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		return fmt.Errorf("creating new path: %w", err)
	}

	mi := old.Metainfo()
	info := old.Info()
	// Refuse before dropping anything: with no folder of its own, "the
	// torrent's data" would be the whole save dir, and the rename below
	// would move everything else in it too.
	oldContent, ok := torrentContentPath(oldPath, info)
	if info != nil && !ok {
		return fmt.Errorf("can't move this torrent: its name doesn't give it a folder of its own")
	}
	// Drop first so the storage releases its files before they move.
	old.Drop()

	dest := newPath // where the data ends up
	if info != nil {
		newContent, _ := torrentContentPath(newPath, info)
		if exists(oldContent) {
			if err := os.Rename(oldContent, newContent); err != nil {
				// Reopen where the data still is rather than losing it.
				s.logger.Warn("failed to move torrent data; keeping it in place", "hash", hash, "error", err)
				if reErr := s.reopen(mt, hash, &mi, oldPath, paused); reErr != nil {
					return fmt.Errorf("moving data: %w (and reopening failed: %v)", err, reErr)
				}
				return fmt.Errorf("moving data (must be on the same filesystem): %w", err)
			}
		}
	}
	if err := s.reopen(mt, hash, &mi, dest, paused); err != nil {
		return err
	}
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE torrents SET save_path = ? WHERE info_hash = ?`, dest, hash)
	return err
}

// reopen re-adds a dropped torrent with storage rooted at dir and
// installs it as a new managedTorrent. The old managedTorrent is left
// untouched for the goroutines still holding it (they see the old,
// dropped torrent and stop); mutating it in place would race with them.
// It re-adds from the raw info bytes (when known) rather than rebuilding
// a spec from the torrent's metainfo, which trips v2 piece-layer checks.
func (s *Session) reopen(mt *managedTorrent, hash string, mi *metainfo.MetaInfo, dir string, paused bool) error {
	spec := &lt.TorrentSpec{Trackers: mi.UpvertedAnnounceList()}
	spec.InfoHash = metainfo.NewHashFromHex(hash)
	spec.InfoBytes = mi.InfoBytes
	t, err := s.addSpec(spec, dir)
	if err != nil {
		return fmt.Errorf("reopening torrent: %w", err)
	}
	t.AddTrackers(DefaultPublicTrackers)

	s.mu.Lock()
	next := &managedTorrent{
		t:           t,
		paused:      mt.paused,
		queuePaused: mt.queuePaused,
		category:    mt.category,
		requester:   mt.requester,
		tags:        append([]string(nil), mt.tags...),
		addedAt:     mt.addedAt,
		savePath:    dir,
		stalledAt:   mt.stalledAt,
		// Keep stall history so a moved torrent isn't judged as brand
		// new; byte counters restart with the new handle, so
		// lastBytesRead starts at zero.
		firstPeerAt:    mt.firstPeerAt,
		lastActivityAt: mt.lastActivityAt,
	}
	s.torrents[hash] = next
	s.mu.Unlock()

	go s.waitAndStart(next, hash, paused, true)
	return nil
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
		if !ok || mt.paused || !mt.ready.Load() {
			continue
		}

		// Only check seeding torrents.
		if mt.t.BytesMissing() > 0 {
			continue
		}

		// Check ratio limit from DB.
		var ratioLimit *float64
		var timeLimit *int
		_ = s.db.QueryRow(`SELECT seed_ratio_limit, seed_time_limit FROM torrents WHERE info_hash = ?`, hash).
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
		_ = s.db.QueryRow(`SELECT seed_limit_action FROM torrents WHERE info_hash = ?`, hash).Scan(&perTorrentAction)
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

// Reannounce forces a fresh announce to every tracker by restarting the
// torrent's announcers. ModifyTrackers stops the existing tracker
// scrapers and starts new ones, which announce immediately; Metainfo()
// clones the live announce list, so runtime-added trackers survive.
func (s *Session) Reannounce(hash string) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}
	mt.t.ModifyTrackers(mt.t.Metainfo().AnnounceList)
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
		if !mt.ready.Load() {
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
