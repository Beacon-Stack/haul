package torrent

// classify_stalled_test.go — Unit tests for the pure stall classifier used
// by torrentInfo() to set Info.Stalled. This logic is the third surface
// (alongside CheckStalls and ListStalled) that reads the same primitive
// state, so keep it in sync with stall.go.
//
// The bug this file pins: after a container restart, torrents appeared as
// "stalled" (red) in the UI for up to a minute before turning blue again.
// Root cause: torrentInfo() ignored sessionStartupGrace. Restarted torrents
// had firstPeerAt=nil (fresh in-memory) and addedAt=<original DB time>
// (possibly hours ago), which tripped the "no peers for firstPeerTimeout"
// check immediately. CheckStalls/ListStalled both honored the grace, but
// torrentInfo() didn't. If anyone reintroduces that mismatch, the
// "RestartGrace" test here fails with a clear regression message.

import (
	"testing"
	"time"
)

func TestClassifyStalled(t *testing.T) {
	// Lock our threshold overrides for the test.
	savedFirst := firstPeerTimeout
	savedGrace := sessionStartupGrace
	firstPeerTimeout = 3 * time.Minute
	sessionStartupGrace = 10 * time.Minute
	t.Cleanup(func() {
		firstPeerTimeout = savedFirst
		sessionStartupGrace = savedGrace
	})

	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	longAgo := now.Add(-24 * time.Hour)
	justStarted := now.Add(-30 * time.Second)
	wayBack := now.Add(-1 * time.Hour)

	base := stallParams{
		now:              now,
		status:           StatusDownloading,
		hasInfo:          true,
		bytesMissing:     1024 * 1024, // 1 MB missing → download in progress
		sessionStartedAt: wayBack,     // well past startup grace by default
		addedAt:          wayBack,
		firstPeerAt:      nil,
		lastActivityAt:   now.Add(-30 * time.Second), // fresh activity
		stallTimeout:     2 * time.Minute,
	}

	t.Run("NonDownloadingStatusNeverStalled", func(t *testing.T) {
		for _, s := range []Status{StatusSeeding, StatusCompleted, StatusPaused, StatusQueued, StatusFailed} {
			p := base
			p.status = s
			if classifyStalled(p) {
				t.Errorf("status=%q was classified stalled; only downloading should be", s)
			}
		}
	})

	t.Run("PreMetadataNeverStalled", func(t *testing.T) {
		p := base
		p.hasInfo = false
		if classifyStalled(p) {
			t.Error("pre-metadata torrent was stalled; should be unclassified until metadata arrives")
		}
	})

	t.Run("FullyDownloadedNeverStalled", func(t *testing.T) {
		p := base
		p.bytesMissing = 0
		if classifyStalled(p) {
			t.Error("bytesMissing=0 was stalled; completed work can't stall")
		}
	})

	// ── The headline regression guard ────────────────────────────────────────
	// Simulates container restart: the session just started, but the torrent's
	// addedAt is the DB-persisted original (hours ago), firstPeerAt is nil
	// (in-memory state was reset), and lastActivityAt is zero. Without the
	// startup grace check, this set of inputs matches rule 5 ("no peers ever
	// past firstPeerTimeout") and/or rule 6 (no recent activity) and we'd
	// wrongly report stalled.
	t.Run("RestartGrace_NoPeersYetButSessionJustStarted", func(t *testing.T) {
		p := stallParams{
			now:              now,
			status:           StatusDownloading,
			hasInfo:          true,
			bytesMissing:     1024 * 1024,
			sessionStartedAt: now.Add(-30 * time.Second), // WELL inside 10-min grace
			addedAt:          longAgo,                    // original add time from DB
			firstPeerAt:      nil,                        // in-memory state is fresh
			lastActivityAt:   time.Time{},                // never observed activity yet
			stallTimeout:     2 * time.Minute,
		}
		if classifyStalled(p) {
			t.Fatal("REGRESSION: a freshly-restarted session reported stalled within the startup grace. " +
				"This means torrentInfo() isn't honoring sessionStartupGrace the way CheckStalls does — " +
				"users see red bars on every restart until peers reconnect. See classify_stalled_test.go.")
		}
	})

	t.Run("NoPeersEver_StallsAfterGraceAndPastFirstPeerTimeout", func(t *testing.T) {
		p := stallParams{
			now:              now,
			status:           StatusDownloading,
			hasInfo:          true,
			bytesMissing:     1024 * 1024,
			sessionStartedAt: longAgo, // past grace
			addedAt:          longAgo, // past firstPeerTimeout
			firstPeerAt:      nil,
			lastActivityAt:   time.Time{},
			stallTimeout:     2 * time.Minute,
		}
		if !classifyStalled(p) {
			t.Error("expected stalled=true for a torrent past grace + past firstPeerTimeout with no peers ever")
		}
	})

	t.Run("NoPeersEver_NotYetStalledInsideFirstPeerWindow", func(t *testing.T) {
		// Session is past grace, but the torrent was only added 30 seconds
		// ago — below the firstPeerTimeout threshold.
		p := stallParams{
			now:              now,
			status:           StatusDownloading,
			hasInfo:          true,
			bytesMissing:     1024 * 1024,
			sessionStartedAt: longAgo,
			addedAt:          justStarted, // less than firstPeerTimeout ago
			firstPeerAt:      nil,
			lastActivityAt:   time.Time{},
			stallTimeout:     2 * time.Minute,
		}
		if classifyStalled(p) {
			t.Error("expected stalled=false for a fresh add still inside firstPeerTimeout")
		}
	})

	t.Run("RecentActivity_NotStalled", func(t *testing.T) {
		// Recent activity + a peer observation in the past — the classic
		// "actively downloading" state. Needs firstPeerAt set so we skip
		// rule 5 (no-peers-ever) and hit the activity check instead.
		p := base
		fp := now.Add(-10 * time.Minute)
		p.firstPeerAt = &fp
		p.lastActivityAt = now.Add(-5 * time.Second)
		if classifyStalled(p) {
			t.Error("expected stalled=false with activity 5s ago")
		}
	})

	t.Run("ActivityOlderThanStallTimeout_Stalled", func(t *testing.T) {
		p := base
		// Activity older than stallTimeout (which is 2 min in base).
		p.lastActivityAt = now.Add(-3 * time.Minute)
		// Set firstPeerAt so we don't trip rule 5 by accident.
		fp := now.Add(-3 * time.Minute)
		p.firstPeerAt = &fp
		if !classifyStalled(p) {
			t.Error("expected stalled=true with lastActivityAt older than stallTimeout")
		}
	})

	t.Run("ZeroLastActivity_FallsBackToAddedAt", func(t *testing.T) {
		// lastActivityAt is zero; the baseline becomes addedAt. If addedAt
		// is older than stallTimeout, should report stalled.
		p := base
		p.lastActivityAt = time.Time{}
		p.addedAt = now.Add(-5 * time.Minute)
		// firstPeerAt older than stallTimeout so we fall into rule 6 path.
		fp := now.Add(-4 * time.Minute)
		p.firstPeerAt = &fp
		if !classifyStalled(p) {
			t.Error("expected stalled=true when lastActivityAt is zero and addedAt baseline exceeds stallTimeout")
		}
	})

	t.Run("ZeroLastActivity_UsesFirstPeerAtIfLaterThanAddedAt", func(t *testing.T) {
		// lastActivityAt is zero. addedAt is ancient but firstPeerAt is
		// recent (peer connected 10 seconds ago). Baseline should be the
		// firstPeerAt (more recent), so we're NOT stalled.
		p := base
		p.lastActivityAt = time.Time{}
		p.addedAt = longAgo
		fp := now.Add(-10 * time.Second)
		p.firstPeerAt = &fp
		if classifyStalled(p) {
			t.Error("expected stalled=false when firstPeerAt is more recent than stallTimeout baseline")
		}
	})
}
