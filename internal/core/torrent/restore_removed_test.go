package torrent

// restore_removed_test.go — Regression guard for the "cancelled downloads
// reappear after a restart" bug.
//
// Real-world incident: the user cancelled several in-progress downloads in
// the Haul dashboard. They vanished from the live view (Remove deletes the
// in-memory map entry + drops the anacrolix torrent). But Remove is a
// SOFT-delete — it only stamps `removed_at` on the torrents row, leaving the
// row and its `torrent_data` resume bytes intact. restoreFromDB then read
// EVERY row with no `removed_at IS NULL` filter, so on the next restart it
// re-added the soft-deleted torrents to the engine and they reappeared on
// the dashboard.
//
// The fix: restoreFromDB's SELECT filters `WHERE removed_at IS NULL`, and a
// per-row guard skips any row that still carries a removed_at as a backstop.
// This test drives the real end-to-end path (Add → Remove → reopen session
// over the same DB → restoreFromDB) and asserts the deleted torrent stays
// gone while a non-deleted control torrent is correctly restored.

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/db"
	"github.com/beacon-stack/haul/internal/events"
)

// newTestSessionWithDB builds a Session backed by a real migrated SQLite
// database at dbPath. Reusing the same dbPath across two calls simulates a
// container restart: the second session's NewSession runs restoreFromDB
// against the rows the first session persisted.
func newTestSessionWithDB(t *testing.T, dbPath string) *Session {
	t.Helper()
	database, err := db.Open(config.DatabaseConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	if err := db.Migrate(database.SQL); err != nil {
		t.Fatalf("migrating test db: %v", err)
	}
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: t.TempDir(),
		DataDir:     t.TempDir(),
		EnableDHT:   false,
		EnablePEX:   false,
		EnableUTP:   false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	session, err := NewSession(cfg, database.SQL, bus, logger)
	if err != nil {
		database.Close()
		t.Fatalf("creating db-backed test session: %v", err)
	}
	t.Cleanup(func() {
		session.Close()
		database.Close()
	})
	return session
}

// listContains reports whether the session's live list includes the hash.
func listContains(s *Session, hash string) bool {
	for _, info := range s.List() {
		if info.InfoHash == hash {
			return true
		}
	}
	return false
}

// TestRestoreFromDB_SkipsRemovedTorrents is the headline regression for the
// "cancelled downloads come back after a restart" bug. A soft-deleted torrent
// must NOT be re-added to the engine on the next startup, while a torrent that
// was never deleted MUST be restored.
func TestRestoreFromDB_SkipsRemovedTorrents(t *testing.T) {
	// One DB file shared across both session lifecycles — the restart.
	dbPath := t.TempDir() + "/haul.db"

	// ── Session #1: add two torrents, soft-delete one ──────────────────────
	sess1 := newTestSessionWithDB(t, dbPath)

	deletedHash, _ := addTestTorrent(t, sess1, nil)
	keptHash, _ := addTestTorrent(t, sess1, nil)
	if deletedHash == keptHash {
		t.Fatal("test torrents collided on the same info hash")
	}

	// Both persisted with resume data (addTestTorrent waits for ready, which
	// is past saveTorrentData) — sanity-check before we delete one.
	if !listContains(sess1, deletedHash) || !listContains(sess1, keptHash) {
		t.Fatal("both torrents should be live before Remove")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess1.Remove(ctx, deletedHash, false); err != nil {
		t.Fatalf("Remove(deletedHash): %v", err)
	}

	// Close session #1 so its anacrolix client + bolt store release before
	// the "restart". Cleanup will Close again (idempotent) and close the DB.
	sess1.Close()

	// ── Session #2: reopen over the SAME DB → restoreFromDB runs ───────────
	sess2 := newTestSessionWithDB(t, dbPath)

	// Give restore's async waitAndStart goroutines a beat to register the
	// restored torrent in s.torrents. The deleted one must never appear.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if listContains(sess2, keptHash) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if listContains(sess2, deletedHash) {
		t.Fatalf(
			"REGRESSION: soft-deleted torrent %s reappeared after restart. "+
				"restoreFromDB must filter `removed_at IS NULL` (and skip removed "+
				"rows defensively) — a cancelled download must stay gone from the "+
				"dashboard. See restoreFromDB in session.go.",
			deletedHash,
		)
	}

	// Guard against over-filtering: a torrent that was never deleted must
	// still be restored, otherwise the WHERE clause is too aggressive.
	if !listContains(sess2, keptHash) {
		t.Fatalf(
			"non-deleted torrent %s was NOT restored after restart — the "+
				"`removed_at IS NULL` filter is dropping live torrents too.",
			keptHash,
		)
	}
}
