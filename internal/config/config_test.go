package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFromEnv_Critical is the regression test for the Viper AutomaticEnv
// gotcha that caused two production bugs in quick succession:
//
//   - HAUL_PULSE_URL silently ignored (pulse.url had no SetDefault or
//     BindEnv, so Viper's AutomaticEnv couldn't resolve it — Haul never
//     registered with the control plane).
//   - HAUL_TORRENT_LISTEN_PORT failure mode: if torrent.listen_port loses
//     its SetDefault line, the PIA-forwarded port never reaches the
//     torrent engine, and every torrent stalls at 0 peers.
//
// The key property this test protects is that critical env vars must
// propagate all the way into the config struct — either via an explicit
// BindEnv or a SetDefault that registers the key with Viper.
//
// If any subtest here fails, DO NOT "fix" it by patching the env var in
// the test. Fix the binding in load.go so the env var works for real users.
func TestLoadFromEnv_Critical(t *testing.T) {
	// Isolate from the developer's home config.
	t.Setenv("HOME", t.TempDir())
	// And from any stray /etc/haul/config.yaml (we can't override this in
	// Viper's search path list, so just hope it isn't there). On CI it
	// won't be.

	t.Run("torrent.listen_port", func(t *testing.T) {
		t.Setenv("HAUL_TORRENT_LISTEN_PORT", "54321")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Torrent.ListenPort != 54321 {
			t.Fatalf("HAUL_TORRENT_LISTEN_PORT=54321 did not reach cfg.Torrent.ListenPort — "+
				"got %d. This is the \"torrent stall\" regression — check "+
				"v.SetDefault(\"torrent.listen_port\", ...) in load.go.", cfg.Torrent.ListenPort)
		}
	})

	t.Run("pulse.url", func(t *testing.T) {
		t.Setenv("HAUL_PULSE_URL", "http://pulse-test:9696")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Pulse.URL != "http://pulse-test:9696" {
			t.Fatalf("HAUL_PULSE_URL did not reach cfg.Pulse.URL — got %q. "+
				"This is the \"Haul not registering with Pulse\" regression — "+
				"check v.BindEnv(\"pulse.url\", ...) in load.go.", cfg.Pulse.URL)
		}
	})

	t.Run("database.dsn", func(t *testing.T) {
		t.Setenv("HAUL_DATABASE_DSN", "postgres://test")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Database.DSN.Value() != "postgres://test" {
			t.Fatalf("HAUL_DATABASE_DSN did not reach cfg.Database.DSN — got %q.",
				cfg.Database.DSN.Value())
		}
	})

	t.Run("torrent.download_dir", func(t *testing.T) {
		t.Setenv("HAUL_TORRENT_DOWNLOAD_DIR", "/test/downloads")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Torrent.DownloadDir != "/test/downloads" {
			t.Fatalf("HAUL_TORRENT_DOWNLOAD_DIR did not reach cfg.Torrent.DownloadDir — got %q.",
				cfg.Torrent.DownloadDir)
		}
	})

	// This is the regression test for the "toggle doesn't work" bug.
	// HAUL_TORRENT_PAUSE_ON_COMPLETE was silently dropped because
	// torrent.pause_on_complete had no SetDefault — the Viper
	// AutomaticEnv gotcha. Same root cause as the pulse.url bug.
	t.Run("torrent.pause_on_complete", func(t *testing.T) {
		t.Setenv("HAUL_TORRENT_PAUSE_ON_COMPLETE", "true")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Torrent.PauseOnComplete {
			t.Fatal("HAUL_TORRENT_PAUSE_ON_COMPLETE=true did not reach " +
				"cfg.Torrent.PauseOnComplete. This is the \"settings toggle silently " +
				"ignored\" regression — check v.SetDefault(\"torrent.pause_on_complete\", ...) " +
				"in load.go.")
		}
	})

	t.Run("torrent.rename_on_complete", func(t *testing.T) {
		t.Setenv("HAUL_TORRENT_RENAME_ON_COMPLETE", "true")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Torrent.RenameOnComplete {
			t.Fatal("HAUL_TORRENT_RENAME_ON_COMPLETE=true did not propagate. " +
				"This is the same Viper SetDefault gotcha as pause_on_complete.")
		}
	})

	// Docker secrets path for the DB password. HAUL_DATABASE_PASSWORD_FILE
	// must reach cfg.Database.PasswordFile so load.go can splice the file's
	// contents into the DSN. A tempfile is used here because Load actually
	// reads the file at load time.
	t.Run("database.password_file", func(t *testing.T) {
		pwFile := filepath.Join(t.TempDir(), "pw.txt")
		if err := os.WriteFile(pwFile, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HAUL_DATABASE_DSN", "postgres://user:plain@host:5432/db")
		t.Setenv("HAUL_DATABASE_PASSWORD_FILE", pwFile)
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Database.PasswordFile != pwFile {
			t.Fatalf("HAUL_DATABASE_PASSWORD_FILE did not reach cfg.Database.PasswordFile — "+
				"got %q. Check v.BindEnv(\"database.password_file\", ...) in load.go.",
				cfg.Database.PasswordFile)
		}
	})
}

// TestLoad_PasswordFileSplicedIntoDSN exercises the end-to-end behavior:
// when HAUL_DATABASE_PASSWORD_FILE points at a real file, Load must read
// that file and splice its contents into the password component of
// HAUL_DATABASE_DSN.
func TestLoad_PasswordFileSplicedIntoDSN(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw.txt")
	if err := os.WriteFile(pwFile, []byte("secretpw\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HAUL_DATABASE_DSN", "postgres://user:plain@host:5432/db")
	t.Setenv("HAUL_DATABASE_PASSWORD_FILE", pwFile)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := "postgres://user:secretpw@host:5432/db"
	if got := cfg.Database.DSN.Value(); got != want {
		t.Fatalf("spliced DSN = %q; want %q", got, want)
	}
}

func TestLoad_NoPasswordFile_LeavesDSNIntact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Setenv("HAUL_DATABASE_DSN", "postgres://user:plain@host:5432/db")
	t.Setenv("HAUL_DATABASE_PASSWORD_FILE", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := "postgres://user:plain@host:5432/db"
	if got := cfg.Database.DSN.Value(); got != want {
		t.Fatalf("DSN = %q; want %q", got, want)
	}
}

func TestLoad_InvalidPasswordFilePath_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Setenv("HAUL_DATABASE_DSN", "postgres://user:plain@host:5432/db")
	t.Setenv("HAUL_DATABASE_PASSWORD_FILE", "/nonexistent/secret")

	if _, err := Load(""); err == nil {
		t.Fatal("expected error when password file path is invalid")
	}
}

func TestTorrentConfigDefaults(t *testing.T) {
	var cfg TorrentConfig

	if cfg.ListenPort != 0 {
		t.Error("default listen port should be 0")
	}
	if cfg.MaxActiveDownloads != 0 {
		t.Error("default max active downloads should be 0 (unlimited)")
	}
	if cfg.GlobalDownloadLimit != 0 {
		t.Error("default global download limit should be 0 (unlimited)")
	}
	if cfg.PauseOnComplete {
		t.Error("pause on complete should default to false")
	}
	if cfg.EnableDHT {
		t.Error("DHT should default to false (zero value)")
	}
}

func TestSpeedScheduleConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  SpeedScheduleConfig
		want bool
	}{
		{
			name: "disabled schedule",
			cfg:  SpeedScheduleConfig{Enabled: false},
			want: false,
		},
		{
			name: "enabled schedule",
			cfg:  SpeedScheduleConfig{Enabled: true, FromHour: 22, ToHour: 6, Days: "all"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.Enabled != tt.want {
				t.Errorf("expected enabled=%v, got %v", tt.want, tt.cfg.Enabled)
			}
		})
	}
}

func TestWebhookConfigEvents(t *testing.T) {
	wh := WebhookConfig{
		URL:    "http://example.com/webhook",
		Events: []string{"torrent_completed", "torrent_failed"},
	}

	if wh.URL == "" {
		t.Error("URL should not be empty")
	}
	if len(wh.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(wh.Events))
	}
}

func TestConfigStructure(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 8484},
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "/config/haul.db",
		},
		Torrent: TorrentConfig{
			DownloadDir:     "/downloads",
			PauseOnComplete: true,
			StallTimeout:    120,
		},
	}

	if cfg.Server.Port != 8484 {
		t.Errorf("expected port 8484, got %d", cfg.Server.Port)
	}
	if cfg.Torrent.DownloadDir != "/downloads" {
		t.Error("expected download dir /downloads")
	}
	if !cfg.Torrent.PauseOnComplete {
		t.Error("expected pause on complete")
	}
	if cfg.Torrent.StallTimeout != 120 {
		t.Errorf("expected stall timeout 120, got %d", cfg.Torrent.StallTimeout)
	}
}

func TestAppName(t *testing.T) {
	name := AppName()
	if name != "haul" {
		t.Errorf("expected app name 'haul', got %q", name)
	}
}

func TestSeedLimitActions(t *testing.T) {
	validActions := []string{"pause", "remove", "remove_with_data"}

	for _, action := range validActions {
		cfg := TorrentConfig{SeedLimitAction: action}
		if cfg.SeedLimitAction == "" {
			t.Errorf("action %q should not be empty", action)
		}
	}
}
