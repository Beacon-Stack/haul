package db

import (
	"path/filepath"
	"testing"

	"github.com/beacon-stack/haul/internal/config"
)

// Rows written by older Haul versions hold time.Time.String() output.
// Migrate must rewrite them to dbtime's RFC3339 layout, leave canonical and
// unparseable values alone, and be a no-op the second time.
func TestMigrate_NormalizesLegacyTorrentTimes(t *testing.T) {
	database, err := Open(config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "haul.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Migrate(database.SQL); err != nil {
		t.Fatal(err)
	}

	insert := func(hash, added string, completed, removed any) {
		t.Helper()
		if _, err := database.SQL.Exec(
			`INSERT INTO torrents (info_hash, name, save_path, added_at, completed_at, removed_at) VALUES (?, 'n', '/d', ?, ?, ?)`,
			hash, added, completed, removed); err != nil {
			t.Fatal(err)
		}
	}
	insert("legacy", "2026-11-18 17:26:54.587019257 +0000 UTC", "2026-11-18 18:00:00.5 +0000 UTC m=+12.3", "2026-11-19T09:00:00Z")
	insert("current", "2026-11-18T17:26:54.587Z", nil, nil)
	insert("garbage", "not a time", nil, nil)

	n, err := NormalizeTorrentTimes(database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("normalized %d values, want 3 (legacy added_at, completed_at, removed_at)", n)
	}

	read := func(hash, col string) string {
		t.Helper()
		var v string
		if err := database.SQL.QueryRow(`SELECT `+col+` FROM torrents WHERE info_hash = ?`, hash).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct{ hash, col, want string }{
		{"legacy", "added_at", "2026-11-18T17:26:54.587Z"},
		{"legacy", "completed_at", "2026-11-18T18:00:00.500Z"},
		{"legacy", "removed_at", "2026-11-19T09:00:00.000Z"},
		{"current", "added_at", "2026-11-18T17:26:54.587Z"},
		{"garbage", "added_at", "not a time"},
	} {
		if got := read(c.hash, c.col); got != c.want {
			t.Errorf("%s.%s = %q, want %q", c.hash, c.col, got, c.want)
		}
	}

	if n, err := NormalizeTorrentTimes(database.SQL); err != nil || n != 0 {
		t.Fatalf("second run normalized %d values (err %v), want 0", n, err)
	}
}
