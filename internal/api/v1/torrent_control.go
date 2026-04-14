package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

type setCategoryInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		Category string `json:"category" doc:"Category name"`
	}
}

type modifyTagsInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		Tags []string `json:"tags" doc:"Tag names"`
	}
}

type setSpeedLimitsInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		DownloadLimit int `json:"download_limit" required:"false" doc:"Download speed limit in bytes/s (0=unlimited)"`
		UploadLimit   int `json:"upload_limit"   required:"false" doc:"Upload speed limit in bytes/s (0=unlimited)"`
	}
}

type setSeedLimitsInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		RatioLimit    float64 `json:"ratio_limit"     required:"false" doc:"Seed ratio limit (0=unlimited)"`
		TimeLimitSecs int     `json:"time_limit_secs" required:"false" doc:"Seed time limit in seconds (0=unlimited)"`
	}
}

type setPriorityInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		Priority int `json:"priority" doc:"Queue priority (lower=higher priority)"`
	}
}

type setSequentialInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		Sequential bool `json:"sequential" doc:"Enable sequential download"`
	}
}

type setLocationInput struct {
	Hash string `path:"hash" doc:"Torrent info hash"`
	Body struct {
		Path string `json:"path" doc:"New save path"`
	}
}

type fileListOutput struct {
	Body []torrent.FileInfo
}

type setFilePriorityInput struct {
	Hash  string `path:"hash"  doc:"Torrent info hash"`
	Index int    `path:"index" doc:"File index"`
	Body  struct {
		Priority string `json:"priority" doc:"File priority: skip, normal, or high"`
	}
}

// RegisterTorrentControlRoutes registers extended torrent control endpoints.
func RegisterTorrentControlRoutes(api huma.API, session *torrent.Session) {
	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-category",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/category",
		Summary:     "Set torrent category",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setCategoryInput) (*emptyOutput, error) {
		if err := session.SetCategory(input.Hash, input.Body.Category); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "add-torrent-tags",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/tags",
		Summary:     "Add tags to a torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *modifyTagsInput) (*emptyOutput, error) {
		if err := session.AddTags(input.Hash, input.Body.Tags); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "remove-torrent-tags",
		Method:      http.MethodDelete,
		Path:        "/api/v1/torrents/{hash}/tags",
		Summary:     "Remove tags from a torrent",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *modifyTagsInput) (*emptyOutput, error) {
		if err := session.RemoveTags(input.Hash, input.Body.Tags); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-speed-limits",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/speed-limits",
		Summary:     "Set per-torrent speed limits",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setSpeedLimitsInput) (*emptyOutput, error) {
		if err := session.SetSpeedLimits(input.Hash, input.Body.DownloadLimit, input.Body.UploadLimit); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-seed-limits",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/seed-limits",
		Summary:     "Set per-torrent seed ratio and time limits",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setSeedLimitsInput) (*emptyOutput, error) {
		if err := session.SetSeedLimits(input.Hash, input.Body.RatioLimit, input.Body.TimeLimitSecs); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-priority",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/priority",
		Summary:     "Set torrent queue priority",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setPriorityInput) (*emptyOutput, error) {
		if err := session.SetPriority(input.Hash, input.Body.Priority); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-sequential",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/sequential",
		Summary:     "Toggle sequential download",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setSequentialInput) (*emptyOutput, error) {
		if err := session.SetSequential(input.Hash, input.Body.Sequential); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-torrent-location",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/location",
		Summary:     "Move torrent data to a new path",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setLocationInput) (*emptyOutput, error) {
		if err := session.SetLocation(input.Hash, input.Body.Path); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-torrent-files",
		Method:      http.MethodGet,
		Path:        "/api/v1/torrents/{hash}/files",
		Summary:     "Get torrent file list",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*fileListOutput, error) {
		files, err := session.GetFiles(input.Hash)
		if err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &fileListOutput{Body: files}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-file-priority",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/{hash}/files/{index}/priority",
		Summary:     "Set file download priority",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *setFilePriorityInput) (*emptyOutput, error) {
		if err := session.SetFilePriority(input.Hash, input.Index, input.Body.Priority); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "recheck-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/recheck",
		Summary:     "Re-verify torrent data integrity",
		Tags:        []string{"Torrents"},
	}, func(ctx context.Context, input *controlTorrentInput) (*emptyOutput, error) {
		if err := session.Recheck(ctx, input.Hash); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Bulk reorder — set priority for multiple torrents at once
	huma.Register(api, huma.Operation{
		OperationID: "reorder-torrents",
		Method:      http.MethodPut,
		Path:        "/api/v1/torrents/reorder",
		Summary:     "Set priority order for torrents",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *struct {
		Body struct {
			Order []string `json:"order" doc:"Info hashes in desired priority order"`
		}
	}) (*emptyOutput, error) {
		for i, hash := range input.Body.Order {
			_ = session.SetPriority(hash, i)
		}
		return &emptyOutput{}, nil
	})

	// Force start — bypass queue
	huma.Register(api, huma.Operation{
		OperationID: "force-start-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/force-start",
		Summary:     "Force start a torrent (bypass queue limits)",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*emptyOutput, error) {
		if err := session.ForceStart(input.Hash); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Reannounce — force tracker announce
	huma.Register(api, huma.Operation{
		OperationID: "reannounce-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/reannounce",
		Summary:     "Force reannounce to trackers",
		Tags:        []string{"Torrents"},
	}, func(ctx context.Context, input *controlTorrentInput) (*emptyOutput, error) {
		if err := session.Reannounce(ctx, input.Hash); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// First/last piece priority
	huma.Register(api, huma.Operation{
		OperationID: "set-first-last-priority",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/first-last-priority",
		Summary:     "Prioritize first and last pieces for preview/streaming",
		Tags:        []string{"Torrents"},
	}, func(_ context.Context, input *controlTorrentInput) (*emptyOutput, error) {
		if err := session.SetFirstLastPriority(input.Hash, true); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})

	// Alt speed toggle
	huma.Register(api, huma.Operation{
		OperationID: "toggle-alt-speed",
		Method:      http.MethodPost,
		Path:        "/api/v1/speed/alt/toggle",
		Summary:     "Toggle alternative speed limits on/off",
		Tags:        []string{"Speed"},
	}, func(_ context.Context, _ *struct{}) (*struct {
		Body struct {
			AltSpeedActive bool `json:"alt_speed_active"`
		}
	}, error) {
		current := session.IsAltSpeedActive()
		session.SetAltSpeedEnabled(!current)
		return &struct {
			Body struct {
				AltSpeedActive bool `json:"alt_speed_active"`
			}
		}{Body: struct {
			AltSpeedActive bool `json:"alt_speed_active"`
		}{AltSpeedActive: !current}}, nil
	})

	// Clear archived — remove all torrents in the "archived" category
	huma.Register(api, huma.Operation{
		OperationID: "clear-archived",
		Method:      http.MethodDelete,
		Path:        "/api/v1/torrents/archived",
		Summary:     "Remove all archived (stalled) torrents",
		Tags:        []string{"Torrents"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Removed int `json:"removed"`
		}
	}, error) {
		removed := session.ClearArchived(ctx)
		return &struct {
			Body struct {
				Removed int `json:"removed"`
			}
		}{Body: struct {
			Removed int `json:"removed"`
		}{Removed: removed}}, nil
	})
}
