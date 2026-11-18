package torrent

import (
	"path/filepath"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// torrentContentPath returns where a torrent's data lives under savePath: the
// file (single-file torrent) or top-level folder (multi-file torrent)
// named after the torrent. It mirrors how anacrolix storage lays files
// out (storage.ToSafeFilePath on the torrent name), so the path matches
// what is actually on disk.
//
// The name comes from the .torrent file and is attacker-controlled. ok is
// false when the name doesn't give the torrent an entry of its own inside
// savePath: "." or "" (storage then writes the files straight into
// savePath), or a name that would escape it ("../x"). In that case the
// returned path is savePath itself, which is where the data is, and
// callers must NOT delete, move or rename it as if it were the torrent's
// own folder: that would act on everything else in savePath too.
func torrentContentPath(savePath string, info *metainfo.Info) (path string, ok bool) {
	if info == nil {
		return savePath, false
	}
	return contentPathForName(savePath, info.BestName())
}

func contentPathForName(savePath, name string) (string, bool) {
	rel, err := storage.ToSafeFilePath(name)
	if err != nil {
		return savePath, false
	}
	p := filepath.Join(savePath, rel)
	within, err := filepath.Rel(filepath.Clean(savePath), p)
	if err != nil || within == "." || !filepath.IsLocal(within) {
		return savePath, false
	}
	return p, true
}
