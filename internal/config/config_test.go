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

	t.Run("database.path", func(t *testing.T) {
		t.Setenv("HAUL_DATABASE_PATH", "/tmp/haul-test.db")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Database.Path != "/tmp/haul-test.db" {
			t.Fatalf("HAUL_DATABASE_PATH did not reach cfg.Database.Path — got %q.",
				cfg.Database.Path)
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

	// Docker secrets path for Pulse API key. HAUL_PULSE_API_KEY_FILE must
	// reach cfg.Pulse.APIKeyFile so load.go can read the file and populate
	// Pulse.APIKey. Without this binding, haul silently falls back to
	// "pulse integration disabled — no API key configured" at runtime
	// (the same failure mode that shipped in the initial deploy compose).
	t.Run("pulse.api_key_file", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "pulse-api-key.txt")
		if err := os.WriteFile(keyFile, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HAUL_PULSE_API_KEY_FILE", keyFile)
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Pulse.APIKeyFile != keyFile {
			t.Fatalf("HAUL_PULSE_API_KEY_FILE did not reach cfg.Pulse.APIKeyFile — "+
				"got %q. Check v.BindEnv(\"pulse.api_key_file\", ...) in load.go.",
				cfg.Pulse.APIKeyFile)
		}
	})
}

func TestLoad_PulseAPIKeyFileReadIntoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	keyFile := filepath.Join(t.TempDir(), "pulse-api-key.txt")
	if err := os.WriteFile(keyFile, []byte("pulse-key-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HAUL_PULSE_API_KEY", "inline-loses")
	t.Setenv("HAUL_PULSE_API_KEY_FILE", keyFile)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Pulse.APIKey.Value(); got != "pulse-key-from-file" {
		t.Fatalf("Pulse.APIKey = %q; want pulse-key-from-file (file must override inline env)", got)
	}
}

func TestLoad_InvalidPulseAPIKeyFilePath_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Setenv("HAUL_PULSE_API_KEY_FILE", "/nonexistent/pulse-api-key")

	if _, err := Load(""); err == nil {
		t.Fatal("expected error when pulse api_key_file path is invalid")
	}
}

// Removed: TestTorrentConfigDefaults — asserted Go zero values on a
// `var cfg TorrentConfig` literal that never went through Load. The
// comment claimed "default listen port should be 0" but Load() actually
// SetDefault's listen_port to 26656, so the test was asserting the
// OPPOSITE of the real default. TestLoadFromEnv_Critical covers what
// this should have been.

// Removed: TestSpeedScheduleConfig, TestWebhookConfigEvents,
// TestConfigStructure — all set struct fields, then read them back.
// Pure Go round-trips, no SUT logic.

// Removed: TestSeedLimitActions — tautology. The body asserted that
// each non-empty input produced a non-empty output. A regression to
// `cfg.SeedLimitAction = ""` regardless of input would still pass
// because the test wrote the value itself.

func TestAppName(t *testing.T) {
	name := AppName()
	if name != "haul" {
		t.Errorf("expected app name 'haul', got %q", name)
	}
}
