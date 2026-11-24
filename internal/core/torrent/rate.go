package torrent

import (
	"math"
	"sync"
	"time"
)

// rateTracker converts a monotonically-increasing byte counter into a
// smoothed bytes-per-second rate using an exponential moving average. It
// is sampled on every getInfo call; no background ticker is required.
//
// The EMA uses a 5-second time constant so that α depends on the wall-clock
// gap between samples: short gaps barely nudge the displayed value (matches
// qBittorrent's "doesn't flicker" UX), large gaps overwrite most of the
// stored rate, and gaps over 30s reset the tracker entirely rather than
// extrapolating from stale data.
//
// It exists because anacrolix/torrent's Stats() only exposes cumulative
// counters; treating those as a rate produces nonsense ETAs.
type rateTracker struct {
	mu        sync.Mutex
	lastBytes int64
	lastAt    time.Time
	ema       float64 // bytes per second
}

const (
	rateTimeConstantSecs = 5.0  // τ — responsiveness vs smoothing trade-off
	rateStaleGapSecs     = 30.0 // sampling gap beyond which we reset the tracker
)

// sample records a new cumulative byte count at the given wall-clock time
// and returns the current smoothed rate in bytes/sec.
//
// The first call after construction or a reset returns 0 (we need at least
// two samples to measure an interval). Subsequent calls blend the new
// instant rate into the EMA using α = 1 - exp(-Δt/τ).
func (r *rateTracker) sample(bytes int64, now time.Time) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// First sample or long gap — seed state and report zero. Long gaps
	// reset so we don't carry over a stale rate from hours ago.
	if r.lastAt.IsZero() || now.Sub(r.lastAt).Seconds() > rateStaleGapSecs {
		r.lastBytes = bytes
		r.lastAt = now
		r.ema = 0
		return 0
	}

	delta := now.Sub(r.lastAt).Seconds()
	if delta <= 0 {
		// Same millisecond or clock went backwards — keep existing EMA,
		// don't divide by zero.
		return int64(r.ema)
	}

	// Byte counter should only grow. Guard against resets or going
	// backwards (e.g. torrent removed and re-added) by treating negative
	// deltas as a reset.
	if bytes < r.lastBytes {
		r.lastBytes = bytes
		r.lastAt = now
		r.ema = 0
		return 0
	}

	instant := float64(bytes-r.lastBytes) / delta
	alpha := 1 - math.Exp(-delta/rateTimeConstantSecs)
	r.ema = alpha*instant + (1-alpha)*r.ema
	r.lastBytes = bytes
	r.lastAt = now
	return int64(r.ema)
}
