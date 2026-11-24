package torrent

import (
	"bytes"
	"database/sql"
	"fmt"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/beacon-stack/haul/internal/dbtime"
)

// persistTorrent saves torrent metadata to the database.
//
// Tests may construct a Session with db=nil to exercise the torrent engine
// without a Postgres dependency; in that case persistence is a no-op.
func (s *Session) persistTorrent(hash string, mt *managedTorrent) {
	if s.db == nil {
		return
	}
	// removed_at is cleared on re-add so a previously-removed torrent
	// can be re-downloaded fresh and history-lookup callers see it as
	// active again. The other history fields (requester_*, completed_at)
	// are NOT cleared here — completed_at is set later by
	// monitorCompletion, and the requester_* fields come in via
	// SetMetadata (separate write path).
	_, err := s.db.Exec(`
		INSERT INTO torrents (info_hash, name, save_path, category, added_at, resolution)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (info_hash) DO UPDATE SET
			name = EXCLUDED.name,
			save_path = EXCLUDED.save_path,
			category = EXCLUDED.category,
			added_at = EXCLUDED.added_at,
			resolution = EXCLUDED.resolution,
			removed_at = NULL`,
		hash, mt.t.Name(), mt.savePath, mt.category, dbtime.Format(mt.addedAt), parseResolution(mt.t.Name()),
	)
	if err != nil {
		s.logger.Error("failed to persist torrent", "hash", hash, "error", err)
	}

	// Persist tags.
	for _, tag := range mt.tags {
		_, _ = s.db.Exec(`INSERT INTO torrent_tags (info_hash, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, hash, tag)
	}
}

// updateTorrentMeta updates name and size after metadata is received.
func (s *Session) updateTorrentMeta(hash, name string, size int64) {
	if s.db == nil {
		return
	}
	_, err := s.db.Exec(`UPDATE torrents SET name = ?, size_bytes = ?, resolution = ? WHERE info_hash = ?`, name, size, parseResolution(name), hash)
	if err != nil {
		s.logger.Error("failed to update torrent meta", "hash", hash, "error", err)
	}
}

// saveTorrentData persists the .torrent file bytes so the torrent can be
// restored on restart without needing to re-fetch metadata from peers.
func (s *Session) saveTorrentData(hash string, t *lt.Torrent) {
	if s.db == nil {
		return
	}
	mi := t.Metainfo()
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		s.logger.Error("failed to encode torrent metainfo", "hash", hash, "error", err)
		return
	}
	_, err := s.db.Exec(`UPDATE torrents SET torrent_data = ? WHERE info_hash = ?`, buf.Bytes(), hash)
	if err != nil {
		s.logger.Error("failed to save torrent data", "hash", hash, "error", err)
	}
}

// ExportTorrent returns the raw .torrent bytes for the given hash so an
// operator can re-add it elsewhere. Reads from the torrents.torrent_data
// column populated at add-time by saveTorrentData.
//
// Returns (nil, error) if the hash is unknown OR the torrent_data column
// is empty (legacy rows from before persistence wiring or before metadata
// arrived). Caller should distinguish those cases via the error message.
func (s *Session) ExportTorrent(hash string) ([]byte, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	var data []byte
	err := s.db.QueryRow(`SELECT torrent_data FROM torrents WHERE info_hash = ?`, hash).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("torrent not found: %s", hash)
	}
	if err != nil {
		return nil, fmt.Errorf("reading torrent_data: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("torrent metadata not yet available for %s", hash)
	}
	return data, nil
}

// markCompleted records the completion time.
func (s *Session) markCompleted(hash string, completedAt time.Time) {
	if s.db == nil {
		return
	}
	_, err := s.db.Exec(`UPDATE torrents SET completed_at = ? WHERE info_hash = ?`, dbtime.Format(completedAt), hash)
	if err != nil {
		s.logger.Error("failed to mark torrent completed", "hash", hash, "error", err)
	}
}

// deleteTorrent marks a torrent as removed but keeps the row so
// Pilot/Prism can still ask "did you ever download X?". A separate
// nightly purge job hard-deletes records whose removed_at is older
// than the configured retention window (default 365 days).
//
// torrent_tags is hard-deleted because tags are a per-torrent concept
// and have no historical-lookup value once the torrent is gone.
//
// On hash collision (anacrolix returns the existing torrent for a
// re-add of an already-known info_hash), this path runs after the
// re-add but the row already exists with removed_at = NULL — that's
// fine, the UPDATE is a no-op. New downloads of a previously-removed
// hash should clear removed_at; that's handled in the persist path
// (Add → persistTorrent's ON CONFLICT clause).
func (s *Session) deleteTorrent(hash string) {
	if s.db == nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM torrent_tags WHERE info_hash = ?`, hash)
	_, _ = s.db.Exec(`UPDATE torrents SET removed_at = ? WHERE info_hash = ? AND removed_at IS NULL`, dbtime.Format(time.Now()), hash)
}

// restoreFromDB loads previously saved torrents from the database.
// Torrents with saved .torrent data are restored via AddTorrent (fast-resume).
// Torrents without .torrent data (legacy rows) are cleaned up.
func (s *Session) restoreFromDB() error {
	if s.db == nil {
		return nil
	}

	// torrentRow buffers one row of the outer SELECT. We materialize
	// every row before processing so the connection holding the cursor
	// is released — restore needs to run nested queries (torrent_tags,
	// deleteTorrent) and the DB pool is sized to 1 connection.
	type torrentRow struct {
		hash, name, savePath, category, requester string
		addedAt                                   time.Time
		completedAt, stalledAt, removedAt         *time.Time
		torrentData                               []byte
	}

	// removed_at IS NULL: soft-deleted torrents stay in the table for
	// history (LookupHistory), but must never be re-added to the engine —
	// otherwise a cancelled download reappears on the dashboard after a
	// restart. The per-row guard below is a defensive backstop in case a
	// future caller drops this predicate.
	rows, err := s.db.Query(`SELECT info_hash, name, save_path, category, added_at, torrent_data, completed_at, stalled_at, requester_service, removed_at FROM torrents WHERE removed_at IS NULL`)
	if err != nil {
		return fmt.Errorf("querying torrents: %w", err)
	}

	var pending []torrentRow
	for rows.Next() {
		var r torrentRow
		// Time columns are TEXT in the SQLite schema; scan into nullable
		// strings and parse with dbtime, which also reads the
		// time.Time.String() format older Haul versions wrote.
		var addedAtStr string
		var completedAtStr, stalledAtStr, removedAtStr sql.NullString
		if err := rows.Scan(&r.hash, &r.name, &r.savePath, &r.category, &addedAtStr, &r.torrentData, &completedAtStr, &stalledAtStr, &r.requester, &removedAtStr); err != nil {
			s.logger.Warn("skipping torrent row", "error", err)
			continue
		}
		if t, err := dbtime.Parse(addedAtStr); err == nil {
			r.addedAt = t
		} else {
			s.logger.Warn("unreadable added_at; leaving it unset", "hash", r.hash, "value", addedAtStr)
		}
		if completedAtStr.Valid {
			if t, err := dbtime.Parse(completedAtStr.String); err == nil {
				r.completedAt = &t
			}
		}
		if stalledAtStr.Valid {
			if t, err := dbtime.Parse(stalledAtStr.String); err == nil {
				r.stalledAt = &t
			}
		}
		if removedAtStr.Valid {
			if t, err := dbtime.Parse(removedAtStr.String); err == nil {
				r.removedAt = &t
			}
		}
		pending = append(pending, r)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return rowsErr
	}

	var restored, cleaned int
	for _, r := range pending {
		// The SELECT already filters removed_at IS NULL; this guard makes
		// sure a removed torrent is never re-added to the engine even if
		// that filter is dropped.
		if r.removedAt != nil {
			continue
		}

		// No .torrent data saved — can't restore without re-fetching metadata.
		if len(r.torrentData) == 0 {
			s.logger.Info("removing torrent without resume data", "hash", r.hash, "name", r.name)
			s.deleteTorrent(r.hash)
			cleaned++
			continue
		}

		// Already completed and paused — clean up unless files are still present.
		if r.completedAt != nil {
			s.logger.Info("skipping completed torrent", "hash", r.hash, "name", r.name)
			continue
		}

		// Restore from saved .torrent data — no peer/DHT metadata fetch needed.
		mi, parseErr := metainfo.Load(bytes.NewReader(r.torrentData))
		if parseErr != nil {
			s.logger.Warn("failed to parse saved torrent data, removing", "hash", r.hash, "error", parseErr)
			s.deleteTorrent(r.hash)
			cleaned++
			continue
		}

		dir := s.restoreDir(r.savePath, mi)
		if dir != r.savePath && r.savePath != "" {
			s.logger.Info("restored torrent's data is in the download dir, not its save path; using the download dir",
				"hash", r.hash, "name", r.name, "save_path", r.savePath, "data_dir", dir)
			r.savePath = dir
			if s.db != nil {
				_, _ = s.db.Exec(`UPDATE torrents SET save_path = ? WHERE info_hash = ?`, dir, r.hash)
			}
		}
		t, addErr := s.addMetaInfo(mi, dir)
		if addErr != nil {
			s.logger.Warn("failed to re-add torrent, removing", "hash", r.hash, "error", addErr)
			s.deleteTorrent(r.hash)
			cleaned++
			continue
		}

		t.AddTrackers(DefaultPublicTrackers)

		// A torrent that was auto-paused by the stall watcher before
		// restart comes back paused so the user keeps seeing it on
		// the "needs attention" rail. tags are loaded from the
		// torrent_tags table below to preserve the auto-applied
		// 'stalled' tag (and any user-applied tags).
		isStalled := r.stalledAt != nil

		var tags []string
		if rows2, terr := s.db.Query(`SELECT tag FROM torrent_tags WHERE info_hash = ?`, r.hash); terr == nil {
			for rows2.Next() {
				var tag string
				if scanErr := rows2.Scan(&tag); scanErr == nil {
					tags = append(tags, tag)
				}
			}
			rows2.Close()
		}

		mt := &managedTorrent{
			t:         t,
			paused:    isStalled,
			category:  r.category,
			savePath:  r.savePath,
			tags:      tags,
			addedAt:   r.addedAt,
			stalledAt: r.stalledAt,
			requester: r.requester,
		}

		s.mu.Lock()
		s.torrents[r.hash] = mt
		s.mu.Unlock()

		// verifyOnStart=true — re-hash the .part file on disk so we
		// trust the actual bytes, not the (possibly stale or corrupted)
		// bolt completion store. Protects against the "everything
		// restarts from 0% after a restart" class of bugs where bolt
		// state gets out of sync with the file on disk.
		go s.waitAndStart(mt, r.hash, isStalled, true)
		restored++
		s.logger.Info("restored torrent", "hash", r.hash, "name", r.name)
	}

	if restored > 0 || cleaned > 0 {
		s.logger.Info("torrent restore complete", "restored", restored, "cleaned", cleaned)
	}
	return nil
}
