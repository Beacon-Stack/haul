package torrent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// HistoryRecord is the lookup-API view of a torrent's persistent
// history record. Pilot/Prism use this to answer "have I ever
// downloaded this episode/movie?" without polling Haul's live state.
//
// Both active and previously-removed torrents are returned (callers
// distinguish via RemovedAt). Files-on-disk status is NOT tracked
// here — Haul knows the save_path but not whether the file still
// exists; if Pilot/Prism care, they stat the path themselves.
type HistoryRecord struct {
	InfoHash    string `json:"info_hash"`
	Name        string `json:"name"`
	SavePath    string `json:"save_path"`
	Category    string `json:"category"`
	AddedAt     string `json:"added_at"`               // RFC3339
	CompletedAt string `json:"completed_at,omitempty"` // empty when still in progress
	RemovedAt   string `json:"removed_at,omitempty"`   // empty when active

	// Requester metadata — opaque IDs the arr supplied at grab time.
	// Empty when the torrent was added without metadata (e.g. directly
	// in Haul's UI rather than via Pilot/Prism).
	Requester string `json:"requester,omitempty"`
	MovieID   string `json:"movie_id,omitempty"`
	SeriesID  string `json:"series_id,omitempty"`
	EpisodeID string `json:"episode_id,omitempty"`
	TMDBID    int    `json:"tmdb_id,omitempty"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
}

// HistoryFilter narrows a LookupHistory query. All fields are optional
// — only non-zero values are added to the WHERE clause. The query
// joins the filters with AND.
type HistoryFilter struct {
	// Service narrows to "pilot" / "prism" / "manual" / "". Required
	// when looking up by an arr-side ID, since IDs are only unique
	// within a service's namespace.
	Service string

	// One of these is the typical lookup key. Multiple may be set
	// (combined with AND), but in practice the caller picks one.
	InfoHash  string
	MovieID   string
	SeriesID  string
	EpisodeID string

	// Semantic match — for "find any download for this episode of
	// this show, regardless of release". Requires at least TMDBID.
	TMDBID  int
	Season  int
	Episode int

	// IncludeRemoved=false (default) returns only active records.
	// Set true to include removed-but-not-yet-purged history.
	IncludeRemoved bool

	// Limit caps the result set. 0 → 100 default.
	Limit int
}

// buildHistoryQuery composes the SELECT + parameterized WHERE for a
// HistoryFilter. Pure logic, no I/O — exposed package-private so the
// unit test can pin the SQL shape across filter combinations without
// requiring a postgres test fixture.
func buildHistoryQuery(f HistoryFilter) (sql string, args []any) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}

	// Build the parameterized WHERE incrementally. Order doesn't
	// affect correctness; postgres's planner picks the index based
	// on the columns referenced.
	var clauses []string
	add := func(clause string, arg any) {
		args = append(args, arg)
		clauses = append(clauses, fmt.Sprintf(clause, len(args))) // $1, $2 …
	}
	if f.Service != "" {
		add("requester_service = $%d", f.Service)
	}
	if f.InfoHash != "" {
		add("info_hash = $%d", f.InfoHash)
	}
	if f.MovieID != "" {
		add("requester_movie_id = $%d", f.MovieID)
	}
	if f.SeriesID != "" {
		add("requester_series_id = $%d", f.SeriesID)
	}
	if f.EpisodeID != "" {
		add("requester_episode_id = $%d", f.EpisodeID)
	}
	if f.TMDBID > 0 {
		add("requester_tmdb_id = $%d", f.TMDBID)
	}
	if f.Season > 0 {
		add("requester_season = $%d", f.Season)
	}
	if f.Episode > 0 {
		add("requester_episode = $%d", f.Episode)
	}
	if !f.IncludeRemoved {
		clauses = append(clauses, "removed_at IS NULL")
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	// Order newest-first so callers showing "most recent download"
	// get it without sorting client-side.
	sql = fmt.Sprintf(`SELECT info_hash, name, save_path, category, `+
		`added_at, completed_at, removed_at, `+
		`requester_service, requester_movie_id, requester_series_id, `+
		`requester_episode_id, requester_tmdb_id, `+
		`requester_season, requester_episode `+
		`FROM torrents %s ORDER BY added_at DESC LIMIT %d`, where, limit)
	return sql, args
}

// LookupHistory returns torrent records matching the filter. Used by
// Pilot/Prism's history-aware UIs (library badges, manual-search
// guardrail, "downloaded but not in library" rail).
//
// Returns an empty slice (not an error) when nothing matches — callers
// should treat "no record" as "Haul has never seen this".
func (s *Session) LookupHistory(ctx context.Context, f HistoryFilter) ([]HistoryRecord, error) {
	if s.db == nil {
		return []HistoryRecord{}, nil
	}

	query, args := buildHistoryQuery(f)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("history query: %w", err)
	}
	defer rows.Close()

	out := make([]HistoryRecord, 0)
	for rows.Next() {
		var r HistoryRecord
		var addedAt time.Time
		var completedAt, removedAt *time.Time
		if err := rows.Scan(
			&r.InfoHash, &r.Name, &r.SavePath, &r.Category,
			&addedAt, &completedAt, &removedAt,
			&r.Requester, &r.MovieID, &r.SeriesID,
			&r.EpisodeID, &r.TMDBID,
			&r.Season, &r.Episode,
		); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}
		r.AddedAt = addedAt.UTC().Format(time.RFC3339)
		if completedAt != nil {
			r.CompletedAt = completedAt.UTC().Format(time.RFC3339)
		}
		if removedAt != nil {
			r.RemovedAt = removedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("history scan: %w", err)
	}
	return out, nil
}
