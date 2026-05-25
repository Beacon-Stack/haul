package v1

// admin_cleanup_history.go — Settings → System → Cleanup History endpoints.
//
// Soft-deleted rows from the diagnostics tab live here for the configured
// retention window. List, restore, and manual-purge are exposed; a daily
// background sweep also runs in cmd/haul/main.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	adminpkg "github.com/beacon-stack/haul/internal/db/admin"
)

type cleanupHistoryListInput struct {
	Diagnostic  string `query:"diagnostic"   doc:"Filter by diagnostic name"`
	SourceTable string `query:"source_table" doc:"Filter by source table"`
	Limit       int    `query:"limit"        doc:"Max rows (default 100, cap 500)"`
	Offset      int    `query:"offset"       doc:"Offset for pagination"`
}

type cleanupHistoryListOutput struct {
	Body []adminpkg.HistoryEntry
}

type cleanupHistoryRestoreInput struct {
	ID int64 `path:"id" doc:"cleanup_history row id"`
}

type cleanupHistoryRestoreOutput struct {
	Body struct {
		Restored bool   `json:"restored"`
		Reason   string `json:"reason,omitempty"` // populated when restored=false
	}
}

type cleanupHistoryPurgeInput struct {
	Body struct {
		OlderThanDays int `json:"older_than_days" doc:"Hard-delete rows whose deleted_at is older than this many days. 0 = use config retention."`
	}
}

type cleanupHistoryPurgeOutput struct {
	Body struct {
		RowsPurged int64 `json:"rows_purged"`
	}
}

// RegisterAdminCleanupHistoryRoutes wires the cleanup_history endpoints.
func RegisterAdminCleanupHistoryRoutes(api huma.API, registry *adminpkg.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "list-cleanup-history",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/cleanup-history",
		Summary:     "List soft-deleted rows still within retention",
		Tags:        []string{"Admin"},
	}, func(ctx context.Context, input *cleanupHistoryListInput) (*cleanupHistoryListOutput, error) {
		entries, err := registry.ListHistory(ctx, adminpkg.HistoryListFilter{
			Diagnostic:  input.Diagnostic,
			SourceTable: input.SourceTable,
			Limit:       input.Limit,
			Offset:      input.Offset,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if entries == nil {
			entries = []adminpkg.HistoryEntry{}
		}
		return &cleanupHistoryListOutput{Body: entries}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "restore-cleanup-history",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/cleanup-history/{id}/restore",
		Summary:     "Re-insert a soft-deleted row into its original table",
		Tags:        []string{"Admin"},
	}, func(ctx context.Context, input *cleanupHistoryRestoreInput) (*cleanupHistoryRestoreOutput, error) {
		entry, err := registry.GetHistoryEntry(ctx, input.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, huma.Error404NotFound("cleanup_history entry not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}

		// Restoration is per-table because column lists differ. Today
		// only `torrents` is supported (the only soft-deletable resource
		// shipped in Phase A1). New diagnostics that introduce new
		// source_tables must extend this switch.
		out := &cleanupHistoryRestoreOutput{}
		var restored bool
		switch entry.SourceTable {
		case "torrents":
			ok, err := restoreTorrentRow(ctx, registry.DB(), entry)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			restored = ok
		default:
			return nil, huma.Error400BadRequest("restore not supported for source_table: " + entry.SourceTable)
		}

		// Whether we actually re-inserted a row or the live row was
		// already present (PK conflict), the user's intent is satisfied
		// — the row IS in the live table. Drop the cleanup_history
		// entry either way so it doesn't keep showing up in the trash.
		if err := registry.DeleteHistoryEntry(ctx, entry.ID); err != nil {
			registry.Logger().Warn("restore: failed to delete cleanup_history entry",
				"id", entry.ID, "error", err)
		}
		out.Body.Restored = true
		if !restored {
			out.Body.Reason = "row already existed in live table; cleanup_history entry discarded"
		}
		registry.Logger().Warn("db_cleanup_restore",
			"event", "db_cleanup_restore",
			"history_id", entry.ID,
			"source_table", entry.SourceTable,
			"source_pk", entry.SourcePK)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "purge-cleanup-history",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/cleanup-history/purge",
		Summary:     "Hard-delete cleanup_history rows older than the retention window",
		Tags:        []string{"Admin"},
	}, func(ctx context.Context, input *cleanupHistoryPurgeInput) (*cleanupHistoryPurgeOutput, error) {
		days := input.Body.OlderThanDays
		if days <= 0 {
			days = 30 // mirror config default if caller didn't supply one
		}
		retention := time.Duration(days) * 24 * time.Hour
		n, err := registry.PurgeOlderThan(ctx, retention)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &cleanupHistoryPurgeOutput{}
		out.Body.RowsPurged = n
		return out, nil
	})
}

// restoreTorrentRow re-inserts the captured row into the torrents table.
// ON CONFLICT DO NOTHING so a same-PK row that's appeared in the meantime
// isn't overwritten.
//
// SQLite has no equivalent of Postgres' jsonb_populate_record. To stay
// schema-agnostic across future migrations we (a) read the current torrents
// column set from PRAGMA table_info, (b) parse the captured JSON snapshot,
// (c) drop any JSON keys that no longer match a real column, then (d)
// build a parameterised INSERT from what's left. New columns (added since
// the snapshot was taken) just get their DDL defaults; removed columns are
// silently skipped — same recovery semantics as the old Postgres path.
func restoreTorrentRow(ctx context.Context, db *sql.DB, entry *adminpkg.HistoryEntry) (bool, error) {
	cols, err := torrentsColumns(ctx, db)
	if err != nil {
		return false, err
	}
	allowed := make(map[string]bool, len(cols))
	for _, c := range cols {
		allowed[c] = true
	}

	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(entry.RowData, &snapshot); err != nil {
		return false, fmt.Errorf("decoding cleanup_history row_data: %w", err)
	}

	keep := make([]string, 0, len(snapshot))
	placeholders := make([]string, 0, len(snapshot))
	args := make([]any, 0, len(snapshot))
	for col, raw := range snapshot {
		if !allowed[col] {
			continue // column no longer exists in current torrents schema
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return false, fmt.Errorf("decoding cleanup_history value for %q: %w", col, err)
		}
		keep = append(keep, col)
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}
	if len(keep) == 0 {
		return false, fmt.Errorf("cleanup_history row has no columns matching torrents schema")
	}

	query := fmt.Sprintf(
		`INSERT INTO torrents (%s) VALUES (%s) ON CONFLICT (info_hash) DO NOTHING`,
		quoteIdentList(keep), strings.Join(placeholders, ", "),
	)
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("inserting torrents row from cleanup_history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// torrentsColumns returns the live torrents column names via PRAGMA table_info.
// The result is cached process-wide; the schema is created at startup and
// doesn't change while the binary runs.
func torrentsColumns(ctx context.Context, db *sql.DB) ([]string, error) {
	torrentsColumnsOnce.Do(func() {
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(torrents)`)
		if err != nil {
			torrentsColumnsErr = err
			return
		}
		defer rows.Close()
		var cols []string
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				torrentsColumnsErr = err
				return
			}
			cols = append(cols, name)
		}
		torrentsColumnsCache = cols
		torrentsColumnsErr = rows.Err()
	})
	return torrentsColumnsCache, torrentsColumnsErr
}

var (
	torrentsColumnsOnce  sync.Once
	torrentsColumnsCache []string
	torrentsColumnsErr   error
)

// quoteIdentList renders a slice of column names as a comma-separated list
// of double-quoted identifiers. Each name is already validated against the
// PRAGMA table_info result, so embedded quotes/whitespace are not possible
// in practice — the quoting is defence in depth.
func quoteIdentList(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
	}
	return strings.Join(out, ", ")
}
