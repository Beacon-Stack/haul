// StallCallout tests pin the detail-page banner that surfaces the
// multi-level stall classification. Two render-time guards live here:
//
//   1. Renders the level + reason + inactive-time when stalled=true.
//   2. The level → severity mapping (esp. Level 4 = "Dead torrent") so
//      the no_peers_ever case stays visually distinct from L1/L2/L3.
//
// The "doesn't render" case lives in TorrentDetail.test.tsx — that's
// where the conditional render decision happens, not in this leaf.

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import StallCallout from "./StallCallout";
import type { StallInfo } from "@/api/torrents";

function makeStall(overrides: Partial<StallInfo> = {}): StallInfo {
  return {
    stalled: true,
    level: 1,
    inactive_secs: 180,
    reason: "no_data_received",
    ...overrides,
  };
}

describe("StallCallout", () => {
  it("renders the level label, reason copy, and formatted inactive duration", () => {
    render(<StallCallout stall={makeStall({ level: 1, inactive_secs: 185, reason: "no_data_received" })} />);
    expect(screen.getByText("Stalled (Level 1)")).toBeInTheDocument();
    expect(screen.getByText("peers connected but no data received")).toBeInTheDocument();
    expect(screen.getByText(/Inactive for 3m 5s/)).toBeInTheDocument();
  });

  it("uses 'Dead torrent' label for Level 4 (no_peers_ever)", () => {
    // This guards the marketing-positioned distinction between the
    // activity-escalation rungs (L1/L2/L3) and the dead-torrent class.
    // If someone changes severityVisual to render Level 4 as just
    // "Stalled (Level 4)", the headline classification disappears
    // from the UI and the bug report comes back.
    render(<StallCallout stall={makeStall({ level: 4, reason: "no_peers_ever", inactive_secs: 600 })} />);
    expect(screen.getByText("Dead torrent")).toBeInTheDocument();
    expect(screen.queryByText(/Level 4/)).not.toBeInTheDocument();
    expect(screen.getByText("no peers ever observed")).toBeInTheDocument();
  });

  it("renders unknown reason strings verbatim instead of crashing", () => {
    // If the backend grows a new reason and the frontend hasn't been
    // updated, fall back to the raw string. Catches the temptation to
    // silently render an empty span when the lookup misses.
    render(<StallCallout stall={makeStall({ reason: "freshly_invented_reason" })} />);
    expect(screen.getByText("freshly_invented_reason")).toBeInTheDocument();
  });

  it("formats sub-minute inactive durations as just seconds", () => {
    render(<StallCallout stall={makeStall({ inactive_secs: 42 })} />);
    expect(screen.getByText(/Inactive for 42s/)).toBeInTheDocument();
  });

  it("formats hour-scale inactive durations as Xh Ym", () => {
    render(<StallCallout stall={makeStall({ inactive_secs: 3 * 3600 + 25 * 60 + 10 })} />);
    expect(screen.getByText(/Inactive for 3h 25m/)).toBeInTheDocument();
  });
});
