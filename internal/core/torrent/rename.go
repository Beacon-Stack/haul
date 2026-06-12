package torrent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beacon-stack/haul/internal/core/renamer"
)

// renameCompleted renames downloaded files using the renamer when
// RequesterMetadata is available. Called after a torrent finishes downloading.
//
// Skips the rename entirely when the requester is Pilot or Prism. Those
// services own the import + rename step in their own pipelines — if
// Haul renames first, the file's path changes out from under their
// importer mid-flight and the import either fails or finds the file
// only via a fuzzy name search. The rename_on_complete toggle is meant
// for standalone Haul deployments where no arr is in the loop; the UI
// already disables it when Pulse reports an arr peer (see Media
// Management page) but a legacy user with the toggle set on a saved
// state could still hit this path. Belt-and-suspenders.
func (s *Session) renameCompleted(hash string, mt *managedTorrent) {
	meta, err := s.GetMetadata(hash)
	if err != nil || meta == nil || meta.Title == "" {
		return // no metadata — keep original names
	}

	if shouldSkipRenameForRequester(meta.Requester) {
		s.logger.Debug("rename skipped: arr-requested torrent — let the manager handle the import",
			"hash", hash, "requester", meta.Requester)
		return
	}

	info := mt.t.Info()
	if info == nil {
		return
	}

	colon := renamer.ColonReplacement(s.cfg.ColonReplacement)
	if colon == "" {
		colon = renamer.ColonSpaceDash
	}

	quality := renamer.Quality{
		Name:  meta.Quality,
		Codec: meta.QualityCodec,
	}

	files := mt.t.Files()
	for _, f := range files {
		oldPath := filepath.Join(mt.savePath, f.DisplayPath())
		ext := filepath.Ext(oldPath)

		// Skip non-media files (nfo, txt, etc).
		if !isMediaExt(ext) {
			continue
		}

		var newName string
		switch meta.MediaType {
		case "tv":
			episodeFmt := s.cfg.EpisodeFormat
			if episodeFmt == "" {
				episodeFmt = renamer.DefaultEpisodeFormat
			}
			newName = renamer.ApplyEpisodeFormat(episodeFmt, renamer.Series{
				Title: meta.Title,
				Year:  meta.Year,
			}, renamer.Episode{
				SeasonNumber:  meta.SeasonNumber,
				EpisodeNumber: meta.EpisodeNumber,
				Title:         meta.EpisodeTitle,
			}, quality, colon)

		case "movie":
			movieFmt := s.cfg.MovieFormat
			if movieFmt == "" {
				movieFmt = renamer.DefaultMovieFormat
			}
			newName = renamer.ApplyMovieFormat(movieFmt, renamer.Movie{
				Title: meta.Title,
				Year:  meta.Year,
			}, quality, colon)

		default:
			continue
		}

		if newName == "" {
			continue
		}

		newPath := filepath.Join(filepath.Dir(oldPath), newName+ext)
		if oldPath == newPath {
			continue
		}

		// Handle collisions: add (1), (2), etc.
		newPath = uniquePath(newPath)

		s.logger.Info("renaming file", "hash", hash, "from", filepath.Base(oldPath), "to", filepath.Base(newPath))
		if err := os.Rename(oldPath, newPath); err != nil {
			s.logger.Warn("rename failed", "hash", hash, "error", err)
		}
	}

	// Rename the top-level folder if it's a multi-file torrent.
	if len(files) > 1 {
		s.renameTorrentFolder(hash, mt, meta, quality, colon)
	}
}

// renameTorrentFolder renames the top-level download directory for a torrent.
func (s *Session) renameTorrentFolder(hash string, mt *managedTorrent, meta *RequesterMetadata, quality renamer.Quality, colon renamer.ColonReplacement) {
	info := mt.t.Info()
	if info == nil {
		return
	}

	oldDir := filepath.Join(mt.savePath, info.BestName())
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return
	}

	var newDirName string
	switch meta.MediaType {
	case "tv":
		folderFmt := s.cfg.SeriesFolderFormat
		if folderFmt == "" {
			folderFmt = renamer.DefaultSeriesFolderFormat
		}
		newDirName = renamer.ApplyFolderFormat(folderFmt, meta.Title, meta.Year)

		// Add season folder inside.
		seasonFmt := s.cfg.SeasonFolderFormat
		if seasonFmt == "" {
			seasonFmt = renamer.DefaultSeasonFolderFormat
		}
		seasonDir := renamer.ApplySeasonFolderFormat(seasonFmt, meta.SeasonNumber)
		newDirName = filepath.Join(newDirName, seasonDir)
	case "movie":
		folderFmt := s.cfg.MovieFolderFormat
		if folderFmt == "" {
			folderFmt = renamer.DefaultMovieFolderFormat
		}
		newDirName = renamer.ApplyFolderFormat(folderFmt, meta.Title, meta.Year)
	default:
		return
	}

	newDir := filepath.Join(mt.savePath, newDirName)
	if oldDir == newDir {
		return
	}

	// Create parent dirs if needed (for nested series/season structure).
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		s.logger.Warn("failed to create rename dir", "error", err)
		return
	}

	newDir = uniquePath(newDir)
	s.logger.Info("renaming torrent folder", "hash", hash, "from", filepath.Base(oldDir), "to", newDirName)
	if err := os.Rename(oldDir, newDir); err != nil {
		s.logger.Warn("folder rename failed", "hash", hash, "error", err)
	}
}

// uniquePath returns a path with (1), (2), etc. appended if the path already exists.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}

	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)

	for i := 1; i < 100; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return p // give up after 100
}

// isMediaExt returns true for common video/audio file extensions.
func isMediaExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".wmv", ".flv", ".mov",
		".ts", ".m2ts", ".webm", ".ogv", ".divx",
		".mp3", ".flac", ".aac", ".ogg", ".wav", ".wma",
		".srt", ".sub", ".ass", ".ssa", ".idx":
		return true
	}
	return false
}

// shouldSkipRenameForRequester is the gate that prevents Haul from
// double-renaming files when an arr (Pilot/Prism) requested the
// torrent. Those services own the rename step in their own import
// pipelines; if Haul renames first, the file's path changes out
// from under their importer mid-flight. The list is exact-match —
// case-sensitive, no fuzzy substring matching — because Pilot/Prism
// stamp the requester field with one of these constants verbatim
// (see internal/core/torrent/metadata.go RequesterMetadata.Requester).
func shouldSkipRenameForRequester(requester string) bool {
	switch requester {
	case "pilot", "prism":
		return true
	}
	return false
}
