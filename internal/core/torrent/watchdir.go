package torrent

// watchdir.go — auto-add `.torrent` files dropped into a configured
// directory. Matches qBit's "Automatically download .torrents from this
// directory" Behavior option, and supports the long-standing pattern
// where RSS scripts / browser extensions / cron jobs deposit .torrent
// files in a folder for the client to pick up.
//
// Lifecycle: started from cmd/haul/main.go iff cfg.Torrent.WatchDir is
// non-empty. The goroutine survives until Session.Close (closes the
// fsnotify watcher).
//
// Behavior:
//  - Picks up files already present at startup (initial scan)
//  - Watches for CREATE / WRITE events thereafter
//  - Skips files that don't end in .torrent (case-insensitive)
//  - Reads the file, calls Session.Add with mode=file
//  - On success: deletes the source file (qBit calls this "Delete .torrent
//    files afterwards") so the watcher doesn't loop. We always delete
//    rather than expose a toggle — leaving consumed files lying around
//    causes more support pain than it solves.
//  - On parse failure: logs warn and renames the file to
//    "<orig>.invalid" so the user can see what failed.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// StartWatchDir launches the auto-add goroutine. Caller (main.go) is
// responsible for setting up Session lifecycle; we tie our cleanup to a
// passed-in done channel so a session shutdown closes the watcher
// cleanly. Errors during goroutine setup are returned synchronously;
// runtime errors are logged.
func (s *Session) StartWatchDir(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating watch dir: %w", err)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("starting fsnotify watcher: %w", err)
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return fmt.Errorf("adding %q to watcher: %w", dir, err)
	}

	s.logger.Info("watch dir started", "path", dir)

	go s.watchDirLoop(ctx, w, dir)
	return nil
}

func (s *Session) watchDirLoop(ctx context.Context, w *fsnotify.Watcher, dir string) {
	defer w.Close()

	// Initial scan — pick up any .torrent files already present when the
	// service started. Without this, files that arrived during downtime
	// are stranded forever.
	s.scanWatchDir(ctx, dir)

	// Debounce window: fsnotify often fires multiple events per file as
	// it's being written (CREATE, then several WRITEs). Wait this long
	// after the LAST event before processing, so partial files don't
	// race the parser.
	const debounce = 500 * time.Millisecond
	pending := map[string]*time.Timer{}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if !isTorrentFile(ev.Name) {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			// Debounce: cancel any previous pending timer for this path
			// and start a fresh one.
			if t, exists := pending[ev.Name]; exists {
				t.Stop()
			}
			path := ev.Name // capture for closure
			pending[ev.Name] = time.AfterFunc(debounce, func() {
				s.processWatchedFile(ctx, path)
			})
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			s.logger.Warn("watch dir error", "error", err)
		}
	}
}

func (s *Session) scanWatchDir(ctx context.Context, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.logger.Warn("watch dir initial scan failed", "dir", dir, "error", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !isTorrentFile(path) {
			continue
		}
		s.processWatchedFile(ctx, path)
	}
}

func (s *Session) processWatchedFile(ctx context.Context, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		s.logger.Warn("watch dir: read failed", "path", path, "error", err)
		return
	}
	if len(data) == 0 {
		// Almost certainly a partial write — leave it for the next event.
		return
	}
	if data[0] != 'd' {
		// Not bencoded; rename so it doesn't keep firing the watcher.
		s.markInvalid(path, "not a bencoded torrent")
		return
	}

	info, err := s.Add(ctx, AddRequest{File: data})
	if err != nil {
		s.logger.Warn("watch dir: add failed", "path", path, "error", err)
		s.markInvalid(path, err.Error())
		return
	}
	s.logger.Info("watch dir: added torrent",
		"path", path, "hash", info.InfoHash, "name", info.Name)

	// Consumed successfully — delete the source file. If we kept it,
	// the next watcher restart would re-add the same torrent.
	if err := os.Remove(path); err != nil {
		s.logger.Warn("watch dir: failed to remove consumed file",
			"path", path, "error", err)
	}
}

// markInvalid renames a file we couldn't parse so the watcher stops
// retrying it but the operator can still see what arrived.
func (s *Session) markInvalid(path, reason string) {
	dst := path + ".invalid"
	if err := os.Rename(path, dst); err != nil {
		s.logger.Warn("watch dir: failed to rename invalid file",
			"path", path, "error", err)
		return
	}
	s.logger.Info("watch dir: marked file invalid",
		"path", dst, "reason", reason)
}

func isTorrentFile(p string) bool {
	return strings.EqualFold(filepath.Ext(p), ".torrent")
}
