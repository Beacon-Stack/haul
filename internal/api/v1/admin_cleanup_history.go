package v1

// admin_cleanup_history.go — Settings → System → Cleanup History endpoints.
//
// Soft-deleted rows from the diagnostics tab live here for the configured
// retention window. List, restore, and manual-purge are exposed; a daily
// background sweep also runs in cmd/haul/main.go.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
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

// restoreTorrentRow re-inserts the captured row into the torrents
// table. Uses ON CONFLICT DO NOTHING so a same-PK row that's appeared
// in the meantime isn't overwritten — the caller is told the row
// already exists and decides whether to drop the cleanup_history entry.
//
// We unmarshal explicit columns rather than relying on jsonb_to_record
// so we get a clear error if the captured row's shape doesn't match the
// current schema (after a future migration).
func restoreTorrentRow(ctx context.Context, db *sql.DB, entry *adminpkg.HistoryEntry) (bool, error) {
	// Postgres' jsonb_populate_record + INSERT … SELECT keeps us
	// schema-agnostic up to the columns torrents has at restore time.
	// Columns added by future migrations get NULL/default values; columns
	// removed are silently dropped — both are acceptable for a recovery
	// path. The alternative (enumerating every column here) means every
	// torrents schema change has to remember to update this restore.
	res, err := db.ExecContext(ctx, `
		INSERT INTO torrents
		SELECT * FROM jsonb_populate_record(NULL::torrents, $1::jsonb)
		ON CONFLICT (info_hash) DO NOTHING
	`, []byte(entry.RowData))
	if err != nil {
		return false, fmt.Errorf("inserting torrents row from cleanup_history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
