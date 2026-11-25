package torrent

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// PieceUpdate is one batch of changes for a torrent's live piece view.
type PieceUpdate struct {
	// Done lists pieces that finished since the last update.
	Done []int `json:"done,omitempty"`
	// Progress gives how much of each in-progress piece has arrived. A
	// piece is listed only when its fraction changed.
	Progress []PieceProgress `json:"progress,omitempty"`
}

// PieceProgress is how much of one in-progress piece has been downloaded.
type PieceProgress struct {
	Index    int     `json:"i"`
	Fraction float64 `json:"f"` // 0..1
}

const (
	// pieceFlushInterval is how often the piece states are read: short
	// enough that a piece appears to land the moment it finishes, long
	// enough that a fast download sends a dozen messages a second rather
	// than hundreds. The states are read directly: it is one cheap call,
	// and simpler than following the library's per-piece change notices.
	pieceFlushInterval = 80 * time.Millisecond
	// pieceProgressEvery is how many flushes pass between progress
	// readings (about four a second).
	pieceProgressEvery = 3
	pieceReadyPoll     = 500 * time.Millisecond
)

// pieceTracker compares successive readings of a torrent's piece states
// and reports what changed.
type pieceTracker struct {
	complete map[int]struct{}
	active   map[int]float64 // in-progress piece → fraction last reported
	primed   bool
}

func newPieceTracker() *pieceTracker {
	return &pieceTracker{complete: make(map[int]struct{}), active: make(map[int]float64)}
}

// update takes the current piece states and returns the pieces that
// finished since the last call, lowest index first. With withProgress it
// also returns the fill level of in-progress pieces whose level changed.
// The first call only records what is already there: pieces that finished
// before watching began are not announced.
func (p *pieceTracker) update(runs []PieceStateRun, fraction func(index int) float64, withProgress bool) PieceUpdate {
	var out PieceUpdate
	complete := make(map[int]struct{}, len(p.complete))
	active := make(map[int]float64, len(p.active))

	index := 0
	for _, run := range runs {
		switch run.State {
		case "complete":
			for i := index; i < index+run.Length; i++ {
				complete[i] = struct{}{}
				if _, had := p.complete[i]; !had && p.primed {
					out.Done = append(out.Done, i)
				}
			}
		case "partial", "checking":
			for i := index; i < index+run.Length; i++ {
				last, seen := p.active[i]
				if !seen {
					last = -1
				}
				active[i] = last
			}
		}
		index += run.Length
	}
	p.complete, p.active, p.primed = complete, active, true

	if withProgress {
		for i, last := range p.active {
			f := fraction(i)
			if f == last {
				continue
			}
			p.active[i] = f
			out.Progress = append(out.Progress, PieceProgress{Index: i, Fraction: f})
		}
		sort.Slice(out.Progress, func(a, b int) bool { return out.Progress[a].Index < out.Progress[b].Index })
	}
	return out
}

// WatchPieces streams a torrent's piece changes to send until ctx ends or
// send fails: each piece as it finishes, and the fill level of pieces in
// progress. It waits for the torrent's metadata if that hasn't arrived.
func (s *Session) WatchPieces(ctx context.Context, hash string, send func(PieceUpdate) error) error {
	s.mu.RLock()
	mt, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}
	for !mt.ready.Load() || mt.t.Info() == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pieceReadyPoll):
		}
	}

	tracker := newPieceTracker()
	states := func() []PieceStateRun {
		runs := mt.t.PieceStateRuns()
		out := make([]PieceStateRun, len(runs))
		for i, r := range runs {
			out[i] = PieceStateRun{Length: r.Length, State: classifyPieceState(r.PieceState)}
		}
		return out
	}
	fraction := func(i int) float64 {
		length := mt.t.Piece(i).Info().Length()
		if length <= 0 {
			return 0
		}
		return 1 - float64(mt.t.PieceBytesMissing(i))/float64(length)
	}

	tracker.update(states(), fraction, false)

	ticker := time.NewTicker(pieceFlushInterval)
	defer ticker.Stop()
	for tick := 0; ; tick++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-mt.t.Closed():
			return nil
		case <-ticker.C:
		}
		update := tracker.update(states(), fraction, tick%pieceProgressEvery == 0)
		if len(update.Done) == 0 && len(update.Progress) == 0 {
			continue
		}
		if err := send(update); err != nil {
			return err
		}
	}
}
