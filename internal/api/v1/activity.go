package v1

// activity.go — Activity page API.
//
// The Activity page is the user-facing audit trail for everything Haul
// has downloaded. The list endpoint returns one row per torrent
// (active + completed + removed), with search / sort / pagination
// suited to a desktop table. The detail endpoint returns the per-torrent
// event timeline so a row click can render the lifecycle (added →
// metadata → started → completed → …).
//
// This is distinct from /api/v1/history which exists for arr-side
// integrations (filter by tmdb_id / episode_id, no pagination, no
// sort). Keeping them separate lets us evolve the UI shape without
// risking the inter-service contract Pilot/Prism rely on.

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/activity"
)

type activityListInput struct {
	Q      string `query:"q"      doc:"Free-text search across torrent name + category"`
	Status string `query:"status" doc:"\"active\" | \"completed\" | \"removed\" | \"all\" (default \"all\")"`
	Sort   string `query:"sort"   doc:"Sort column: added_at | completed_at | removed_at | size_bytes | resolution | name"`
	Order  string `query:"order"  doc:"\"asc\" or \"desc\" (default \"desc\")"`
	Limit  int    `query:"limit"  doc:"Page size (default 50, max 200)"`
	Offset int    `query:"offset" doc:"Pagination offset"`
}

type activityListOutput struct {
	Body struct {
		Items []activity.Item `json:"items"`
		Total int64           `json:"total"`
	}
}

type activityEventsInput struct {
	Hash  string `path:"hash"  doc:"Torrent info_hash"`
	Limit int    `query:"limit" doc:"Max events (default 200, max 1000)"`
}

type activityEventsOutput struct {
	Body struct {
		Events []activity.EventRow `json:"events"`
	}
}

// RegisterActivityRoutes wires the activity-page endpoints onto the
// API. Reads from torrents + torrent_events; never mutates state.
func RegisterActivityRoutes(api huma.API, db *sql.DB) {
	huma.Register(api, huma.Operation{
		OperationID: "list-activity",
		Method:      http.MethodGet,
		Path:        "/api/v1/activity",
		Summary:     "List torrents with full history (search, sort, paginate)",
		Description: "Returns one row per torrent (including completed/removed) for the Activity page table.",
		Tags:        []string{"Activity"},
	}, func(ctx context.Context, input *activityListInput) (*activityListOutput, error) {
		items, total, err := activity.List(ctx, db, activity.ListFilter{
			Search: input.Q,
			Status: input.Status,
			Sort:   input.Sort,
			Order:  input.Order,
			Limit:  input.Limit,
			Offset: input.Offset,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &activityListOutput{}
		out.Body.Items = items
		out.Body.Total = total
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-activity-events",
		Method:      http.MethodGet,
		Path:        "/api/v1/activity/{hash}/events",
		Summary:     "Per-torrent event timeline",
		Description: "Returns the lifecycle event log (added/completed/failed/stalled/state_changed/removed) for one torrent, newest first.",
		Tags:        []string{"Activity"},
	}, func(ctx context.Context, input *activityEventsInput) (*activityEventsOutput, error) {
		events, err := activity.ListEvents(ctx, db, input.Hash, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &activityEventsOutput{}
		out.Body.Events = events
		return out, nil
	})
}
