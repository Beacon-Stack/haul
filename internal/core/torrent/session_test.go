package torrent

import (
	"testing"
)

// Removed: TestTorrentInfoNotReady, TestManagedTorrentDefaults — both
// asserted Go zero-values on a struct literal without invoking any SUT
// code path. A mutation that broke `managedTorrent` initialization in
// Session.Add (where the struct is actually constructed) would not be
// caught. The dead-torrent regression suite covers what these were
// pretending to.

func TestStatusConstants(t *testing.T) {
	statuses := []Status{
		StatusDownloading,
		StatusSeeding,
		StatusPaused,
		StatusChecking,
		StatusQueued,
		StatusCompleted,
		StatusFailed,
	}

	seen := make(map[Status]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("status should not be empty")
		}
	}
}

func TestStallLevelConstants(t *testing.T) {
	if StallNone >= StallLevel1 {
		t.Error("StallNone should be less than StallLevel1")
	}
	if StallLevel1 >= StallLevel2 {
		t.Error("StallLevel1 should be less than StallLevel2")
	}
	if StallLevel2 >= StallLevel3 {
		t.Error("StallLevel2 should be less than StallLevel3")
	}
}

// Removed: TestTransferStatsZeroValue, TestAddRequestValidation,
// TestFileInfoPriority, TestHealthReportFields — all asserted Go
// runtime semantics (zero values, struct round-trips) or
// re-implemented validation logic in the test body and never invoked
// the SUT (Session.Add / Session.SetFilePriority / Session.GetHealth).
// Coverage holes that result are tracked separately under P1.

// Removed: TestRequesterMetadata — assigned struct fields, then
// asserted the same values back. Pure Go round-trip, no SUT involved.

// Removed: TestEffectivePriority — covered the deadline-based priority
// scheduler (EffectivePriority/SetDeadline), a dead feature removed
// along with bandwidth.go and the deadline column.
