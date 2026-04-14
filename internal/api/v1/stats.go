package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/version"
)

type statsBody struct {
	Version         string `json:"version"`
	TotalTorrents   int    `json:"total_torrents"`
	ActiveDownloads int    `json:"active_downloads"`
	ActiveUploads   int    `json:"active_uploads"`
	TotalDownloaded int64  `json:"total_downloaded"`
	TotalUploaded   int64  `json:"total_uploaded"`
	DownloadSpeed   int64  `json:"download_speed"`
	UploadSpeed     int64  `json:"upload_speed"`
	PeersConnected  int    `json:"peers_connected"`
	SeedsConnected  int    `json:"seeds_connected"`
	AltSpeedActive  bool   `json:"alt_speed_active"`
	ArchivedCount   int    `json:"archived_count"`
}

type statsOutput struct {
	Body *statsBody
}

type appVersionBody struct {
	Version string `json:"version"`
	AppName string `json:"app_name"`
}

type appVersionOutput struct {
	Body *appVersionBody
}

// RegisterStatsRoutes registers stats and app info endpoints.
func RegisterStatsRoutes(api huma.API, session *torrent.Session) {
	huma.Register(api, huma.Operation{
		OperationID: "get-stats",
		Method:      http.MethodGet,
		Path:        "/api/v1/stats",
		Summary:     "Session statistics",
		Tags:        []string{"Stats"},
	}, func(_ context.Context, _ *struct{}) (*statsOutput, error) {
		ts := session.GetTransferStats()
		return &statsOutput{Body: &statsBody{
			Version:         version.Version,
			TotalTorrents:   ts.TotalTorrents,
			ActiveDownloads: ts.ActiveDownloads,
			ActiveUploads:   ts.ActiveUploads,
			TotalDownloaded: ts.TotalDownloaded,
			TotalUploaded:   ts.TotalUploaded,
			DownloadSpeed:   ts.DownloadSpeed,
			UploadSpeed:     ts.UploadSpeed,
			PeersConnected:  ts.TotalPeers,
			SeedsConnected:  ts.TotalSeeds,
			AltSpeedActive:  session.IsAltSpeedActive(),
		ArchivedCount:   session.GetArchivedCount(),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-version",
		Method:      http.MethodGet,
		Path:        "/api/v1/app/version",
		Summary:     "Application version",
		Tags:        []string{"App"},
	}, func(_ context.Context, _ *struct{}) (*appVersionOutput, error) {
		return &appVersionOutput{Body: &appVersionBody{
			Version: version.Version,
			AppName: version.AppName,
		}}, nil
	})
}
