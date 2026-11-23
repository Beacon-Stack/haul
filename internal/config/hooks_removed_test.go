package config

import (
	"os"
	"path/filepath"
	"testing"
)

// An old config that still sets the removed command hooks must load, so
// Haul can start and warn that they're ignored.
func TestLoad_OldHookSettingsStillLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("torrent:\n  on_complete_command: /scripts/x.sh %p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("old config should load: %v", err)
	}
	if cfg.Torrent.OnCompleteCommand == "" {
		t.Error("the old setting should be read so Haul can warn about it")
	}
}
