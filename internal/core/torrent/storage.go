package torrent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// Per-torrent storage.
//
// anacrolix writes a torrent's data through the storage it was added
// with. Haul used to add every torrent with the client's default
// storage, rooted at the startup download dir, so a torrent's save path
// (explicit, per category, or <download dir>/<category>) was recorded
// and reported to Pilot/Prism but the files always landed in the
// download dir root. Imports then looked in the wrong place.
//
// Each torrent is now added with mmap storage rooted at its own save
// path. Storages are cached per directory and all share the session's
// piece-completion store, so resume-after-restart works the same in
// every folder.

// storageFor returns the storage to add a torrent saved in dir with.
// nil means the client's default storage, which is rooted at the
// startup download dir, so torrents there behave exactly as before.
func (s *Session) storageFor(dir string) storage.ClientImpl {
	if s.pieceCompletion == nil || dir == "" || filepath.Clean(dir) == filepath.Clean(s.cfg.DownloadDir) {
		return nil
	}
	dir = filepath.Clean(dir)
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	if st, ok := s.storages[dir]; ok {
		return st
	}
	if s.storages == nil {
		s.storages = make(map[string]storage.ClientImpl)
	}
	// Never closed individually: closing an mmap client closes its piece
	// completion, which is shared. The shared store is closed with the
	// client's default storage when the session shuts down.
	st := storage.NewMMapWithCompletion(dir, s.pieceCompletion)
	s.storages[dir] = st
	return st
}

// addMetaInfo adds a torrent from metainfo, storing its data in dir.
func (s *Session) addMetaInfo(mi *metainfo.MetaInfo, dir string) (*lt.Torrent, error) {
	spec, err := lt.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return nil, err
	}
	return s.addSpec(spec, dir)
}

// addMagnet adds a torrent from a magnet URI, storing its data in dir.
func (s *Session) addMagnet(uri, dir string) (*lt.Torrent, error) {
	spec, err := lt.TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return nil, err
	}
	return s.addSpec(spec, dir)
}

func (s *Session) addSpec(spec *lt.TorrentSpec, dir string) (*lt.Torrent, error) {
	spec.Storage = s.storageFor(dir)
	t, _, err := s.client.AddTorrentSpec(spec)
	return t, err
}

// restoreDir picks the folder to reopen a restored torrent from. It is
// the recorded save path, unless that folder doesn't have the torrent's
// data but the download dir root does: torrents added before per-torrent
// storage existed were always written to the root, whatever save path
// they recorded. Using the root for those avoids re-downloading them.
func (s *Session) restoreDir(savePath string, mi *metainfo.MetaInfo) string {
	root := s.cfg.DownloadDir
	if savePath == "" || filepath.Clean(savePath) == filepath.Clean(root) {
		return root
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return savePath
	}
	name := info.BestName()
	if exists(filepath.Join(savePath, name)) || !exists(filepath.Join(root, name)) {
		return savePath
	}
	return root
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// DownloadDir returns where torrents without an explicit or category
// save path are saved. It starts as the configured download dir and can
// be changed at runtime from the settings page.
func (s *Session) DownloadDir() string {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	if s.downloadDir != "" {
		return s.downloadDir
	}
	return s.cfg.DownloadDir
}

// SetDownloadDir changes the download dir for torrents added from now on.
// Existing torrents stay where they are. The folder is created if needed.
func (s *Session) SetDownloadDir(dir string) error {
	if err := ValidateDownloadDir(dir); err != nil {
		return err
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating download dir: %w", err)
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.downloadDir != dir {
		s.logger.Info("download dir changed via settings", "from", s.downloadDir, "to", dir)
	}
	s.downloadDir = dir
	return nil
}

// ErrRelativeDownloadDir is returned for a download dir that isn't an
// absolute path.
var ErrRelativeDownloadDir = errors.New("download folder must be an absolute path, e.g. /data/torrents")

// ValidateDownloadDir checks a download dir before it is saved.
func ValidateDownloadDir(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return ErrRelativeDownloadDir
	}
	return nil
}
