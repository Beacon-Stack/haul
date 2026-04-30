package torrent

// queue_test.go — pins the max-active-downloads queue gate. The math is
// extracted into queueGateDecision so it can be exercised without
// spinning up real anacrolix torrents; the wiring (Pause clearing
// queuePaused, Resume re-running the gate, the runtime settings
// dispatch) is covered by stall_test.go's newTestSession + targeted
// state checks below.

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// equalSet asserts that two []string values contain the same hashes,
// ignoring order. The decision function preserves the input order, but
// the tests assert by content so a future refactor that returns a map
// or sorts differently doesn't drop legitimate behavior on the floor.
func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	return reflect.DeepEqual(ac, bc)
}

// TestQueueGateDecision_UnlimitedResumesEverything exercises the
// max <= 0 branch: when the cap is disabled, nothing should ever be
// queued, and any currently-queue-paused torrent must be resumed.
// This is the behaviour the user gets by leaving max_active_downloads
// blank or setting it to 0 in settings.
func TestQueueGateDecision_UnlimitedResumesEverything(t *testing.T) {
	cands := []queueCandidate{
		{hash: "a", paused: false, prio: 0},
		{hash: "b", paused: true, prio: 1}, // queue-paused leftover
		{hash: "c", paused: false, prio: 2},
	}
	resume, queue := queueGateDecision(cands, 0)

	if !equalSet(resume, []string{"b"}) {
		t.Errorf("resume = %v, want [b] (queue-paused leftover should be released)", resume)
	}
	if len(queue) != 0 {
		t.Errorf("queue = %v, want empty (max=0 means unlimited)", queue)
	}
}

// TestQueueGateDecision_BelowCapIsNoOp pins the no-op path: when fewer
// candidates exist than the cap allows, neither resume nor queue lists
// should fire. This matters because the gate runs on every Add /
// Remove / SetPriority — a noisy gate would publish useless
// TorrentStateChanged events on every action.
func TestQueueGateDecision_BelowCapIsNoOp(t *testing.T) {
	cands := []queueCandidate{
		{hash: "a", paused: false, prio: 0},
		{hash: "b", paused: false, prio: 1},
	}
	resume, queue := queueGateDecision(cands, 5)
	if len(resume) != 0 || len(queue) != 0 {
		t.Errorf("expected no-op below cap; got resume=%v queue=%v", resume, queue)
	}
}

// TestQueueGateDecision_OverCapPausesLowestPriority is the headline
// test. With cap=2 and 5 active candidates, the bottom three (by
// priority index) must be queue-paused. If the slot logic ever flips
// the comparison (i > max instead of i >= max), this catches it.
func TestQueueGateDecision_OverCapPausesLowestPriority(t *testing.T) {
	cands := []queueCandidate{
		{hash: "top1", paused: false, prio: 0},
		{hash: "top2", paused: false, prio: 1},
		{hash: "mid", paused: false, prio: 2},
		{hash: "low1", paused: false, prio: 3},
		{hash: "low2", paused: false, prio: 4},
	}
	resume, queue := queueGateDecision(cands, 2)

	if len(resume) != 0 {
		t.Errorf("resume = %v, want empty (none of the actives are paused)", resume)
	}
	if !equalSet(queue, []string{"mid", "low1", "low2"}) {
		t.Errorf("queue = %v, want [mid low1 low2]", queue)
	}
}

// TestQueueGateDecision_PromotesQueuedWhenSlotFree covers the "complete
// one, next one starts" flow. With cap=2 and one of the top two
// already paused, the next-priority paused torrent must be resumed —
// otherwise a completed torrent's slot stays empty until the next user
// action.
func TestQueueGateDecision_PromotesQueuedWhenSlotFree(t *testing.T) {
	// After completion: top1 is gone (filtered out by caller), top2
	// is active, mid is queue-paused. Cap=2, so mid should resume.
	cands := []queueCandidate{
		{hash: "top2", paused: false, prio: 0},
		{hash: "mid", paused: true, prio: 1},
		{hash: "low", paused: true, prio: 2},
	}
	resume, queue := queueGateDecision(cands, 2)

	if !equalSet(resume, []string{"mid"}) {
		t.Errorf("resume = %v, want [mid] (next-priority queued slot)", resume)
	}
	if len(queue) != 0 {
		t.Errorf("queue = %v, want empty (low is already queue-paused)", queue)
	}
}

// TestQueueGateDecision_PriorityRePromotion locks the drag-to-reorder
// behaviour: when the user bumps a queued torrent above the cap line,
// the gate must resume it AND queue-pause whatever it displaced. The
// caller is responsible for re-sorting before calling — this test
// asserts that given a correctly-sorted input, both halves of the
// promotion happen in one pass.
func TestQueueGateDecision_PriorityRePromotion(t *testing.T) {
	// User dragged "wanted" to the top. Caller passes the new order.
	// Old state: wanted was queued (paused=true), displaced was active.
	cands := []queueCandidate{
		{hash: "wanted", paused: true, prio: 0},
		{hash: "kept", paused: false, prio: 1},
		{hash: "displaced", paused: false, prio: 2},
	}
	resume, queue := queueGateDecision(cands, 2)

	if !equalSet(resume, []string{"wanted"}) {
		t.Errorf("resume = %v, want [wanted]", resume)
	}
	if !equalSet(queue, []string{"displaced"}) {
		t.Errorf("queue = %v, want [displaced]", queue)
	}
}

// TestQueueGateDecision_ExactCapIsNoOp is a boundary test: the
// candidate count exactly equal to max should not pause anything.
// Off-by-one at this boundary would cause spurious queueing when the
// user hasn't even hit the limit yet.
func TestQueueGateDecision_ExactCapIsNoOp(t *testing.T) {
	cands := []queueCandidate{
		{hash: "a", paused: false, prio: 0},
		{hash: "b", paused: false, prio: 1},
	}
	resume, queue := queueGateDecision(cands, 2)
	if len(resume) != 0 || len(queue) != 0 {
		t.Errorf("expected no-op at exact cap; got resume=%v queue=%v", resume, queue)
	}
}

// ── Wiring tests ─────────────────────────────────────────────────────────────
//
// These exercise the Session-level integration: that Pause clears
// queuePaused (sticky semantics), Resume clears it, and SetMaxActiveDownloads
// triggers a gate run. They use newTestSession (db=nil) plus directly-
// constructed managedTorrents so we don't have to wire up a real
// anacrolix client just to flip state bits.

// TestPause_ClearsQueuePausedFlag pins the user-pause sticky semantics:
// even if the gate had previously queue-paused this torrent, an
// explicit Pause must mark it user-paused so the next gate run leaves
// it alone. Without this, the user pauses a torrent → gate runs (e.g.
// because of Add) → gate sees paused=true,queuePaused=true → gate
// thinks "queued, fair game" → resumes the torrent the user just
// paused.
func TestPause_ClearsQueuePausedFlag(t *testing.T) {
	session := newTestSession(t)

	// Inject a managed torrent already in queue-paused state.
	hash := "deadbeef"
	mt := &managedTorrent{paused: true, queuePaused: true, ready: false}
	session.mu.Lock()
	session.torrents[hash] = mt
	session.mu.Unlock()

	// User explicitly pauses → queuePaused must clear.
	if err := session.Pause(hash); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	session.mu.RLock()
	gotPaused := session.torrents[hash].paused
	gotQueue := session.torrents[hash].queuePaused
	session.mu.RUnlock()

	if !gotPaused {
		t.Errorf("paused = false after Pause; want true")
	}
	if gotQueue {
		t.Errorf("queuePaused = true after Pause; want false (sticky user pause)")
	}
}

// TestResume_ClearsQueuePausedFlag is the symmetric assertion: after
// Resume, queuePaused must be false so the torrent shows as
// "downloading" rather than "queued" until the gate re-evaluates.
func TestResume_ClearsQueuePausedFlag(t *testing.T) {
	session := newTestSession(t)

	hash := "feedface"
	mt := &managedTorrent{paused: true, queuePaused: true, ready: false}
	session.mu.Lock()
	session.torrents[hash] = mt
	session.mu.Unlock()

	if err := session.Resume(hash); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	session.mu.RLock()
	gotPaused := session.torrents[hash].paused
	gotQueue := session.torrents[hash].queuePaused
	session.mu.RUnlock()

	if gotPaused {
		t.Errorf("paused = true after Resume; want false")
	}
	if gotQueue {
		t.Errorf("queuePaused = true after Resume; want false")
	}
}

// TestSetMaxActiveDownloads_RuntimeToggle pins the runtime-mutable
// contract for the cap setting. If this test fails, the settings UI
// toggle is a phantom write that doesn't take effect until restart —
// the same class of bug that motivated TestPauseOnComplete_RuntimeToggle.
func TestSetMaxActiveDownloads_RuntimeToggle(t *testing.T) {
	session := newTestSession(t)

	if got := session.MaxActiveDownloads(); got != 0 {
		t.Errorf("initial MaxActiveDownloads = %d, want 0 (cfg default)", got)
	}

	session.SetMaxActiveDownloads(3)
	if got := session.MaxActiveDownloads(); got != 3 {
		t.Errorf("MaxActiveDownloads after set = %d, want 3", got)
	}

	session.SetMaxActiveDownloads(0)
	if got := session.MaxActiveDownloads(); got != 0 {
		t.Errorf("MaxActiveDownloads after reset = %d, want 0", got)
	}
}

// TestEnforceMaxActiveDownloads_NoOpOnEmpty makes sure the gate
// gracefully handles a session with zero torrents. A panic or DB
// lookup error here would crash callers (Add, Pause, Remove all
// invoke the gate unconditionally).
func TestEnforceMaxActiveDownloads_NoOpOnEmpty(t *testing.T) {
	session := newTestSession(t)
	session.SetMaxActiveDownloads(2)

	// Should be a clean no-op — no panic, no bus.Publish, no DB query
	// (db is nil in test session).
	session.enforceMaxActiveDownloads(context.Background())
}
