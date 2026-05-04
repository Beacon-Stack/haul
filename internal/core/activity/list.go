package activity

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Item is a single row in the activity list. One row per torrent,
// including those that have been completed or removed — the table is
// the full history.
type Item struct {
	InfoHash    string `json:"info_hash"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	SavePath    string `json:"save_path"`
	SizeBytes   int64  `json:"size_bytes"`
	Resolution  string `json:"resolution"`
	AddedAt     string `json:"added_at"`               // RFC3339
	CompletedAt string `json:"completed_at,omitempty"` // empty if still in progress
	RemovedAt   string `json:"removed_at,omitempty"`   // empty if still active

	Requester string `json:"requester,omitempty"`
	MovieID   string `json:"movie_id,omitempty"`
	SeriesID  string `json:"series_id,omitempty"`
	EpisodeID string `json:"episode_id,omitempty"`
	TMDBID    int    `json:"tmdb_id,omitempty"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
}

// ListFilter scopes a List query.
type ListFilter struct {
	Search string // free-text matched against name + category (ILIKE)
	Status string // "active" | "completed" | "removed" | "all" (default "all")
	Sort   string // "added_at" (default) | "completed_at" | "removed_at" | "size_bytes" | "resolution" | "name"
	Order  string // "asc" | "desc" (default "desc")
	Limit  int    // default 50, cap 200
	Offset int
}

// validSortColumns is the allowlist for ORDER BY. Anything not listed
// here falls back to added_at — required because we splice the column
// name into the SQL, so user input MUST be validated.
var validSortColumns = map[string]string{
	"added_at":     "added_at",
	"completed_at": "completed_at",
	"removed_at":   "removed_at",
	"size_bytes":   "size_bytes",
	"resolution":   "resolution",
	"name":         "name",
}

// buildListQuery composes the SELECT + COUNT for an activity list.
// Returns the list query, the count query, and the shared args slice.
// Pure logic, no I/O — split out so the unit test can pin the SQL
// shape without a postgres fixture.
func buildListQuery(f ListFilter) (listSQL, countSQL string, args []any) {
	var clauses []string
	add := func(clause string, arg any) {
		args = append(args, arg)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}

	// Search across the fields the user can see in the table. ILIKE is
	// fine here — the torrents table is small enough that a sequential
	// scan with a substring match is sub-millisecond.
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		idx := len(args)
		clauses = append(clauses, fmt.Sprintf("(name ILIKE $%d OR category ILIKE $%d)", idx, idx))
	}

	switch f.Status {
	case "active":
		clauses = append(clauses, "removed_at IS NULL AND completed_at IS NULL")
	case "completed":
		clauses = append(clauses, "completed_at IS NOT NULL AND removed_at IS NULL")
	case "removed":
		clauses = append(clauses, "removed_at IS NOT NULL")
	case "", "all":
		// no filter — show everything
	default:
		add("removed_at IS NULL AND $%d = $%d", f.Status) // unreachable; default is "all"
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	sortCol, ok := validSortColumns[f.Sort]
	if !ok {
		sortCol = "added_at"
	}
	// Resolution is stored as a string ("1080p", "2160p", …) so a
	// plain ORDER BY would sort it alphabetically — 1080p, 2160p,
	// 480p, 720p — which is meaningless to the user. Map to an
	// ordinal so "ascending" really does mean "lower quality first."
	if sortCol == "resolution" {
		sortCol = `CASE resolution WHEN '480p' THEN 1 WHEN '720p' THEN 2 WHEN '1080p' THEN 3 WHEN '2160p' THEN 4 ELSE 0 END`
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	// NULLS LAST keeps unsorted rows (e.g. resolution="" for unparsable
	// names) at the bottom regardless of asc/desc, which matches user
	// expectation for "sort by resolution."
	listSQL = fmt.Sprintf(`
		SELECT info_hash, name, category, save_path, size_bytes, resolution,
			   added_at, completed_at, removed_at,
			   requester_service, requester_movie_id, requester_series_id,
			   requester_episode_id, requester_tmdb_id, requester_season, requester_episode
		FROM torrents
		%s
		ORDER BY %s %s NULLS LAST, info_hash %s
		LIMIT %d OFFSET %d`,
		where, sortCol, order, order, limit, offset,
	)
	countSQL = fmt.Sprintf(`SELECT COUNT(*) FROM torrents %s`, where)
	return listSQL, countSQL, args
}

// List runs the activity list query and returns rows + total match
// count. Returns an empty slice (not an error) when nothing matches.
func List(ctx context.Context, db *sql.DB, f ListFilter) ([]Item, int64, error) {
	if db == nil {
		return []Item{}, 0, nil
	}
	listSQL, countSQL, args := buildListQuery(f)

	var total int64
	if err := db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("activity count: %w", err)
	}

	rows, err := db.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("activity list: %w", err)
	}
	defer rows.Close()

	out := make([]Item, 0)
	for rows.Next() {
		var it Item
		var addedAt time.Time
		var completedAt, removedAt *time.Time
		if err := rows.Scan(
			&it.InfoHash, &it.Name, &it.Category, &it.SavePath, &it.SizeBytes, &it.Resolution,
			&addedAt, &completedAt, &removedAt,
			&it.Requester, &it.MovieID, &it.SeriesID,
			&it.EpisodeID, &it.TMDBID, &it.Season, &it.Episode,
		); err != nil {
			return nil, 0, fmt.Errorf("activity scan: %w", err)
		}
		it.AddedAt = addedAt.UTC().Format(time.RFC3339)
		if completedAt != nil {
			it.CompletedAt = completedAt.UTC().Format(time.RFC3339)
		}
		if removedAt != nil {
			it.RemovedAt = removedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("activity rows: %w", err)
	}
	return out, total, nil
}

// EventRow is a single row from the per-torrent event timeline. The
// payload column is delivered as raw JSON so the frontend can render
// whatever shape the producer emitted without us needing to model
// every event type.
type EventRow struct {
	ID         int64  `json:"id"`
	InfoHash   string `json:"info_hash"`
	EventType  string `json:"event_type"`
	OccurredAt string `json:"occurred_at"` // RFC3339
	Payload    any    `json:"payload"`
}

// ListEvents returns the event timeline for one torrent, newest first.
func ListEvents(ctx context.Context, db *sql.DB, infoHash string, limit int) ([]EventRow, error) {
	if db == nil {
		return []EventRow{}, nil
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, info_hash, event_type, occurred_at, payload
		FROM torrent_events
		WHERE info_hash = $1
		ORDER BY occurred_at DESC, id DESC
		LIMIT $2`, infoHash, limit)
	if err != nil {
		return nil, fmt.Errorf("event list: %w", err)
	}
	defer rows.Close()

	out := make([]EventRow, 0)
	for rows.Next() {
		var r EventRow
		var occurred time.Time
		var payload []byte
		if err := rows.Scan(&r.ID, &r.InfoHash, &r.EventType, &occurred, &payload); err != nil {
			return nil, fmt.Errorf("event scan: %w", err)
		}
		r.OccurredAt = occurred.UTC().Format(time.RFC3339)
		// Parse the JSONB payload back into a generic map so it ships
		// to the client as a real object, not a base64 blob. Failure
		// here means the column got corrupted somehow — return the raw
		// string so the caller can at least see what's there.
		if len(payload) > 0 {
			var v any
			if err := unmarshalPayload(payload, &v); err == nil {
				r.Payload = v
			} else {
				r.Payload = string(payload)
			}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event rows: %w", err)
	}
	return out, nil
}
