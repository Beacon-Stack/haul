package torrent

import (
	"reflect"
	"testing"
)

func TestPieceBatcher_DoneKeepsOrderAndClears(t *testing.T) {
	b := newPieceBatcher()
	b.observe(7, "partial")
	b.observe(7, "complete")
	b.observe(2, "complete")

	if got, want := b.takeDone(), []int{7, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("takeDone = %v, want %v", got, want)
	}
	if got := b.takeDone(); len(got) != 0 {
		t.Fatalf("second takeDone = %v, want none", got)
	}
}

func TestPieceBatcher_ProgressOnlyWhenChanged(t *testing.T) {
	b := newPieceBatcher()
	b.observe(4, "partial")
	b.observe(9, "partial")
	levels := map[int]float64{4: 0.25, 9: 0.5}
	read := func(i int) float64 { return levels[i] }

	want := []PieceProgress{{Index: 4, Fraction: 0.25}, {Index: 9, Fraction: 0.5}}
	if got := b.takeProgress(read); !reflect.DeepEqual(got, want) {
		t.Fatalf("first takeProgress = %v, want %v", got, want)
	}
	if got := b.takeProgress(read); len(got) != 0 {
		t.Fatalf("unchanged takeProgress = %v, want none", got)
	}
	levels[9] = 0.75
	want = []PieceProgress{{Index: 9, Fraction: 0.75}}
	if got := b.takeProgress(read); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed takeProgress = %v, want %v", got, want)
	}
}

func TestPieceBatcher_FinishedOrFailedPiecesStopReportingProgress(t *testing.T) {
	b := newPieceBatcher()
	b.observe(1, "partial")
	b.observe(2, "partial")
	b.observe(1, "complete")
	b.observe(2, "missing") // failed its hash check

	if got := b.takeProgress(func(int) float64 { return 0.9 }); len(got) != 0 {
		t.Fatalf("takeProgress = %v, want none", got)
	}
}
