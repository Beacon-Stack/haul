package torrent

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/beacon-stack/haul/internal/events"
	"github.com/beacon-stack/haul/internal/version"
)

// Add adds a new torrent from a magnet link, URL, or .torrent file bytes.
func (s *Session) Add(ctx context.Context, req AddRequest) (result *Info, resultErr error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in Add", "panic", r)
			result = nil
			resultErr = fmt.Errorf("internal error adding torrent: %v", r)
		}
	}()

	source := classifyAddSource(req)
	s.logger.Info("add torrent", "source", source, "uri_preview", uriPreview(req.URI), "file_bytes", len(req.File))

	savePath := s.resolveSavePath(req.SavePath, req.Category)

	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return nil, fmt.Errorf("creating save path: %w", err)
	}

	var t *lt.Torrent
	var err error

	if len(req.File) > 0 {
		mi, parseErr := metainfo.Load(bytes.NewReader(req.File))
		if parseErr != nil {
			s.logger.Warn("add torrent rejected", "source", source, "reason", "parse failed", "error", parseErr)
			return nil, fmt.Errorf("parsing torrent file: %w", parseErr)
		}
		t, err = s.addMetaInfo(mi, savePath)
	} else if req.URI != "" {
		if strings.HasPrefix(req.URI, "magnet:") {
			t, err = s.addMagnet(req.URI, savePath)
			if err == nil && t != nil {
				// Bare magnets from public indexers often have no tracker URLs.
				// Without trackers, metadata resolution relies entirely on DHT,
				// which is slow/unreliable behind VPNs. Inject public trackers
				// to speed up peer discovery — same pattern as Gopeed/exatorrent.
				t.AddTrackers(DefaultPublicTrackers)
			}
		} else if strings.HasPrefix(req.URI, "data:application/x-bittorrent;base64,") {
			// Base64-encoded .torrent file from Prism/Pilot plugin or the UI upload path.
			b64 := req.URI[len("data:application/x-bittorrent;base64,"):]
			torrentBytes, decErr := base64.StdEncoding.DecodeString(b64)
			if decErr != nil {
				s.logger.Warn("add torrent rejected", "source", source, "reason", "base64 decode failed", "error", decErr)
				return nil, fmt.Errorf("decoding base64 torrent: %w", decErr)
			}
			mi, parseErr := metainfo.Load(bytes.NewReader(torrentBytes))
			if parseErr != nil {
				s.logger.Warn("add torrent rejected", "source", source, "reason", "parse failed", "error", parseErr)
				return nil, fmt.Errorf("parsing torrent from base64 data: %w", parseErr)
			}
			t, err = s.addMetaInfo(mi, savePath)
		} else if strings.HasPrefix(req.URI, "http://") || strings.HasPrefix(req.URI, "https://") {
			// HTTP/HTTPS URL — download the .torrent file first.
			t, err = s.addFromURL(ctx, req.URI, savePath)
		} else {
			s.logger.Warn("add torrent rejected", "source", source, "reason", "unsupported uri scheme")
			return nil, fmt.Errorf("unsupported URI scheme: %s", req.URI)
		}
	} else {
		s.logger.Warn("add torrent rejected", "source", source, "reason", "empty input")
		return nil, fmt.Errorf("either uri or file must be provided")
	}

	if err != nil {
		s.logger.Warn("add torrent rejected", "source", source, "reason", "engine rejected", "error", err)
		return nil, fmt.Errorf("adding torrent: %w", err)
	}
	if t == nil {
		s.logger.Warn("add torrent rejected", "source", source, "reason", "duplicate or invalid")
		return nil, fmt.Errorf("torrent handle is nil (duplicate or invalid)")
	}

	hash := t.InfoHash().HexString()

	mt := &managedTorrent{
		t:        t,
		paused:   req.Paused,
		category: req.Category,
		tags:     req.Tags,
		addedAt:  time.Now().UTC(),
		savePath: savePath,
	}

	s.mu.Lock()
	s.torrents[hash] = mt
	s.mu.Unlock()

	// Persist to database.
	s.persistTorrent(hash, mt)

	// Store requester metadata if provided.
	if req.Metadata != nil {
		_ = s.SetMetadata(hash, *req.Metadata)
	}

	s.bus.Publish(ctx, events.Event{
		Type:     events.TypeTorrentAdded,
		InfoHash: hash,
		Data:     map[string]any{"name": t.Name()},
	})

	s.logger.Info("torrent added", "hash", hash, "name", t.Name(), "source", source, "paused", req.Paused)

	// Wait for metadata in background, then start.
	go s.waitAndStart(mt, hash, req.Paused, false /* verifyOnStart — fresh add, nothing to verify */)

	return s.torrentInfo(hash, mt), nil
}

// classifyAddSource maps an AddRequest to a short source label used in
// log lines. Stable strings — operators may grep for them.
func classifyAddSource(req AddRequest) string {
	if len(req.File) > 0 {
		return "file"
	}
	switch {
	case strings.HasPrefix(req.URI, "magnet:"):
		return "magnet"
	case strings.HasPrefix(req.URI, "data:application/x-bittorrent;base64,"):
		return "data-uri"
	case strings.HasPrefix(req.URI, "http://"), strings.HasPrefix(req.URI, "https://"):
		return "http"
	case req.URI == "":
		return "empty"
	default:
		return "unknown"
	}
}

// uriPreview returns a short, log-safe prefix of the URI. Magnet hashes and
// HTTP URLs are kept; data URIs are truncated so we never spam the log
// with a base64 blob.
func uriPreview(uri string) string {
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "data:application/x-bittorrent;base64,") {
		return "data:application/x-bittorrent;base64,…"
	}
	if len(uri) > 200 {
		return uri[:200] + "…"
	}
	return uri
}

// addFromURL downloads a .torrent file from an HTTP/HTTPS URL and adds it,
// storing its data in dir.
func (s *Session) addFromURL(ctx context.Context, torrentURL, dir string) (*lt.Torrent, error) {
	// Use a client that doesn't follow redirects — we need to intercept
	// magnet: URI redirects from torznab proxies.
	noRedirectClient := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", torrentURL, err)
	}
	req.Header.Set("User-Agent", version.AppName+"/"+version.Version)

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching torrent from %s: %w", torrentURL, err)
	}
	defer resp.Body.Close()

	// Check for redirect to magnet link.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if strings.HasPrefix(location, "magnet:") {
			s.logger.Info("torrent URL redirected to magnet", "url", torrentURL)
			return s.addMagnet(location, dir)
		}
		// Follow non-magnet redirects manually.
		return s.addFromURL(ctx, location, dir)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching torrent from %s: HTTP %d", torrentURL, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MiB limit
	if err != nil {
		return nil, fmt.Errorf("reading torrent from %s: %w", torrentURL, err)
	}

	// Check if the response body is actually a magnet link (some sites return
	// magnet URIs as the response body instead of redirecting).
	if len(data) < 2048 && strings.HasPrefix(strings.TrimSpace(string(data)), "magnet:") {
		return s.addMagnet(strings.TrimSpace(string(data)), dir)
	}

	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parsing torrent from %s: %w", torrentURL, err)
	}

	return s.addMetaInfo(mi, dir)
}

// waitAndStart waits for metadata then begins downloading.
func (s *Session) waitAndStart(mt *managedTorrent, hash string, paused, verifyOnStart bool) {
	select {
	case <-mt.t.GotInfo():
	case <-mt.t.Closed():
		// Dropped before its metadata arrived (removed, or reopened at a
		// new location by SetLocation, which starts its own waitAndStart).
		return
	}

	// Mark as ready — safe to call Stats(), BytesMissing(), etc.
	// atomic store is safe without holding s.mu; readers don't need
	// the lock either.
	mt.ready.Store(true)

	s.logger.Info("torrent metadata received",
		"hash", hash,
		"name", mt.t.Name(),
		"size", mt.t.Info().TotalLength(),
	)

	// Re-hash the file on disk against the piece hashes in the metainfo.
	// This is how we rebuild completion state after a restart when bolt
	// might be stale, missing, or corrupted. It runs asynchronously in
	// anacrolix — VerifyData returns immediately and the hash pass happens
	// in a background goroutine. Pieces are marked complete/missing in
	// the bolt store as the pass progresses. Progress stays at 0% briefly
	// and then jumps up as pieces verify.
	//
	// Cost: ~3-5 minutes for a 50 GB file on a fast disk. Acceptable on
	// restart (rare event) in exchange for guaranteed-correct state that
	// can't drift away from what's actually on disk.
	//
	// Only done on RESTORE, not on fresh Add — new torrents have no data
	// on disk to verify. Callers pass verifyOnStart=true when they know
	// the torrent is being re-added from the DB (see restoreFromDB).
	if verifyOnStart {
		s.logger.Info("verifying existing file data — this may take a few minutes",
			"hash", hash, "name", mt.t.Name())
		// Background context: waitAndStart runs as a goroutine tied to
		// the session lifetime, not to any request. VerifyDataContext only
		// uses the context to short-circuit the hash pass on cancel — which
		// we don't do here; the pass runs to completion on startup.
		_ = mt.t.VerifyDataContext(context.Background())
	}

	if !paused {
		mt.t.DownloadAll()
	}

	// Apply the max-active-downloads cap now that this torrent is ready
	// (BytesMissing is meaningful once metadata has arrived). If we're
	// over the cap, this run will queue-pause the lowest-priority active
	// torrent — which may be the one we just started. The UI shows it
	// as "Queued" until a slot frees.
	s.enforceMaxActiveDownloads(context.Background())

	// Update DB with name, size, and .torrent bytes now that we have metadata.
	// Saving the torrent bytes enables fast-resume on restart without re-fetching
	// metadata from peers.
	s.updateTorrentMeta(hash, mt.t.Name(), mt.t.Info().TotalLength())
	s.saveTorrentData(hash, mt.t)

	// Monitor for completion.
	go s.monitorCompletion(mt, hash)
}

// monitorCompletion watches for torrent completion.
func (s *Session) monitorCompletion(mt *managedTorrent, hash string) {
	if mt.t.Info() == nil {
		return
	}

	// Poll completion state.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if mt.t.BytesMissing() == 0 {
			s.logger.Info("torrent completed", "hash", hash, "name", mt.t.Name())
			now := time.Now().UTC()

			s.markCompleted(hash, now)

			// Rename files if configured and metadata is available.
			if s.cfg.RenameOnComplete {
				s.renameCompleted(hash, mt)
			}

			// Publish completion event BEFORE pausing — the queue poller
			// needs to see "completed" status to trigger the import pipeline.
			contentPath, _ := torrentContentPath(mt.savePath, mt.t.Info())
			s.bus.Publish(context.Background(), events.Event{
				Type:     events.TypeTorrentCompleted,
				InfoHash: hash,
				Data:     map[string]any{"name": mt.t.Name(), "path": contentPath},
			})

			// Immediately pause if configured to not seed. Read the
			// runtime-mutable value, not cfg.PauseOnComplete — UI
			// toggles go through SetPauseOnComplete and must be
			// respected here. See the settings handler for the write
			// path.
			if s.PauseOnComplete() {
				s.logger.Info("pause on complete enabled, pausing", "hash", hash)
				_ = s.Pause(hash)
			}

			// Completion frees a download slot; promote the next
			// queued torrent into the gap. (If PauseOnComplete fired
			// above, Pause() already ran the gate — this second call
			// is a cheap no-op since nothing else has changed.)
			s.enforceMaxActiveDownloads(context.Background())
			return
		}

		// Stop if the torrent was removed, or replaced by SetLocation
		// (which reopens it as a new managedTorrent with its own monitor).
		s.mu.RLock()
		cur, exists := s.torrents[hash]
		s.mu.RUnlock()
		if !exists || cur != mt {
			return
		}
	}
}
