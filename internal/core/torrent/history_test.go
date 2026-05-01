package torrent

// history_test.go — pins the SQL the history-lookup endpoint emits
// across filter combinations. Tested at the buildHistoryQuery layer
// (pure: filter struct → SQL string + args) so we don't need a
// postgres test fixture, while still catching regressions in the
// WHERE-clause assembly that powers Pilot/Prism's library badges
// and the manual-search guardrail.
//
// LookupHistory's nil-DB short-circuit and rows.Scan plumbing are
// covered separately by an integration test against the live compose
// stack — out of scope here because Haul has no DB-test infra.

import (
	"context"
	"strings"
	"testing"
)

func TestBuildHistoryQuery_AllFiltersJoinWithAnd(t *testing.T) {
	q, args := buildHistoryQuery(HistoryFilter{
		Service:   "pilot",
		InfoHash:  "abc",
		EpisodeID: "ep-1",
		TMDBID:    95479,
		Season:    1,
		Episode:   48,
	})

	// Every supplied filter must show up as a WHERE clause.
	for _, want := range []string{
		"requester_service = $1",
		"info_hash = $2",
		"requester_episode_id = $3",
		"requester_tmdb_id = $4",
		"requester_season = $5",
		"requester_episode = $6",
		"removed_at IS NULL", // implicit when IncludeRemoved=false
	} {
		if !strings.Contains(q, want) {
			t.Errorf("expected query to contain %q, got:\n%s", want, q)
		}
	}

	wantArgs := []any{"pilot", "abc", "ep-1", 95479, 1, 48}
	if len(args) != len(wantArgs) {
		t.Fatalf("args: got %d, want %d (%v)", len(args), len(wantArgs), args)
	}
	for i, want := range wantArgs {
		if args[i] != want {
			t.Errorf("args[%d]: got %v, want %v", i, args[i], want)
		}
	}
}

// IncludeRemoved=true must NOT add the implicit "removed_at IS NULL"
// guard. Without this, Pilot's manual-search guardrail can't see
// previously-removed downloads (the user might still want to know
// "you grabbed this and removed it" before grabbing again).
func TestBuildHistoryQuery_IncludeRemovedDropsImplicitGuard(t *testing.T) {
	q, _ := buildHistoryQuery(HistoryFilter{
		InfoHash:       "xyz",
		IncludeRemoved: true,
	})
	if strings.Contains(q, "removed_at IS NULL") {
		t.Errorf("IncludeRemoved=true should drop the active-only guard; got:\n%s", q)
	}
}

// Empty filter still produces a valid query — it returns the most
// recent active torrents. Pilot's Activity rail uses this when
// listing "downloaded but not in library" and applies the join
// client-side.
func TestBuildHistoryQuery_NoFiltersStillScopesToActive(t *testing.T) {
	q, args := buildHistoryQuery(HistoryFilter{})
	if !strings.Contains(q, "WHERE removed_at IS NULL") {
		t.Errorf("no-filter query must still scope to active records; got:\n%s", q)
	}
	if len(args) != 0 {
		t.Errorf("no-filter args: got %v, want []", args)
	}
}

// Default limit when zero/negative — protects against a
// "give me everything" request that could OOM the API.
func TestBuildHistoryQuery_LimitDefaultsTo100(t *testing.T) {
	q, _ := buildHistoryQuery(HistoryFilter{})
	if !strings.Contains(q, "LIMIT 100") {
		t.Errorf("expected default LIMIT 100; got:\n%s", q)
	}

	q2, _ := buildHistoryQuery(HistoryFilter{Limit: -5})
	if !strings.Contains(q2, "LIMIT 100") {
		t.Errorf("negative Limit should default to 100; got:\n%s", q2)
	}
}

// Explicit limit honored — caller-controlled paging.
func TestBuildHistoryQuery_LimitOverrideHonored(t *testing.T) {
	q, _ := buildHistoryQuery(HistoryFilter{Limit: 7})
	if !strings.Contains(q, "LIMIT 7") {
		t.Errorf("expected LIMIT 7; got:\n%s", q)
	}
}

// Newest-first ordering matters for callers that want "the most
// recent download for this episode". Locking the ORDER BY here
// prevents an accidental switch to ASC during a refactor.
func TestBuildHistoryQuery_OrdersByAddedAtDesc(t *testing.T) {
	q, _ := buildHistoryQuery(HistoryFilter{})
	if !strings.Contains(q, "ORDER BY added_at DESC") {
		t.Errorf("expected ORDER BY added_at DESC; got:\n%s", q)
	}
}

// Movie-side lookup (Prism) — confirms the full triple of arr-side
// columns is plumbed independently from the TVDB/episode columns.
func TestBuildHistoryQuery_MovieFilter(t *testing.T) {
	q, args := buildHistoryQuery(HistoryFilter{
		Service: "prism",
		MovieID: "27b54ce3-1bbf-4df8-8f93-e002e52f19c7",
	})
	if !strings.Contains(q, "requester_movie_id = $2") {
		t.Errorf("movie filter not in query:\n%s", q)
	}
	if len(args) != 2 || args[0] != "prism" || args[1] != "27b54ce3-1bbf-4df8-8f93-e002e52f19c7" {
		t.Errorf("movie args: got %v", args)
	}
}

// LookupHistory's nil-DB short-circuit returns an empty slice (not
// an error). Callers treat "no record" as "Haul has never seen
// this" — making this distinction matters because the alternative
// (returning nil + error) would surface as a UI error toast.
func TestLookupHistory_NilDBReturnsEmpty(t *testing.T) {
	s := &Session{} // no db
	out, err := s.LookupHistory(context.Background(), HistoryFilter{InfoHash: "x"})
	if err != nil {
		t.Fatalf("nil-db lookup should not error; got %v", err)
	}
	if out == nil {
		t.Fatal("nil-db lookup should return non-nil empty slice")
	}
	if len(out) != 0 {
		t.Errorf("nil-db should return empty; got %d records", len(out))
	}
}
