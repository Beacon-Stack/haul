// torrentStatus.test.ts — pins the canonical helper that every status
// badge / progress bar color / filter pill in Haul reads from. The big
// comment block at the top of torrentStatus.ts explains the bug this
// file guards against: three independent inline switches in TorrentList
// drifted out of sync, one of them flipping torrents yellow on every
// brief connection blip mid-download. We extracted torrentVisual() as
// the single source of truth — these tests lock that contract in.
//
// Tests verify *behavior* (what label / color comes back for a given
// (status, stalled) pair). They are NOT snapshot tests; if the assertion
// starts failing, investigate why before changing it.

import { describe, it, expect } from "vitest";
import { torrentVisual, visualByKey } from "./torrentStatus";
import type { TorrentInfo } from "@/api/torrents";

// makeT builds a partial TorrentInfo for the helper. torrentVisual only
// reads .status and .stalled — the cast keeps the test focused.
function makeT(status: TorrentInfo["status"], stalled: boolean = false): Pick<TorrentInfo, "status" | "stalled"> {
  return { status, stalled };
}

describe("torrentVisual", () => {
  it("returns the matching VISUAL for plain statuses", () => {
    expect(torrentVisual(makeT("downloading")).key).toBe("downloading");
    expect(torrentVisual(makeT("seeding")).key).toBe("seeding");
    expect(torrentVisual(makeT("completed")).key).toBe("completed");
    expect(torrentVisual(makeT("paused")).key).toBe("paused");
    expect(torrentVisual(makeT("queued")).key).toBe("queued");
    expect(torrentVisual(makeT("failed")).key).toBe("failed");
  });

  it("overrides downloading -> stalled when the backend flags stalled=true", () => {
    // REGRESSION GUARD: the WHOLE POINT of the helper. If this assertion
    // ever fails, the badge color and filter-pill counts will diverge
    // exactly the way the original 3-switch drift did. Don't 'fix' by
    // changing the assertion — fix the helper.
    const v = torrentVisual(makeT("downloading", true));
    expect(v.key).toBe("stalled");
    expect(v.label).toBe("Stalled");
  });

  it("does NOT flip non-downloading statuses to stalled even if the flag is true", () => {
    // Defensive: backend should never set stalled=true for non-downloading
    // statuses (it's a documented invariant on the TorrentInfo type), but
    // if it ever does, the helper must still respect the status field.
    expect(torrentVisual(makeT("paused", true)).key).toBe("paused");
    expect(torrentVisual(makeT("seeding", true)).key).toBe("seeding");
    expect(torrentVisual(makeT("queued", true)).key).toBe("queued");
  });

  it("falls back to 'downloading' for unknown statuses rather than crashing", () => {
    // The helper has a documented fallback for forward-compat with new
    // server statuses. Test it via cast; the type system would normally
    // forbid this.
    const unknown = torrentVisual({ status: "future-state" as unknown as TorrentInfo["status"], stalled: false });
    expect(unknown.key).toBe("downloading");
  });

  it("each visual carries a non-empty label and a CSS-variable color", () => {
    // Locks in the contract that every visual object is renderable.
    // If a future refactor strips a label or color, the layout breaks.
    const keys = ["downloading", "stalled", "seeding", "completed", "paused", "queued", "failed"] as const;
    for (const k of keys) {
      const v = visualByKey(k);
      expect(v.key).toBe(k);
      expect(v.label.length).toBeGreaterThan(0);
      expect(v.color).toMatch(/^var\(--color-/);
    }
  });
});
