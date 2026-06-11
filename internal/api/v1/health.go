package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

type healthOutput struct {
	Body *torrent.HealthReport
}

type setMetadataInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body torrent.RequesterMetadata
}

type metadataOutput struct {
	Body *torrent.RequesterMetadata
}

type stallOutput struct {
	Body *torrent.StallInfo
}

type stalledListOutput struct {
	Body []torrent.StalledTorrent
}

// RegisterHealthRoutes registers health and Beacon integration endpoints.
func RegisterHealthRoutes(api huma.API, session *torrent.Session) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/api/v1/health",
		Summary:     "Detailed health report",
		Tags:        []string{"Health"},
	}, func(_ context.Context, _ *struct{}) (*healthOutput, error) {
		return &healthOutput{Body: session.GetHealth()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-metadata",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/metadata",
		Summary:     "Set requester metadata (Beacon integration)",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setMetadataInput) (*emptyOutput, error) {
		if err := session.SetMetadata(input.Hash, input.Body); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-torrent-metadata",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/metadata",
		Summary:     "Get requester metadata",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*metadataOutput, error) {
		meta, err := session.GetMetadata(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &metadataOutput{Body: meta}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-torrent-stall",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/stall",
		Summary:     "Get stall detection info",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*stallOutput, error) {
		info, err := session.GetStallInfo(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &stallOutput{Body: info}, nil
	})

	// GET /api/v1/stalls — bulk stall status for all torrents. Returns only
	// those currently classified as stalled. Called by Pilot's stallwatcher
	// every 60s to correlate stalls with grab_history and populate the
	// release blocklist.
	huma.Register(api, huma.Operation{
		OperationID: "list-stalled-torrents",
		Method:      http.MethodGet,
		Path:        "/api/v1/stalls",
		Summary:     "List currently stalled torrents (bulk)",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, _ *struct{}) (*stalledListOutput, error) {
		return &stalledListOutput{Body: session.ListStalled()}, nil
	})
}
