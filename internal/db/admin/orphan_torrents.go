package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SessionRemover is the narrow surface OrphanTorrents needs from the
// torrent Session - just enough to look up which hashes are currently
// in-memory. Defined here (instead of importing core/torrent) so the
// admin package doesn't depend on the torrent engine.
//
// Note: we deliberately do NOT use Session.Remove for the cleanup -
// orphan rows are by definition NOT in the in-memory map, so
// Session.Remove returns "torrent not found". Direct DB delete is the
// correct path; the bolt-DB piece-completion entry (if any) becomes a
// harmless tiny stale entry until the same hash is re-added.
type SessionRemover interface {
	// LiveHashes returns the info hashes currently tracked in memory.
	LiveHashes() map[string]struct{}
}

// OrphanTorrents detects torrent rows that exist in the DB but aren't
// tracked by the in-memory engine. These are the "ghost rows" that
// today's smoke-fixture cleanup surfaced - a delete via the API can
// silently leave the DB row behind in some edge cases, and on restart
// restoreFromDB happily re-loads them.
type OrphanTorrents struct {
	db      *sql.DB
	session SessionRemover
}

// NewOrphanTorrents constructs the diagnostic.
func NewOrphanTorrents(db *sql.DB, session SessionRemover) *OrphanTorrents {
	return &OrphanTorrents{db: db, session: session}
}

func (o *OrphanTorrents) Name() string { return "orphan_torrents" }
func (o *OrphanTorrents) Description() string {
	return "Torrent rows in DB but not tracked by the in-memory engine"
}

func (o *OrphanTorrents) Detect(ctx context.Context) ([]Row, error) {
	live := o.session.LiveHashes()

	rows, err := o.db.QueryContext(ctx, `
		SELECT info_hash, name, added_at, completed_at IS NOT NULL AS completed
		  FROM torrents
	`)
	if err != nil {
		return nil, fmt.Errorf("listing torrents: %w", err)
	}
	defer rows.Close()

	var orphans []Row
	for rows.Next() {
		var hash, name, addedAt string
		var completed bool
		if err := rows.Scan(&hash, &name, &addedAt, &completed); err != nil {
			return nil, fmt.Errorf("scanning torrents row: %w", err)
		}
		if _, ok := live[hash]; ok {
			continue // tracked - not an orphan
		}
		state := "incomplete"
		if completed {
			state = "completed"
		}
		orphans = append(orphans, Row{
			ID:              hash,
			Summary:         fmt.Sprintf("%s - %s", truncName(name), state),
			WhyFlagged:      "no in-memory torrent for this info_hash",
			SuggestedAction: "Delete the orphan DB row + bolt-DB piece-completion entry",
		})
	}
	return orphans, rows.Err()
}

func truncName(s string) string {
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

// Cleanup deletes the listed (or all-matching) orphans. Soft mode
// captures the full torrents row as JSON into cleanup_history before
// removing.
func (o *OrphanTorrents) Cleanup(ctx context.Context, req CleanupRequest) (CleanupResult, error) {
	hashes := req.IDs
	if req.All {
		// Re-detect to get the current set (don't trust stale frontend ids).
		current, err := o.Detect(ctx)
		if err != nil {
			return CleanupResult{}, err
		}
		hashes = make([]string, 0, len(current))
		for _, r := range current {
			hashes = append(hashes, r.ID)
		}
	}
	if len(hashes) == 0 {
		return CleanupResult{}, nil
	}

	// Re-confirm orphan-ness right before deleting. A torrent that came
	// back into the in-memory engine since Detect() ran should NOT be
	// deleted - that would yank a live torrent.
	live := o.session.LiveHashes()
	confirmed := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if _, ok := live[h]; !ok {
			confirmed = append(confirmed, h)
		}
	}
	if len(confirmed) == 0 {
		return CleanupResult{}, ErrNoMatch
	}

	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var historyIDs []int64
	if req.Mode == ModeSoft {
		ids, err := captureTorrentsToHistory(ctx, tx, o.Name(), confirmed)
		if err != nil {
			return CleanupResult{}, err
		}
		historyIDs = ids
	}

	args := stringsToAny(confirmed)
	placeholders := inPlaceholders(len(confirmed))

	res, err := tx.ExecContext(ctx,
		`DELETE FROM torrents WHERE info_hash IN (`+placeholders+`)`,
		args...)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("deleting orphan torrents: %w", err)
	}
	deleted, _ := res.RowsAffected()

	// Also drop any torrent_tags rows that referenced these hashes -
	// foreign-key cleanup that would otherwise leave dangling tags.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM torrent_tags WHERE info_hash IN (`+placeholders+`)`,
		args...); err != nil {
		return CleanupResult{}, fmt.Errorf("deleting orphan torrent_tags: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{RowsDeleted: int(deleted), HistoryEntryIDs: historyIDs}, nil
}

// captureTorrentsToHistory snapshots torrents rows into cleanup_history
// within the supplied transaction. Rather than Postgres' row_to_json,
// we scan all columns generically via rows.Columns() and marshal in Go -
// stays schema-agnostic across future migrations.
func captureTorrentsToHistory(ctx context.Context, tx *sql.Tx, diagnosticName string, hashes []string) ([]int64, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	args := stringsToAny(hashes)
	placeholders := inPlaceholders(len(hashes))

	rows, err := tx.QueryContext(ctx, `
		SELECT * FROM torrents
		 WHERE info_hash IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("snapshotting torrents: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("reading column metadata: %w", err)
	}
	pkIdx := -1
	for i, name := range cols {
		if name == "info_hash" {
			pkIdx = i
			break
		}
	}
	if pkIdx < 0 {
		return nil, fmt.Errorf("torrents schema missing info_hash column")
	}

	var pks []string
	var jsons [][]byte
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scanning torrents snapshot: %w", err)
		}
		obj := make(map[string]any, len(cols))
		for i, name := range cols {
			v := raw[i]
			// SQLite TEXT scans into []byte under modernc; normalise to
			// string so the JSON encoding is human-readable rather than
			// base64.
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			obj[name] = v
		}
		pk, _ := obj["info_hash"].(string)
		buf, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("marshalling torrent row: %w", err)
		}
		pks = append(pks, pk)
		jsons = append(jsons, buf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ids, err := InsertCleanupHistory(ctx, tx, CaptureContext{
		Diagnostic:  diagnosticName,
		SourceTable: "torrents",
	}, pks, jsons)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// inPlaceholders returns "?,?,?" with n entries, for use in an IN clause.
func inPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// stringsToAny converts a string slice to []any for ExecContext varargs.
func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
