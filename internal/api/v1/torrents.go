package v1

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

// maxTorrentFileBytes caps the .torrent payload accepted via the data-URI
// upload path. Real .torrent files are typically <100KB; 10MB is generous
// even for huge multi-thousand-file torrents and stops accidental or
// malicious oversized uploads at the handler boundary.
const maxTorrentFileBytes = 10 * 1024 * 1024

const dataURITorrentPrefix = "data:application/x-bittorrent;base64,"

// validateAddTorrentURI checks the URI is one of the supported schemes
// before we hand it to Session.Add. The handler returns clean 400s for
// these so the user sees a precise error instead of a generic 422 from
// deeper in the engine.
func validateAddTorrentURI(uri string) error {
	if uri == "" {
		return huma.Error400BadRequest("either uri or .torrent file is required")
	}
	switch {
	case strings.HasPrefix(uri, "magnet:"):
		return nil
	case strings.HasPrefix(uri, "http://"), strings.HasPrefix(uri, "https://"):
		return nil
	case strings.HasPrefix(uri, dataURITorrentPrefix):
		b64 := uri[len(dataURITorrentPrefix):]
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return huma.Error400BadRequest("invalid base64 in .torrent payload: " + err.Error())
		}
		if len(decoded) == 0 {
			return huma.Error400BadRequest(".torrent payload is empty")
		}
		if len(decoded) > maxTorrentFileBytes {
			return huma.Error400BadRequest(".torrent payload exceeds size limit")
		}
		// Bencoded torrent files always start with 'd' (a dict). Reject
		// obvious non-torrents at the boundary.
		if decoded[0] != 'd' {
			return huma.Error400BadRequest(".torrent payload is not a bencoded torrent file")
		}
		return nil
	default:
		return huma.Error400BadRequest("unsupported uri scheme: must be magnet:, http(s)://, or data:application/x-bittorrent;base64,")
	}
}

// ── Types ────────────────────────────────────────────────────────────────────

type addTorrentInput struct {
	Body struct {
		URI      string                    `json:"uri"                          doc:"Magnet link or HTTP URL to .torrent file"`
		Category string                    `json:"category,omitempty"  required:"false" doc:"Category to assign"`
		SavePath string                    `json:"save_path,omitempty" required:"false" doc:"Override default save path"`
		Tags     []string                  `json:"tags,omitempty"      required:"false" doc:"Tags to assign"`
		Paused   bool                      `json:"paused,omitempty"    required:"false" doc:"Start in paused state"`
		Metadata *torrent.RequesterMetadata `json:"metadata,omitempty"  required:"false" doc:"Media context from requesting service"`
	}
}

type torrentOutput struct {
	Body *torrent.Info
}

type torrentListOutput struct {
	Body []torrent.Info
}

type getTorrentInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
}

type deleteTorrentInput struct {
	Hash        string `path:"hash"         doc:"Torrent info hash"`
	DeleteFiles bool   `query:"delete_files" default:"false" doc:"Also delete downloaded files"`
}

type controlTorrentInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
}

type peersOutput struct {
	Body struct {
		Peers []torrent.PeerInfo `json:"peers"`
	}
}

type piecesOutput struct {
	Body *torrent.PiecesInfo
}

type trackersOutput struct {
	Body struct {
		Trackers []torrent.TrackerInfo `json:"trackers"`
	}
}

type swarmOutput struct {
	Body *torrent.SwarmInfo
}

type emptyOutput struct {
	Body struct{}
}

type addTrackersInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		URLs []string `json:"urls"           doc:"One or more announce URLs"`
		Tier int      `json:"tier,omitempty" doc:"Tier index (0 = highest priority, default 0)"`
	}
}

type removeTrackerInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	URL  string `query:"url" doc:"Tracker URL to remove"`
}

// ── Registration ─────────────────────────────────────────────────────────────

// RegisterTorrentRoutes registers the /api/v1/torrents endpoints.
func RegisterTorrentRoutes(api huma.API, session *torrent.Session) {
	// List all torrents
	huma.Register(api, huma.Operation{
		OperationID: "list-torrents",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents",
		Summary:     "List all torrents",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, _ *struct{}) (*torrentListOutput, error) {
		return &torrentListOutput{Body: session.List()}, nil
	})

	// Get a single torrent
	huma.Register(api, huma.Operation{
		OperationID: "get-torrent",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}",
		Summary:     "Get torrent details",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *getTorrentInput) (*torrentOutput, error) {
		info, err := session.Get(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &torrentOutput{Body: info}, nil
	})

	// Add a torrent
	huma.Register(api, huma.Operation{
		OperationID: "add-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents",
		Summary:     "Add a torrent",
		Tags:        []string{"Torrents"},
		// Lift Huma's 1 MB default so legitimate multi-MB .torrent files
		// (huge multi-file torrents — Linux distros, datasets, …)
		// reach the handler. The decoded payload size is enforced
		// separately by validateAddTorrentURI / maxTorrentFileBytes; this
		// limit is a transport-level guard, sized to comfortably hold a
		// 10 MB torrent after base64+JSON expansion.
		MaxBodyBytes: 16 * 1024 * 1024,
	}, func(ctx context.Context, input *addTorrentInput) (*torrentOutput, error) {
		if err := validateAddTorrentURI(input.Body.URI); err != nil {
			return nil, err
		}
		info, err := session.Add(ctx, torrent.AddRequest{
			URI:      input.Body.URI,
			Category: input.Body.Category,
			SavePath: input.Body.SavePath,
			Tags:     input.Body.Tags,
			Paused:   input.Body.Paused,
			Metadata: input.Body.Metadata,
		})
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return &torrentOutput{Body: info}, nil
	})

	// Delete a torrent
	huma.Register(api, huma.Operation{
		OperationID: "delete-torrent",
		Method:      http.MethodDelete,
		Path:        "/api/v1/torrents/{hash}",
		Summary:     "Remove a torrent",
		Tags:        []string{"Torrents"},
	}, func(ctx context.Context, input *deleteTorrentInput) (*emptyOutput, error) {
		if err := session.Remove(ctx, input.Hash, input.DeleteFiles); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Pause a torrent
	huma.Register(api, huma.Operation{
		OperationID: "pause-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/pause",
		Summary:     "Pause a torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*emptyOutput, error) {
		if err := session.Pause(input.Hash); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Resume a torrent
	huma.Register(api, huma.Operation{
		OperationID: "resume-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/resume",
		Summary:     "Resume a paused torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*emptyOutput, error) {
		if err := session.Resume(input.Hash); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// List connected peers for a torrent.
	huma.Register(api, huma.Operation{
		OperationID: "list-torrent-peers",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/peers",
		Summary:     "List connected peers for a torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *getTorrentInput) (*peersOutput, error) {
		peers, err := session.Peers(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		out := &peersOutput{}
		out.Body.Peers = peers
		return out, nil
	})

	// Get piece-state snapshot for a torrent.
	huma.Register(api, huma.Operation{
		OperationID: "get-torrent-pieces",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/pieces",
		Summary:     "Piece-state snapshot (run-length encoded)",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *getTorrentInput) (*piecesOutput, error) {
		pieces, err := session.Pieces(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		// Pieces returns (nil, nil) when metadata isn't ready — represent
		// that as an empty PiecesInfo so the frontend's type stays simple.
		if pieces == nil {
			pieces = &torrent.PiecesInfo{}
		}
		return &piecesOutput{Body: pieces}, nil
	})

	// List configured trackers for a torrent.
	huma.Register(api, huma.Operation{
		OperationID: "list-torrent-trackers",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/trackers",
		Summary:     "Configured tracker list (no live announce status)",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *getTorrentInput) (*trackersOutput, error) {
		trackers, err := session.Trackers(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		out := &trackersOutput{}
		out.Body.Trackers = trackers
		return out, nil
	})

	// Add tracker URLs to a torrent. Operators paste from "edit
	// trackers" textbox; we accept newline-separated URLs and ignore
	// duplicates inside the engine.
	huma.Register(api, huma.Operation{
		OperationID: "add-torrent-trackers",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/trackers",
		Summary:     "Add tracker URLs to a torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *addTrackersInput) (*emptyOutput, error) {
		if err := session.AddTrackers(input.Hash, input.Body.URLs, input.Body.Tier); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Remove a single tracker URL. Path-style instead of body so curl
	// clients can hit it without crafting JSON; URL-encode the URL when
	// it contains query strings (most do).
	huma.Register(api, huma.Operation{
		OperationID: "remove-torrent-tracker",
		Method:      http.MethodDelete,
		Path:        "/api/v1/torrents/{hash}/trackers",
		Summary:     "Remove a tracker URL from a torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *removeTrackerInput) (*emptyOutput, error) {
		if err := session.RemoveTracker(input.Hash, input.URL); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Swarm gauges — diagnoses "tracker says N seeders but we connected
	// to far fewer" by exposing TotalPeers / PendingPeers / HalfOpenPeers
	// alongside ActivePeers.
	huma.Register(api, huma.Operation{
		OperationID: "get-torrent-swarm",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/swarm",
		Summary:     "Swarm-level peer gauges (known / pending / dialing / connected)",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *getTorrentInput) (*swarmOutput, error) {
		swarm, err := session.Swarm(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &swarmOutput{Body: swarm}, nil
	})
}
