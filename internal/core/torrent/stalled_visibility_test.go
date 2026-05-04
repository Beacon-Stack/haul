package torrent

// stalled_visibility_test.go covers the "level 3 = pause+tag, not drop"
// behavior change. The forward path (CheckStalls → level 3) requires
// torrent metadata to fire (Class 2 of stall.go), which is hard to
// fake without a real metainfo. So we test the *post-level-3* state
// (stalledAt set, paused, 'stalled' tag) and verify Resume cleans it
// up — these are the user-visible guarantees that matter.

import (
	"testing"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// TestStalledTorrent_StaysInList: after the stall watcher escalates a
// torrent to level 3, it MUST still be present in s.torrents (was the
// original bug — the old code called Drop()+delete()). Without this,
// stalled torrents vanished from /api/v1/torrents and the user had no
// surface to see they existed.
func TestStalledTorrent_StaysInList(t *testing.T) {
	session := newTestSession(t)

	// Add a real anacrolix torrent so torrentInfo() doesn't blow up.
	var hash metainfo.Hash
	copy(hash[:], []byte("stalled-still-visible0"))
	spec := &lt.TorrentSpec{
		AddTorrentOpts: lt.AddTorrentOpts{InfoHash: hash},
		DisplayName:    "stalled-test",
	}
	if _, _, err := session.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("adding test torrent: %v", err)
	}
	tHandle, _ := session.client.Torrent(hash)

	// Simulate the post-level-3 state: paused, stalledAt set, tag added.
	stalledAt := time.Now().Add(-10 * time.Minute)
	session.mu.Lock()
	session.torrents[hash.HexString()] = &managedTorrent{
		t:         tHandle,
		paused:    true,
		stalledAt: &stalledAt,
		tags:      []string{"tv", "stalled"},
		addedAt:   time.Now().Add(-2 * time.Hour),
	}
	session.mu.Unlock()

	// The torrent must still be in the map — that's the headline
	// regression guarantee. Drop()+delete() is gone.
	session.mu.RLock()
	_, present := session.torrents[hash.HexString()]
	session.mu.RUnlock()
	if !present {
		t.Fatal("stalled torrent missing from session.torrents — the old Drop()+delete() bug is back")
	}

	// And Get must surface it with stalledAt set so the UI can render
	// the "needs attention" badge.
	info, err := session.Get(hash.HexString())
	if err != nil {
		t.Fatalf("Get on stalled torrent failed: %v", err)
	}
	if info.StalledAt == nil {
		t.Fatal("Info.StalledAt is nil — UI won't render the badge")
	}
	if !info.StalledAt.Equal(stalledAt) {
		t.Errorf("Info.StalledAt = %v, want %v", info.StalledAt, stalledAt)
	}
	hasTag := false
	for _, tag := range info.Tags {
		if tag == "stalled" {
			hasTag = true
			break
		}
	}
	if !hasTag {
		t.Errorf("Info.Tags missing 'stalled' tag (got %v) — tag filter won't surface this torrent", info.Tags)
	}
}

// TestResume_ClearsStalledState: when a user clicks Resume on an
// auto-paused torrent, the stalled markers must come off — otherwise
// the torrent stays on the "needs attention" rail forever even
// though the user took action. The watcher will re-apply them on the
// next escalation if peers stay zero.
//
// Uses a synthetic managedTorrent with t=nil — Pause and Resume
// both have nil guards (see "nil guard keeps unit tests..." comment
// in session.go) so the state-flip path is exercised without needing
// real anacrolix metadata, which is unavailable for a fresh test
// torrent.
func TestResume_ClearsStalledState(t *testing.T) {
	session := newTestSession(t)

	hash := "resumeclearsstalledhashcafef00d000000000"

	stalledAt := time.Now().Add(-1 * time.Hour)
	session.mu.Lock()
	session.torrents[hash] = &managedTorrent{
		t:         nil,
		paused:    true,
		stalledAt: &stalledAt,
		tags:      []string{"movies", "stalled"},
		addedAt:   time.Now().Add(-2 * time.Hour),
	}
	session.mu.Unlock()

	if err := session.Resume(hash); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	session.mu.RLock()
	mt := session.torrents[hash]
	if mt == nil {
		t.Fatal("torrent gone from map after Resume")
	}
	gotStalledAt := mt.stalledAt
	gotPaused := mt.paused
	gotTags := append([]string(nil), mt.tags...)
	session.mu.RUnlock()

	if gotStalledAt != nil {
		t.Errorf("stalledAt = %v, want nil after Resume", gotStalledAt)
	}
	if gotPaused {
		t.Error("paused = true after Resume, want false")
	}
	for _, tag := range gotTags {
		if tag == "stalled" {
			t.Errorf("Resume should remove the 'stalled' tag, but tags = %v", gotTags)
		}
	}
	// Non-stalled tags must survive — Resume clears the auto-applied
	// 'stalled' tag only, not user tags or requester tags.
	hasMovies := false
	for _, tag := range gotTags {
		if tag == "movies" {
			hasMovies = true
		}
	}
	if !hasMovies {
		t.Errorf("Resume removed the 'movies' tag too — should only remove 'stalled'. Got %v", gotTags)
	}
}

// TestContainsString_RemoveString: tiny but worth pinning so a
// future "let's use slices.Contains" refactor doesn't quietly change
// the case-sensitivity contract.
func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b", "stalled"}, "stalled") {
		t.Error("expected true for present element")
	}
	if containsString([]string{"a", "b"}, "stalled") {
		t.Error("expected false for absent element")
	}
	if containsString(nil, "stalled") {
		t.Error("nil slice must return false, not panic")
	}
	// Case-sensitive — 'Stalled' != 'stalled'.
	if containsString([]string{"Stalled"}, "stalled") {
		t.Error("comparison must be case-sensitive")
	}
}

func TestRemoveString(t *testing.T) {
	got := removeString([]string{"a", "stalled", "b", "stalled"}, "stalled")
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("removeString length: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("removeString[%d] = %q, want %q", i, g, want[i])
		}
	}

	// Removing absent element returns the original entries.
	got2 := removeString([]string{"a", "b"}, "stalled")
	if len(got2) != 2 || got2[0] != "a" || got2[1] != "b" {
		t.Errorf("removeString of absent element altered slice: %v", got2)
	}

	// Empty / nil input: must return empty, not panic.
	got3 := removeString(nil, "stalled")
	if len(got3) != 0 {
		t.Errorf("removeString(nil) = %v, want empty", got3)
	}
}
