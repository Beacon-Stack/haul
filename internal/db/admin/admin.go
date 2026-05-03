// Package admin owns the diagnostic registry and the soft-delete shim
// (cleanup_history) used by the Settings → System → Diagnostics tab.
//
// Each diagnostic surfaces "this looks abnormal" rows in a stable shape
// and offers a cleanup action. By default cleanup is soft — the deleted
// rows are first captured into cleanup_history as JSONB, then removed
// from their source table. A daily sweep purges rows older than the
// configured retention.
//
// See plans/db-inspector.md for the full design.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// CleanupMode selects whether a cleanup writes a cleanup_history row
// (recoverable) before deleting, or hard-deletes immediately.
type CleanupMode string

const (
	// ModeSoft writes the row to cleanup_history before deletion. The
	// row can be restored within the retention window. Default.
	ModeSoft CleanupMode = "soft"
	// ModeHard deletes the row immediately without writing
	// cleanup_history. Not recoverable.
	ModeHard CleanupMode = "hard"
)

// ParseMode validates a string mode and falls back to soft for empty.
func ParseMode(s string) (CleanupMode, error) {
	switch s {
	case "", string(ModeSoft):
		return ModeSoft, nil
	case string(ModeHard):
		return ModeHard, nil
	default:
		return "", fmt.Errorf("invalid cleanup mode %q (want soft or hard)", s)
	}
}

// Row is one matching record returned from a diagnostic. The shape is
// deliberately stable across all diagnostics so the frontend renders
// every diagnostic with the same component.
type Row struct {
	// ID identifies the row for cleanup. The diagnostic chooses the
	// representation (info_hash for torrents, integer id for tags, etc).
	ID string `json:"id"`
	// Summary is a one-line human-readable description of the row.
	Summary string `json:"summary"`
	// WhyFlagged states the test that matched (e.g. "no in-memory torrent").
	WhyFlagged string `json:"why_flagged"`
	// SuggestedAction is what cleanup will do (free-text for the modal).
	SuggestedAction string `json:"suggested_action"`
}

// CleanupRequest is what cleanup() receives — a list of IDs (or "all"
// to use whatever the diagnostic currently matches), plus the mode.
type CleanupRequest struct {
	IDs  []string
	All  bool
	Mode CleanupMode
}

// CleanupResult is what cleanup() returns to the handler. HistoryEntryIDs
// is populated only in soft mode; the frontend uses it to offer an Undo
// affordance on the success toast that calls restore for each id.
type CleanupResult struct {
	RowsDeleted      int     `json:"rows_deleted"`
	HistoryEntryIDs  []int64 `json:"history_entry_ids,omitempty"` // empty slice in hard mode
}

// Diagnostic is the contract every named diagnostic implements.
//
// Diagnostics MUST be deterministic given the current DB state — the
// frontend assumes Detect() called twice in a row returns the same rows
// (modulo intervening writes). They MUST be cheap enough to run on every
// diagnostics-tab open without rate-limiting.
type Diagnostic interface {
	// Name is a stable identifier used in URLs and logs (e.g. "orphan_torrents").
	Name() string
	// Description is a short one-line description shown in the UI.
	Description() string
	// Detect runs the diagnostic and returns matching rows.
	Detect(ctx context.Context) ([]Row, error)
	// Cleanup deletes the matching rows. Captures JSONB to cleanup_history
	// in soft mode; skips the capture in hard mode.
	Cleanup(ctx context.Context, req CleanupRequest) (CleanupResult, error)
}

// Registry owns the set of available diagnostics. Use Register at boot;
// Get by name; List for the diagnostics index.
type Registry struct {
	db          *sql.DB
	logger      *slog.Logger
	diagnostics map[string]Diagnostic
}

// New creates an empty Registry. Call Register on each diagnostic.
func New(db *sql.DB, logger *slog.Logger) *Registry {
	return &Registry{
		db:          db,
		logger:      logger,
		diagnostics: make(map[string]Diagnostic),
	}
}

// Register adds a diagnostic to the registry. Panics on duplicate name
// — diagnostic names must be unique and known at boot.
func (r *Registry) Register(d Diagnostic) {
	if _, dup := r.diagnostics[d.Name()]; dup {
		panic(fmt.Sprintf("diagnostic registered twice: %s", d.Name()))
	}
	r.diagnostics[d.Name()] = d
}

// Get returns the diagnostic with the given name, or nil.
func (r *Registry) Get(name string) Diagnostic {
	return r.diagnostics[name]
}

// List returns all registered diagnostics in insertion order — actually
// in iteration order (Go maps are randomized) but the frontend sorts
// by name anyway.
func (r *Registry) List() []Diagnostic {
	out := make([]Diagnostic, 0, len(r.diagnostics))
	for _, d := range r.diagnostics {
		out = append(out, d)
	}
	return out
}

// DB returns the database handle for diagnostics that need it.
func (r *Registry) DB() *sql.DB { return r.db }

// Logger returns the registry's logger for diagnostic implementations
// that want to emit structured logs.
func (r *Registry) Logger() *slog.Logger { return r.logger }

// ErrNoMatch is returned by cleanup when the requested IDs don't match
// any rows in the diagnostic's current detection set.
var ErrNoMatch = errors.New("no matching rows for cleanup")
