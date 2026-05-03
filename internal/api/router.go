package api

import (
	"database/sql"
	"log/slog"
	"net/http"

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
	"github.com/beacon-stack/haul/internal/version"
)

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
