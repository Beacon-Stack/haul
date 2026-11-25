package torrent

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// PieceUpdate is one batch of changes for a torrent's live piece view.
type PieceUpdate struct {
	// Done lists pieces that finished since the last update, in the order
	// they finished.
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
	// pieceFlushInterval batches finished pieces: short enough that a
	// piece appears to land the moment it finishes, long enough that a
	// fast download sends a dozen messages a second rather than hundreds.
	pieceFlushInterval = 80 * time.Millisecond
	// pieceProgressEvery is how many flushes pass between progress
	// readings (about four a second).
	pieceProgressEvery = 3
	pieceReadyPoll     = 500 * time.Millisecond
)

// pieceBatcher collects piece state changes between flushes.
type pieceBatcher struct {
	mu      sync.Mutex
	done    []int
	partial map[int]float64 // in-progress piece → fraction last reported (-1: none yet)
}

func newPieceBatcher() *pieceBatcher {
	return &pieceBatcher{partial: make(map[int]float64)}
}

// observe records a piece's new state ("complete", "partial", "checking"
// or "missing", as classifyPieceState names them).
func (b *pieceBatcher) observe(index int, state string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch state {
	case "complete":
		delete(b.partial, index)
		b.done = append(b.done, index)
	case "partial":
		if _, ok := b.partial[index]; !ok {
			b.partial[index] = -1
		}
	case "missing":
		// A piece that failed its hash check goes back to missing.
		delete(b.partial, index)
	}
}

// takeDone returns the pieces finished since the last call.
func (b *pieceBatcher) takeDone() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.done
	b.done = nil
	return out
}

// takeProgress reads each in-progress piece's fraction and returns the
// ones that changed since they were last reported.
func (b *pieceBatcher) takeProgress(fraction func(index int) float64) []PieceProgress {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []PieceProgress
	for index, last := range b.partial {
		f := fraction(index)
		if f == last {
			continue
		}
		b.partial[index] = f
		out = append(out, PieceProgress{Index: index, Fraction: f})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
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

	batch := newPieceBatcher()

	// Subscribe before reading the current state so no change is missed.
	sub := mt.t.SubscribePieceStateChanges()
	defer sub.Close()
	index := 0
	for _, run := range mt.t.PieceStateRuns() {
		if classifyPieceState(run.PieceState) == "partial" {
			for i := 0; i < run.Length; i++ {
				batch.observe(index+i, "partial")
			}
		}
		index += run.Length
	}

	// The subscription queues without limit, so it is drained on its own
	// goroutine and never waits on a slow client.
	go func() {
		for change := range sub.Values {
			batch.observe(change.Index, classifyPieceState(change.PieceState))
		}
	}()

	fraction := func(i int) float64 {
		length := mt.t.Piece(i).Info().Length()
		if length <= 0 {
			return 0
		}
		return 1 - float64(mt.t.PieceBytesMissing(i))/float64(length)
	}

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
		update := PieceUpdate{Done: batch.takeDone()}
		if tick%pieceProgressEvery == 0 {
			update.Progress = batch.takeProgress(fraction)
		}
		if len(update.Done) == 0 && len(update.Progress) == 0 {
			continue
		}
		if err := send(update); err != nil {
			return err
		}
	}
}
