package torrent

import (
	"testing"
	"time"
)

// ⚠ Regression guard for the "ETA is wildly wrong" bug. Before this suite
// existed, Info.DownloadRate was set to `stats.ConnStats.BytesReadData.Int64()`
// — a cumulative byte counter — and the ETA formula (remaining/rate) computed
// nonsense. If any of these tests fail, that class of bug is back.

const tolerance = 0.10 // 10% for convergence assertions

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// TestRateTracker_FirstSampleReturnsZeroAndSeeds verifies the initial
// sample cannot compute a rate (no prior measurement), returns 0, and
// seeds internal state for the next call.
func TestRateTracker_FirstSampleReturnsZeroAndSeeds(t *testing.T) {
	var rt rateTracker
	t0 := time.Unix(1_000_000, 0)
	got := rt.sample(0, t0)
	if got != 0 {
		t.Errorf("first sample: got %d, want 0", got)
	}
	if rt.lastAt != t0 {
		t.Error("first sample must seed lastAt")
	}
}

// TestRateTracker_ConstantRateConverges feeds a steady 1 MB/s stream and
// asserts the reported rate converges within 10% of truth after the EMA
// has had ~5 time-constants to settle.
func TestRateTracker_ConstantRateConverges(t *testing.T) {
	var rt rateTracker
	const bytesPerSec int64 = 1_000_000
	start := time.Unix(1_000_000, 0)

	// Seed + 20 samples at 1-second spacing. τ = 5s, so after 4τ = 20s
	// the EMA should be within ~2% of the true instant rate.
	var last int64
	for i := 0; i <= 20; i++ {
		bytes := bytesPerSec * int64(i)
		got := rt.sample(bytes, start.Add(time.Duration(i)*time.Second))
		if i == 0 && got != 0 {
			t.Fatalf("first sample must return 0, got %d", got)
		}
		last = got
	}

	diff := abs(float64(last-bytesPerSec) / float64(bytesPerSec))
	if diff > tolerance {
		t.Errorf("converged rate = %d, want within %.0f%% of %d (diff %.2f%%)",
			last, tolerance*100, bytesPerSec, diff*100)
	}
}

// TestRateTracker_BurstSmoothedOut confirms a single huge sample does NOT
// swing the displayed rate anywhere near the burst value — qBT's "doesn't
// flicker" property. After 10 samples at 1 MB/s, one sample that
// claims 100 MB/s should move the EMA to somewhere between 1 MB/s and
// a small multiple of it, never near 100 MB/s.
func TestRateTracker_BurstSmoothedOut(t *testing.T) {
	var rt rateTracker
	const steady int64 = 1_000_000
	start := time.Unix(1_000_000, 0)

	// Seed with 10 samples at 1 MB/s.
	for i := 0; i <= 10; i++ {
		rt.sample(steady*int64(i), start.Add(time.Duration(i)*time.Second))
	}

	// One burst sample: 100 MB delivered in the next 1 second.
	burstRate := steady * 100
	after := rt.sample(steady*10+burstRate, start.Add(11*time.Second))

	// With τ=5s and Δt=1s, α ≈ 0.18. New EMA ≈ 0.18 * 100MB + 0.82 * 1MB ≈ 18.8 MB/s.
	// It absolutely must NOT be near 100 MB/s on a single sample.
	if after > int64(float64(burstRate)*0.5) {
		t.Errorf("burst not smoothed: got %d, want << %d", after, burstRate)
	}
	if after <= steady {
		t.Errorf("burst had no effect: got %d, want > %d", after, steady)
	}
}

// TestRateTracker_StaleGapResets asserts that a long gap between samples
// (user closed the UI for a minute, torrent sat idle, etc.) resets the
// tracker rather than extrapolating a stale rate across the gap.
func TestRateTracker_StaleGapResets(t *testing.T) {
	var rt rateTracker
	start := time.Unix(1_000_000, 0)

	// Prime with a few samples so the EMA is non-zero.
	for i := 0; i <= 5; i++ {
		rt.sample(int64(i)*1_000_000, start.Add(time.Duration(i)*time.Second))
	}
	if rt.ema == 0 {
		t.Fatal("precondition: EMA should be non-zero after priming")
	}

	// Jump 60 seconds with no samples. Gap > 30s → reset.
	got := rt.sample(6_000_000, start.Add(65*time.Second))
	if got != 0 {
		t.Errorf("post-stale-gap sample: got %d, want 0 (reset)", got)
	}
	if rt.ema != 0 {
		t.Errorf("EMA not reset: %f", rt.ema)
	}
}

// TestRateTracker_ZeroDeltaNoOp — two samples at identical now must not
// divide by zero or mutate state into something nonsensical.
func TestRateTracker_ZeroDeltaNoOp(t *testing.T) {
	var rt rateTracker
	t0 := time.Unix(1_000_000, 0)
	rt.sample(0, t0)
	rt.sample(1_000_000, t0.Add(time.Second))
	priorEMA := rt.ema

	// Same timestamp, more bytes. Must not crash; must not change EMA. The
	// return type is int64, so we only need to assert the EMA didn't
	// mutate — float-NaN paths can't be reached through the int64 return.
	_ = rt.sample(2_000_000, t0.Add(time.Second))
	if rt.ema != priorEMA {
		t.Errorf("zero-delta mutated EMA: %f → %f", priorEMA, rt.ema)
	}
}

// TestRateTracker_NegativeByteDeltaResets guards against a torrent being
// removed and re-added under the same managedTorrent instance (shouldn't
// happen in practice, but cheap insurance against a negative delta).
func TestRateTracker_NegativeByteDeltaResets(t *testing.T) {
	var rt rateTracker
	t0 := time.Unix(1_000_000, 0)
	for i := 0; i <= 3; i++ {
		rt.sample(int64(i)*1_000_000, t0.Add(time.Duration(i)*time.Second))
	}
	// Backwards: bytes go from 3_000_000 to 500_000.
	got := rt.sample(500_000, t0.Add(4*time.Second))
	if got != 0 {
		t.Errorf("backwards bytes: got %d, want 0 (reset)", got)
	}
}

// TestRateTracker_DecayToZeroOnPause — once bytes stop growing (paused or
// completed), the EMA should decay toward zero, not stick at the last
// active rate forever. Matches qBT behavior.
func TestRateTracker_DecayToZeroOnPause(t *testing.T) {
	var rt rateTracker
	const steady int64 = 1_000_000
	start := time.Unix(1_000_000, 0)

	// Prime with 20 samples at 1 MB/s.
	for i := 0; i <= 20; i++ {
		rt.sample(steady*int64(i), start.Add(time.Duration(i)*time.Second))
	}
	peak := rt.ema
	if peak == 0 {
		t.Fatal("precondition: EMA should be non-zero")
	}

	// Now pause: bytes frozen at steady*20, keep sampling every second.
	// Each sample contributes an instant rate of 0. With τ=5s, after
	// ~20 more seconds (4τ) the EMA should be near zero.
	const frozen = 20
	for i := 1; i <= 20; i++ {
		rt.sample(steady*frozen, start.Add(time.Duration(frozen+i)*time.Second))
	}

	if rt.ema > peak*0.1 {
		t.Errorf("EMA did not decay on pause: %f (peak was %f)", rt.ema, peak)
	}
}
