package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

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
	}, func(ctx context.Context, input *addTorrentInput) (*torrentOutput, error) {
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
