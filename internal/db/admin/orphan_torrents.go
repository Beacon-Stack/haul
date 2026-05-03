package admin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SessionRemover is the narrow surface OrphanTorrents needs from the
// torrent Session — just enough to look up which hashes are currently
// in-memory. Defined here (instead of importing core/torrent) so the
// admin package doesn't depend on the torrent engine.
//
// Note: we deliberately do NOT use Session.Remove for the cleanup —
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
// today's smoke-fixture cleanup surfaced — a delete via the API can
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

func (o *OrphanTorrents) Name() string        { return "orphan_torrents" }
func (o *OrphanTorrents) Description() string { return "Torrent rows in DB but not tracked by the in-memory engine" }

func (o *OrphanTorrents) Detect(ctx context.Context) ([]Row, error) {
	live := o.session.LiveHashes()

	// Pull the persisted set. Cheap — we have ~tens to ~thousands of
	// rows max in any realistic install.
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
		var hash, name string
		var addedAt sql.NullTime
		var completed bool
		if err := rows.Scan(&hash, &name, &addedAt, &completed); err != nil {
			return nil, fmt.Errorf("scanning torrents row: %w", err)
		}
		if _, ok := live[hash]; ok {
			continue // tracked — not an orphan
		}
		state := "incomplete"
		if completed {
			state = "completed"
		}
		orphans = append(orphans, Row{
			ID:              hash,
			Summary:         fmt.Sprintf("%s — %s", truncName(name), state),
			WhyFlagged:      "no in-memory torrent for this info_hash",
			SuggestedAction: "Delete the orphan DB row + bolt-DB piece-completion entry",
		})
	}
	return orphans, rows.Err()
}

func truncName(s string) string {
	if len(s) > 60 {
		return s[:57] + "…"
	}
	return s
}

// Cleanup deletes the listed (or all-matching) orphans. Soft mode
// captures the full torrents row as JSONB into cleanup_history before
// removing. The actual DB row removal + bolt cleanup is delegated to
// session.RemoveByHash so we don't duplicate that logic here.
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
	// deleted — that would yank a live torrent.
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

	// Soft mode: snapshot + cleanup_history insert + direct DB delete in
	// a single transaction. Hard mode skips the cleanup_history insert
	// but still deletes in a transaction for atomicity.
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if req.Mode == ModeSoft {
		if err := captureTorrentsToHistory(ctx, tx, o.Name(), confirmed); err != nil {
			return CleanupResult{}, err
		}
	}

	// Direct DELETE FROM torrents — orphans aren't in the engine, so
	// Session.Remove can't help us here (it would return "not found").
	res, err := tx.ExecContext(ctx,
		`DELETE FROM torrents WHERE info_hash = ANY($1)`,
		pgArray(confirmed))
	if err != nil {
		return CleanupResult{}, fmt.Errorf("deleting orphan torrents: %w", err)
	}
	deleted, _ := res.RowsAffected()

	// Also drop any torrent_tags rows that referenced these hashes —
	// foreign-key cleanup that would otherwise leave dangling tags.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM torrent_tags WHERE info_hash = ANY($1)`,
		pgArray(confirmed)); err != nil {
		return CleanupResult{}, fmt.Errorf("deleting orphan torrent_tags: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{RowsDeleted: int(deleted)}, nil
}

// captureTorrentsToHistory snapshots torrents rows into cleanup_history
// within the supplied transaction. row_to_json over the full row keeps
// us schema-agnostic — new columns added in future migrations get
// captured automatically.
func captureTorrentsToHistory(ctx context.Context, tx *sql.Tx, diagnosticName string, hashes []string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT info_hash, row_to_json(t)::jsonb::text
		  FROM torrents t
		 WHERE info_hash = ANY($1)
	`, pgArray(hashes))
	if err != nil {
		return fmt.Errorf("snapshotting torrents: %w", err)
	}
	defer rows.Close()

	var pks []string
	var jsons [][]byte
	for rows.Next() {
		var pk string
		var rowJSON []byte
		if err := rows.Scan(&pk, &rowJSON); err != nil {
			return fmt.Errorf("scanning torrents snapshot: %w", err)
		}
		pks = append(pks, pk)
		jsons = append(jsons, rowJSON)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := InsertCleanupHistory(ctx, tx, CaptureContext{
		Diagnostic:  diagnosticName,
		SourceTable: "torrents",
	}, pks, jsons); err != nil {
		return err
	}
	return nil
}

// pgArray formats a string slice as a Postgres ARRAY[…] literal. Using
// pq's Array type would be cleaner, but haul's existing code uses raw
// SQL throughout — match the pattern.
func pgArray(s []string) string {
	if len(s) == 0 {
		return "{}"
	}
	parts := make([]string, len(s))
	for i, x := range s {
		parts[i] = `"` + strings.ReplaceAll(x, `"`, `\"`) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}
