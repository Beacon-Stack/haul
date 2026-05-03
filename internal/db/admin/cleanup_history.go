package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// HistoryEntry is one row of cleanup_history surfaced via the admin API.
// row_data is kept opaque (json.RawMessage) — the frontend can render it
// generically and only diagnostics that own a specific source_table know
// how to deserialize it for restore.
type HistoryEntry struct {
	ID              int64           `json:"id"`
	Diagnostic      string          `json:"diagnostic"`
	SourceTable     string          `json:"source_table"`
	SourcePK        string          `json:"source_pk"`
	RowData         json.RawMessage `json:"row_data"`
	DeletedAt       time.Time       `json:"deleted_at"`
	RequestID       string          `json:"request_id"`
	ActorKeyPrefix  string          `json:"actor_key_prefix"`
}

// HistoryListFilter narrows the cleanup_history query. All fields are
// optional; zero values disable the filter.
type HistoryListFilter struct {
	Diagnostic   string
	SourceTable  string
	Limit        int
	Offset       int
}

// CaptureContext is the metadata passed to InsertCleanupHistory so the
// audit row identifies the actor that triggered the cleanup. Filled by
// the HTTP handler from the request.
type CaptureContext struct {
	Diagnostic     string
	SourceTable    string
	RequestID      string
	ActorKeyPrefix string
}

// InsertCleanupHistory writes one cleanup_history row per (sourcePK, rowJSON)
// pair within an existing transaction. Use this when soft-deleting; skip
// it when hard-deleting. Returns the inserted IDs in the same order as
// the input pairs (caller may want to log them).
//
// rowJSON should be the JSONB-encoded full row at deletion time. The
// caller is responsible for encoding (typically `SELECT row_to_json(t)::jsonb FROM <table> t WHERE pk = ANY($1)`).
func InsertCleanupHistory(ctx context.Context, tx *sql.Tx, capture CaptureContext, sourcePKs []string, rowsJSON [][]byte) ([]int64, error) {
	if len(sourcePKs) != len(rowsJSON) {
		return nil, fmt.Errorf("InsertCleanupHistory: pks=%d rows=%d (must match)", len(sourcePKs), len(rowsJSON))
	}
	ids := make([]int64, 0, len(sourcePKs))
	for i, pk := range sourcePKs {
		var id int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO cleanup_history (diagnostic, source_table, source_pk, row_data, request_id, actor_key_prefix)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, capture.Diagnostic, capture.SourceTable, pk, rowsJSON[i], capture.RequestID, capture.ActorKeyPrefix).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("inserting cleanup_history row for %s/%s: %w", capture.SourceTable, pk, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ListHistory returns cleanup_history rows newest-first.
func (r *Registry) ListHistory(ctx context.Context, filter HistoryListFilter) ([]HistoryEntry, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{limit, filter.Offset}
	q := `
		SELECT id, diagnostic, source_table, source_pk, row_data, deleted_at, request_id, actor_key_prefix
		  FROM cleanup_history
		 WHERE ($3 = '' OR diagnostic = $3)
		   AND ($4 = '' OR source_table = $4)
		 ORDER BY deleted_at DESC, id DESC
		 LIMIT $1 OFFSET $2`
	args = append(args, filter.Diagnostic, filter.SourceTable)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing cleanup_history: %w", err)
	}
	defer rows.Close()

	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.Diagnostic, &e.SourceTable, &e.SourcePK, &raw, &e.DeletedAt, &e.RequestID, &e.ActorKeyPrefix); err != nil {
			return nil, fmt.Errorf("scanning cleanup_history row: %w", err)
		}
		e.RowData = json.RawMessage(raw)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeOlderThan hard-deletes cleanup_history rows whose deleted_at is
// older than now - retention. Returns rows affected. Idempotent.
func (r *Registry) PurgeOlderThan(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM cleanup_history
		 WHERE deleted_at < NOW() - $1::interval
	`, fmt.Sprintf("%d seconds", int64(retention.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("purging cleanup_history: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		r.logger.Info("cleanup_history purge",
			"rows_purged", n,
			"retention_seconds", int64(retention.Seconds()))
	}
	return n, nil
}

// GetHistoryEntry fetches a single cleanup_history row by id.
func (r *Registry) GetHistoryEntry(ctx context.Context, id int64) (*HistoryEntry, error) {
	var e HistoryEntry
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, diagnostic, source_table, source_pk, row_data, deleted_at, request_id, actor_key_prefix
		  FROM cleanup_history
		 WHERE id = $1
	`, id).Scan(&e.ID, &e.Diagnostic, &e.SourceTable, &e.SourcePK, &raw, &e.DeletedAt, &e.RequestID, &e.ActorKeyPrefix)
	if err != nil {
		return nil, err
	}
	e.RowData = json.RawMessage(raw)
	return &e, nil
}

// DeleteHistoryEntry removes one cleanup_history row by id (used after a
// successful restore so the entry doesn't reappear in the trash list).
func (r *Registry) DeleteHistoryEntry(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM cleanup_history WHERE id = $1`, id)
	return err
}
