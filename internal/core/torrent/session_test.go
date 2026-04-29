package torrent

import (
	"testing"
	"time"
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

func TestEffectivePriority(t *testing.T) {
	tests := []struct {
		name     string
		base     int
		deadline *time.Time
		wantLess bool // should effective priority be less (higher urgency) than base?
	}{
		{
			name:     "no deadline",
			base:     10,
			deadline: nil,
			wantLess: false,
		},
		{
			name:     "deadline in 30 minutes",
			base:     10,
			deadline: timePtr(time.Now().Add(30 * time.Minute)),
			wantLess: true,
		},
		{
			name:     "deadline in 3 hours",
			base:     10,
			deadline: timePtr(time.Now().Add(3 * time.Hour)),
			wantLess: true,
		},
		{
			name:     "deadline in 2 days",
			base:     10,
			deadline: timePtr(time.Now().Add(48 * time.Hour)),
			wantLess: true,
		},
		{
			name:     "deadline in 5 days — no boost",
			base:     10,
			deadline: timePtr(time.Now().Add(120 * time.Hour)),
			wantLess: false,
		},
		{
			name:     "overdue deadline — maximum urgency",
			base:     10,
			deadline: timePtr(time.Now().Add(-1 * time.Hour)),
			wantLess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective := EffectivePriority(tt.base, tt.deadline)
			if tt.wantLess && effective >= tt.base {
				t.Errorf("expected priority < %d, got %d", tt.base, effective)
			}
			if !tt.wantLess && effective != tt.base {
				t.Errorf("expected priority == %d, got %d", tt.base, effective)
			}
		})
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

func timePtr(t time.Time) *time.Time {
	return &t
}
