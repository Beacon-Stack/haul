package v1

// settings_test.go — regression suite for the "settings toggle doesn't
// work at runtime" bug class.
//
// ⚠ Run this before touching:
//   - internal/api/v1/settings.go (especially applyRuntimeSettings)
//   - internal/core/torrent/session.go PauseOnComplete / SetPauseOnComplete
//   - internal/core/torrent/session.go monitorCompletion (the completion
//     handler that reads the live value)
//
// Two bugs are being guarded here:
//
//   1. The Viper AutomaticEnv gotcha that dropped HAUL_TORRENT_PAUSE_ON_COMPLETE.
//      That's covered in internal/config/config_test.go.
//
//   2. The phantom-write bug: UI toggle persisted to the `settings` table
//      but nothing in the torrent engine ever consulted it. This test file
//      covers the fix — applyRuntimeSettings dispatches keys to Session
//      setters so the runtime field actually changes.

import (
	"testing"
	"time"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/events"

	"log/slog"
	"os"
)

// Short-circuit the 10-second public-IP lookup so the whole test file
// runs in <1s total.
func init() {
	torrent.SetPublicIPDetectTimeoutForTesting(200 * time.Millisecond)
}

func TestApplyRuntimeSettings_PauseOnComplete(t *testing.T) {
	// Build a minimal Session to exercise the setter path. We don't need
	// a real anacrolix client — we just need something that implements
	// the SetPauseOnComplete / PauseOnComplete contract. Use the real
	// Session with nil DB (the session-side stall tests do the same).
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: t.TempDir(),
		EnableDHT:   false,
		EnablePEX:   false,
		EnableUTP:   false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	session, err := torrent.NewSession(cfg, nil, bus, logger)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(session.Close)

	// Default should be false.
	if session.PauseOnComplete() {
		t.Fatal("expected default pause_on_complete=false")
	}

	// Apply a settings update — this is the critical path that
	// previously was a phantom write.
	applied := applyRuntimeSettings(session, map[string]string{
		"pause_on_complete": "true",
	})

	if len(applied) != 1 || applied[0] != "pause_on_complete" {
		t.Errorf("expected applyRuntimeSettings to report pause_on_complete applied, got %v", applied)
	}

	if !session.PauseOnComplete() {
		t.Fatal("applyRuntimeSettings did not propagate pause_on_complete=true to the live " +
			"Session — the toggle is still a phantom write. Check the switch in " +
			"applyRuntimeSettings() and Session.SetPauseOnComplete().")
	}

	// And the reverse.
	applyRuntimeSettings(session, map[string]string{
		"pause_on_complete": "false",
	})
	if session.PauseOnComplete() {
		t.Fatal("applyRuntimeSettings did not propagate pause_on_complete=false")
	}
}

// TestApplyRuntimeSettings_AlsoAcceptsOne verifies we accept "1" as
// truthy alongside "true". The UI currently sends "true"/"false" but
// some CLI tools / tests may send "1"/"0".
func TestApplyRuntimeSettings_AlsoAcceptsOne(t *testing.T) {
	session := buildMinimalSession(t)
	applyRuntimeSettings(session, map[string]string{"pause_on_complete": "1"})
	if !session.PauseOnComplete() {
		t.Error("applyRuntimeSettings should accept \"1\" as truthy")
	}
}

// TestApplyRuntimeSettings_UnknownKeyIgnored verifies that a key we don't
// know how to dispatch (e.g. max_connections, which needs a restart) is
// silently ignored by applyRuntimeSettings. The DB write is handled
// elsewhere; this function only dispatches runtime-effective keys.
func TestApplyRuntimeSettings_UnknownKeyIgnored(t *testing.T) {
	session := buildMinimalSession(t)
	applied := applyRuntimeSettings(session, map[string]string{
		"max_connections":   "1000",
		"pause_on_complete": "true",
		"network_interface": "wg0",
	})
	// Only pause_on_complete should have been applied — the other two
	// are startup-only settings that the session doesn't hot-reload.
	if len(applied) != 1 || applied[0] != "pause_on_complete" {
		t.Errorf("expected only pause_on_complete to be applied, got %v", applied)
	}
}

// TestApplyRuntimeSettings_NilSession is the graceful-degradation case:
// if for some reason the session is nil (e.g. settings endpoint wired up
// before Session construction finishes), the handler must not panic.
func TestApplyRuntimeSettings_NilSession(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyRuntimeSettings(nil) panicked: %v", r)
		}
	}()
	applied := applyRuntimeSettings(nil, map[string]string{"pause_on_complete": "true"})
	if len(applied) != 0 {
		t.Errorf("expected empty applied list for nil session, got %v", applied)
	}
}

func buildMinimalSession(t *testing.T) *torrent.Session {
	t.Helper()
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: t.TempDir(),
		EnableDHT:   false,
		EnablePEX:   false,
		EnableUTP:   false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	session, err := torrent.NewSession(cfg, nil, bus, logger)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(session.Close)
	return session
}
