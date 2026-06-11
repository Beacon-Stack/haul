package torrent

// category_savepath_test.go — pins the save-path resolution order used
// by Session.Add: explicit request > category default > configured
// download dir. The category fallback is what makes per-category save
// paths a real feature for callers that send only a category name.

import (
	"path/filepath"
	"testing"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/db"
)

func TestCategorySavePath(t *testing.T) {
	database, err := db.Open(config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = database.SQL.Close() })
	if err := db.Migrate(database.SQL); err != nil {
		t.Fatalf("migrating test db: %v", err)
	}
	if _, err := database.SQL.Exec(
		`INSERT INTO categories (name, save_path) VALUES ('tv', '/library/tv'), ('misc', '')`,
	); err != nil {
		t.Fatalf("seeding categories: %v", err)
	}

	s := &Session{db: database.SQL}

	for _, tc := range []struct {
		name, category, want string
	}{
		{"configured category", "tv", "/library/tv"},
		{"category without save path", "misc", ""},
		{"unknown category", "movies", ""},
		{"empty category", "", ""},
	} {
		if got := s.categorySavePath(tc.category); got != tc.want {
			t.Errorf("%s: categorySavePath(%q) = %q, want %q", tc.name, tc.category, got, tc.want)
		}
	}

	// nil DB (sessions constructed without persistence) must not panic.
	nilDB := &Session{}
	if got := nilDB.categorySavePath("tv"); got != "" {
		t.Errorf("nil-db lookup = %q, want \"\"", got)
	}
}
