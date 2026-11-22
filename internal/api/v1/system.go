package v1

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/version"
)

// systemStatusBody is the same shape as /api/v1/system/status in the
// other Beacon apps, so the shared System page can read any of them.
type systemStatusBody struct {
	AppName       string `json:"app_name"               doc:"Application name"`
	Version       string `json:"version"                doc:"Build version"`
	BuildTime     string `json:"build_time"             doc:"UTC build timestamp"`
	GoVersion     string `json:"go_version"             doc:"Go runtime version"`
	DBType        string `json:"db_type"                doc:"Active database driver"`
	DBPath        string `json:"db_path,omitempty"      doc:"SQLite database file path"`
	UptimeSeconds int64  `json:"uptime_seconds"         doc:"Seconds since startup"`
	StartTime     string `json:"start_time"             doc:"UTC server start time"`
}

// RegisterSystemRoutes registers GET /api/v1/system/status.
func RegisterSystemRoutes(api huma.API, startTime time.Time, dbPath string) {
	huma.Register(api, huma.Operation{
		OperationID: "get-system-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/status",
		Summary:     "Get system status",
		Description: "Returns runtime information about the server.",
		Tags:        []string{"System"},
	}, func(_ context.Context, _ *struct{}) (*struct{ Body systemStatusBody }, error) {
		return &struct{ Body systemStatusBody }{Body: systemStatusBody{
			AppName:       "Haul",
			Version:       version.Version,
			BuildTime:     version.BuildTime,
			GoVersion:     runtime.Version(),
			DBType:        "sqlite",
			DBPath:        dbPath,
			UptimeSeconds: int64(time.Since(startTime).Seconds()),
			StartTime:     startTime.UTC().Format(time.RFC3339),
		}}, nil
	})
}
