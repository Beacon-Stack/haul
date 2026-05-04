package api

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/beacon-stack/haul/internal/api/middleware"
	v1 "github.com/beacon-stack/haul/internal/api/v1"
	"github.com/beacon-stack/haul/internal/api/ws"
	"github.com/beacon-stack/haul/internal/core/category"
	"github.com/beacon-stack/haul/internal/core/tag"
	"github.com/beacon-stack/haul/internal/core/torrent"
	adminpkg "github.com/beacon-stack/haul/internal/db/admin"
	"github.com/beacon-stack/haul/internal/pulse"
	"github.com/beacon-stack/haul/internal/version"
	beaconlog "github.com/beacon-stack/pulse/pkg/log"
)

// sanitizeFilename strips characters that would confuse filesystems or
// Content-Disposition parsers. We don't try for full RFC 5987 — just
// drop the obvious offenders.
func sanitizeFilename(s string) string {
	r := strings.NewReplacer(
		`"`, "_",
		`\`, "_",
		"/", "_",
		"\x00", "",
		"\n", " ",
		"\r", " ",
	)
	return r.Replace(s)
}

// RouterConfig holds everything the router needs.
type RouterConfig struct {
	Logger     *slog.Logger
	Session    *torrent.Session
	WSHub      *ws.Hub
	Categories *category.Service
	Tags       *tag.Service
	DB         *sql.DB
	// Admin gates the diagnostics + cleanup-history endpoints. When nil
	// or DiagnosticsEnabled=false the routes are not registered at all
	// (404 on every /api/v1/admin/* path).
	Admin *AdminGate
	// Pulse is the optional integration used to discover sibling
	// services for the Activity page deep-links. Nil when Haul is
	// running standalone — the peers endpoint then returns an empty map.
	Pulse *pulse.Integration
	// LogSystem + DockerLogs come from pulse/pkg/log. When LogSystem
	// is non-nil, /api/v1/system/{logs,log-level} are registered.
	// When DockerLogs is also non-nil, /api/v1/system/logs/docker
	// is registered for full-history access.
	LogSystem  *beaconlog.System
	DockerLogs *beaconlog.DockerLogsReader
}

// AdminGate is the runtime knob for the admin-only endpoints. Held by
// RouterConfig so routes can choose to skip registration when disabled.
type AdminGate struct {
	DiagnosticsEnabled bool
	Registry           *adminpkg.Registry
}

// NewRouter builds and returns the application HTTP handler.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.MaxRequestBodySize(50 << 20)) // 50 MiB for .torrent uploads
	r.Use(middleware.RequestLogger(cfg.Logger))
	r.Use(middleware.Recovery(cfg.Logger))

	// WebSocket
	if cfg.WSHub != nil {
		r.Get("/api/v1/ws", cfg.WSHub.ServeHTTP)
	}

	// .torrent file export. Registered as a chi route because Huma
	// shapes responses as JSON; we want to stream raw BYTEA bytes with
	// a Content-Disposition header so a browser fetch can save-as.
	r.Get("/api/v1/torrents/{hash}/torrent_file", func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "hash")
		data, err := cfg.Session.ExportTorrent(hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Try to get the torrent name for the filename header. Falls
		// back to the hash if metadata isn't available.
		name := hash
		if info, err := cfg.Session.Get(hash); err == nil && info.Name != "" {
			name = info.Name
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(name)+`.torrent"`)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		_, _ = w.Write(data)
	})

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Huma API
	humaConfig := huma.DefaultConfig(version.AppName+" API", version.Version)
	humaConfig.DocsPath = "/api/docs"
	humaConfig.OpenAPIPath = "/api/openapi"
	humaAPI := humachi.New(r, humaConfig)

	v1.RegisterTorrentRoutes(humaAPI, cfg.Session)
	v1.RegisterTorrentControlRoutes(humaAPI, cfg.Session)
	v1.RegisterHistoryRoutes(humaAPI, cfg.Session)
	v1.RegisterActivityRoutes(humaAPI, cfg.DB)
	v1.RegisterPeerRoutes(humaAPI, cfg.Pulse)
	v1.RegisterResearchRoutes(humaAPI, cfg.Session, cfg.Pulse)

	if cfg.LogSystem != nil {
		beaconlog.RegisterRoutesWithDocker(humaAPI, cfg.LogSystem, cfg.DockerLogs)
	}
	v1.RegisterCategoryRoutes(humaAPI, cfg.Categories)
	v1.RegisterTagRoutes(humaAPI, cfg.Tags)
	v1.RegisterStatsRoutes(humaAPI, cfg.Session)
	v1.RegisterSettingsRoutes(humaAPI, cfg.DB, cfg.Session)
	v1.RegisterHealthRoutes(humaAPI, cfg.Session)

	// Admin diagnostics — only registered when explicitly enabled, so
	// the published image's API surface stays minimal for the common
	// case. Operators flip HAUL_ADMIN_DIAGNOSTICS_ENABLED=true.
	if cfg.Admin != nil && cfg.Admin.DiagnosticsEnabled && cfg.Admin.Registry != nil {
		v1.RegisterAdminDiagnosticsRoutes(humaAPI, cfg.Admin.Registry)
		v1.RegisterAdminCleanupHistoryRoutes(humaAPI, cfg.Admin.Registry)
	}

	return r
}
