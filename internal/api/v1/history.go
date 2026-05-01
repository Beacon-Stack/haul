package v1

// history.go — historical-download lookup API for arr-side
// integrations.
//
// Pilot/Prism call these endpoints to answer "have I downloaded this
// movie/episode before?" without polling Haul's live torrent state.
// The endpoints return both active and previously-removed records
// (callers can distinguish via the `removed_at` field).
//
// Three query modes:
//   - GET /api/v1/history?service=pilot&tmdb_id=X&season=Y&episode=Z
//     — semantic match by content, regardless of release
//   - GET /api/v1/history/by-hash/:hash
//     — exact match by info_hash, fast point lookup
//   - GET /api/v1/history?service=pilot&episode_id=X
//     — by arr's UUID; used by per-row library badges

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

type historyListInput struct {
	Service        string `query:"service"        doc:"Requesting service: \"pilot\", \"prism\", \"manual\". Required when filtering by arr-side IDs."`
	InfoHash       string `query:"info_hash"      doc:"Exact info_hash match"`
	MovieID        string `query:"movie_id"       doc:"Arr-side movie UUID (Prism)"`
	SeriesID       string `query:"series_id"      doc:"Arr-side series UUID (Pilot)"`
	EpisodeID      string `query:"episode_id"     doc:"Arr-side episode UUID (Pilot)"`
	TMDBID         int    `query:"tmdb_id"        doc:"TMDB id for semantic match"`
	Season         int    `query:"season"         doc:"Season number (must combine with tmdb_id)"`
	Episode        int    `query:"episode"        doc:"Episode number (must combine with tmdb_id+season)"`
	IncludeRemoved bool   `query:"include_removed" default:"false" doc:"Include records whose removed_at is set"`
	Limit          int    `query:"limit"          default:"100"   doc:"Max results (default 100)"`
}

type historyByHashInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
}

type historyListOutput struct {
	Body struct {
		Items []torrent.HistoryRecord `json:"items"`
	}
}

type historyRecordOutput struct {
	Body *torrent.HistoryRecord
}

// RegisterHistoryRoutes wires the lookup endpoints onto the API. Mounts
// alongside the live torrent routes; both are admin-key gated by the
// surrounding middleware.
func RegisterHistoryRoutes(api huma.API, session *torrent.Session) {
	huma.Register(api, huma.Operation{
		OperationID: "list-history",
		Method:      http.MethodGet,
		Path:        "/api/v1/history",
		Summary:     "List historical torrent records",
		Description: "Returns torrent records (active + previously-removed) matching the filter. Used by Pilot/Prism to detect already-downloaded content.",
		Tags:        []string{"History"},
	}, func(ctx context.Context, input *historyListInput) (*historyListOutput, error) {
		records, err := session.LookupHistory(ctx, torrent.HistoryFilter{
			Service:        input.Service,
			InfoHash:       input.InfoHash,
			MovieID:        input.MovieID,
			SeriesID:       input.SeriesID,
			EpisodeID:      input.EpisodeID,
			TMDBID:         input.TMDBID,
			Season:         input.Season,
			Episode:        input.Episode,
			IncludeRemoved: input.IncludeRemoved,
			Limit:          input.Limit,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &historyListOutput{}
		out.Body.Items = records
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-history-by-hash",
		Method:      http.MethodGet,
		Path:        "/api/v1/history/by-hash/{hash}",
		Summary:     "Get a history record by info_hash",
		Description: "Fast point lookup. Returns 404 when the hash has never been seen by Haul.",
		Tags:        []string{"History"},
	}, func(ctx context.Context, input *historyByHashInput) (*historyRecordOutput, error) {
		// IncludeRemoved=true so manual-search guardrails see "you
		// downloaded this and removed it" — the user might still
		// want the warning.
		records, err := session.LookupHistory(ctx, torrent.HistoryFilter{
			InfoHash:       input.Hash,
			IncludeRemoved: true,
			Limit:          1,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if len(records) == 0 {
			return nil, huma.Error404NotFound("no history record for that info_hash")
		}
		return &historyRecordOutput{Body: &records[0]}, nil
	})
}
