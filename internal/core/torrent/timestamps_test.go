package torrent

// timestamps_test.go — Regression guard for TEXT timestamps written in Go's
// time.Time.String() format ("2026-11-18 17:26:54.587019257 +0000 UTC")
// instead of RFC3339. The SQLite driver stores a bound time.Time that way,
// restoreFromDB parsed it as RFC3339, the parse failed silently, and every
// restored torrent came back with added_at = 0001-01-01. The zero time was
// then written back on the next upsert, losing the real value for good.
// /api/v1/history also returned the raw non-RFC3339 strings to Pilot and
// Prism.

import (
	"context"
	"testing"
	"time"

	"github.com/beacon-stack/haul/internal/dbtime"
)

func TestRestoreFromDB_KeepsAddedAt(t *testing.T) {
	dbPath := t.TempDir() + "/haul.db"

	sess1 := newTestSessionWithDB(t, dbPath)
	hash, _ := addTestTorrent(t, sess1, nil)
	before, err := sess1.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	sess1.Close()

	sess2 := newTestSessionWithDB(t, dbPath)
	var after *Info
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if after, err = sess2.Get(hash); err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if after == nil {
		t.Fatalf("torrent %s was not restored: %v", hash, err)
	}

	if after.AddedAt.IsZero() {
		t.Fatal("REGRESSION: restored torrent has added_at = 0001-01-01. " +
			"Timestamps must be written with dbtime.Format and read with " +
			"dbtime.Parse (internal/core/torrent/session.go persist + restoreFromDB).")
	}
	if d := after.AddedAt.Sub(before.AddedAt); d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("added_at changed across restart: before %s, after %s", before.AddedAt, after.AddedAt)
	}
}

func TestLookupHistory_TimesAreRFC3339(t *testing.T) {
	dbPath := t.TempDir() + "/haul.db"
	sess := newTestSessionWithDB(t, dbPath)
	hash, _ := addTestTorrent(t, sess, nil)
	if err := sess.Remove(context.Background(), hash, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	recs, err := sess.LookupHistory(context.Background(), HistoryFilter{InfoHash: hash, IncludeRemoved: true})
	if err != nil || len(recs) != 1 {
		t.Fatalf("LookupHistory = %d records, err %v; want 1", len(recs), err)
	}
	r := recs[0]
	for field, v := range map[string]string{"added_at": r.AddedAt, "removed_at": r.RemovedAt} {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t.Fatalf("history %s = %q is not RFC3339 (Pilot and Prism parse it as RFC3339): %v", field, v, err)
		}
		if time.Since(ts) > time.Minute || time.Since(ts) < -time.Minute {
			t.Fatalf("history %s = %q is not a recent time", field, v)
		}
	}
	if !dbtime.IsCanonical(r.AddedAt) {
		t.Fatalf("history added_at = %q, want the fixed-width dbtime layout", r.AddedAt)
	}
}
