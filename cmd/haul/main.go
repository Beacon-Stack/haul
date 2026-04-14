package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/beacon-stack/haul/internal/api"
	"github.com/beacon-stack/haul/internal/api/ws"
	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/core/category"
	"github.com/beacon-stack/haul/internal/core/tag"
	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/db"
	"github.com/beacon-stack/haul/internal/events"
	"github.com/beacon-stack/haul/internal/pulse"
	"github.com/beacon-stack/haul/internal/version"
	"github.com/beacon-stack/haul/web"
)

func main() {
	cfgFile := flag.String("config", "", "path to config file")
	flag.Parse()

	// ── Config ────────────────────────────────────────────────────────────
	cfg, err := config.Load(*cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// ── Logger ────────────────────────────────────────────────────────────
	var handler slog.Handler
	opts := &slog.HandlerOptions{}
	switch cfg.Log.Level {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}

	if cfg.Log.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)

	logger.Info(version.AppName+" starting",
		"version", version.Version,
		"port", cfg.Server.Port,
	)

	// ── API Key ───────────────────────────────────────────────────────────
	generated, err := config.EnsureAPIKey(cfg)
	if err != nil {
		logger.Error("failed to generate API key", "error", err)
		os.Exit(1)
	}
	if generated {
		logger.Warn("generated API key — save this, it won't be shown again",
			"api_key", cfg.Auth.APIKey.Value(),
		)
	}

	// ── Database ──────────────────────────────────────────────────────────
	database, err := db.Open(cfg.Database)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	logger.Info("database connected",
		"driver", database.Driver,
		"path", cfg.Database.Path,
	)

	if err := db.Migrate(database.SQL, database.Driver); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("database migrations up to date")

	// ── Event bus ─────────────────────────────────────────────────────────
	bus := events.New(logger)

	// ── WebSocket hub ─────────────────────────────────────────────────────
	wsHub := ws.NewHub(logger)
	bus.Subscribe(wsHub.HandleEvent)

	// ── Torrent session ───────────────────────────────────────────────────
	session, err := torrent.NewSession(cfg.Torrent, database.SQL, bus, logger)
	if err != nil {
		logger.Error("torrent session failed", "error", err)
		os.Exit(1)
	}
	defer session.Close()

	logger.Info("torrent engine started",
		"download_dir", cfg.Torrent.DownloadDir,
		"dht", cfg.Torrent.EnableDHT,
		"pex", cfg.Torrent.EnablePEX,
		"utp", cfg.Torrent.EnableUTP,
	)

	// ── Pulse integration (non-blocking) ──────────────────────────────────
	go func() {
		pi, piErr := pulse.New(cfg.Pulse, cfg.Server.Host, cfg.Server.Port, logger)
		if piErr != nil {
			logger.Warn("pulse integration failed", "error", piErr)
		}
		if pi != nil {
			// Keep reference for cleanup — but since this is in a goroutine,
			// we rely on process exit to clean up the heartbeat.
			_ = pi
		}
	}()

	// ── Services ──────────────────────────────────────────────────────────
	categorySvc := category.NewService(database.SQL)
	tagSvc := tag.NewService(database.SQL)

	// ── Webhook dispatcher ────────────────────────────────────────────────
	if len(cfg.Webhooks) > 0 {
		whDispatcher := torrent.NewWebhookDispatcher(cfg.Webhooks, logger)
		bus.Subscribe(whDispatcher.HandleEvent)
		logger.Info("webhook dispatcher enabled", "targets", len(cfg.Webhooks))
	}

	// ── External command hooks ────────────────────────────────────────────
	if cfg.Torrent.OnAddCommand != "" || cfg.Torrent.OnCompleteCommand != "" {
		hooks := torrent.NewHookRunner(cfg.Torrent.OnAddCommand, cfg.Torrent.OnCompleteCommand, logger)
		bus.Subscribe(hooks.HandleEvent)
		logger.Info("event hooks enabled",
			"on_add", cfg.Torrent.OnAddCommand != "",
			"on_complete", cfg.Torrent.OnCompleteCommand != "",
		)
	}

	// ── VPN check (non-blocking — IP lookup can be slow in containers) ───
	go func() {
		torrent.CheckVPN()
		vpnActive, vpnIface, extIP := torrent.GetVPNStatus()
		if vpnActive {
			logger.Info("VPN detected", "interface", vpnIface, "external_ip", extIP)
		} else {
			logger.Warn("no VPN detected — torrent traffic is NOT encrypted", "external_ip", extIP)
		}
	}()

	// ── Background checkers ──────────────────────────────────────────────
	go func() {
		seedTicker := time.NewTicker(60 * time.Second)
		stallTicker := time.NewTicker(30 * time.Second)
		healthTicker := time.NewTicker(30 * time.Second)
		bandwidthTicker := time.NewTicker(5 * time.Second)
		scheduleTicker := time.NewTicker(60 * time.Second)
		vpnTicker := time.NewTicker(5 * time.Minute)
		defer seedTicker.Stop()
		defer stallTicker.Stop()
		defer healthTicker.Stop()
		defer bandwidthTicker.Stop()
		defer scheduleTicker.Stop()
		defer vpnTicker.Stop()
		for {
			select {
			case <-seedTicker.C:
				session.CheckSeedLimits(context.Background())
			case <-stallTicker.C:
				session.CheckStalls(context.Background())
			case <-healthTicker.C:
				session.PublishHealth(context.Background())
			case <-vpnTicker.C:
				torrent.CheckVPN()
			case <-bandwidthTicker.C:
				session.AdaptiveBandwidth()
			case <-scheduleTicker.C:
				session.CheckSpeedSchedule(cfg.Schedule)
			}
		}
	}()

	// ── HTTP router ───────────────────────────────────────────────────────
	router := api.NewRouter(api.RouterConfig{
		Logger:     logger,
		Session:    session,
		WSHub:      wsHub,
		Categories: categorySvc,
		Tags:       tagSvc,
		DB:         database.SQL,
	})

	// Mount the embedded web UI as a catch-all.
	mux := http.NewServeMux()
	mux.Handle("/api/", router)
	mux.Handle("/health", router)
	mux.Handle("/", web.ServeStatic())

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ── Start ─────────────────────────────────────────────────────────────
	go func() {
		logger.Info("HTTP server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info(version.AppName + " stopped")
}
