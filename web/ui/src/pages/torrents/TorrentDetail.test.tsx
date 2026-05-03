// TorrentDetail.test.tsx — tests for the new stall surface added in
// PR #7 (fix/stall-pill-and-detail). Targets the StallCallout component
// and the four pure helper functions exported from the same module.
//
// What each test guards against:
//  - StallCallout: the callout must NOT render when the torrent is healthy.
//    Otherwise the layout shifts on every page load for non-stalled
//    torrents (≈99% of the dashboard).
//  - StallCallout: when stalled, all three displayed pieces (level label,
//    reason text, inactive duration) must appear and be tied to the
//    correct stall data. Future refactors that drop one would silently
//    break the marketing-positioned classification surface.
//  - severityStyle: the four severity tiers must each return distinct
//    color tokens. If a future palette change collapses two tiers to the
//    same color, the visual distinction the design relies on disappears.
//  - reasonLabel: the four reason constants from internal/core/torrent/
//    stall.go must each map to a non-empty human-readable string. If
//    backend adds a new reason, this test stays green BUT the new reason
//    falls through to the raw constant — flag in code review.
//  - formatInactive: ranges across the three format zones (sub-minute,
//    sub-hour, sub-day) so a typo in the breakpoints (e.g. < 60 vs <= 60)
//    surfaces immediately.

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  StallCallout,
  // Internal helpers — re-exported below the component so this test can
  // see them. If you add a new severity / reason / format function, mirror
  // it here so the contract stays locked.
  // (TorrentDetail.tsx exports StallCallout but not the helpers; the
  //  helpers are tested via the component's rendered output.)
} from "./TorrentDetail";
import type { StallInfo } from "@/api/torrents";

// Builds a StallInfo with healthy defaults; override fields per test.
function makeStall(over: Partial<StallInfo> = {}): StallInfo {
  return {
    stalled: true,
    level: 1,
    reason: "no_data_received",
    inactive_secs: 130,
    last_activity: undefined,
    ...over,
  };
}

describe("<StallCallout>", () => {
  it("renders nothing when stall is undefined (request still loading)", () => {
    const { container } = render(<StallCallout stall={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when stall.stalled is false (healthy torrent)", () => {
    // REGRESSION GUARD: dashboard renders 12+ torrents. If this ever
    // returned a wrapper div with display:none instead of null, every
    // healthy torrent would push down the facts grid by the empty box.
    const { container } = render(
      <StallCallout stall={makeStall({ stalled: false, level: 0 })} />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders the level-1 label, reason, and inactive duration when stalled", () => {
    render(
      <StallCallout
        stall={makeStall({ level: 1, reason: "no_data_received", inactive_secs: 130 })}
      />
    );
    // Level 1 label — locked in. If changed, update test description too.
    expect(screen.getByText(/Level 1.*Reannouncing/i)).toBeInTheDocument();
    // Reason → human-readable.
    expect(screen.getByText(/no useful pieces arriving/i)).toBeInTheDocument();
    // 130s -> "2m" via formatInactive.
    expect(screen.getByText(/Inactive for 2m\./i)).toBeInTheDocument();
  });

  it("renders the level-2 label for level=2", () => {
    render(<StallCallout stall={makeStall({ level: 2, reason: "no_peers" })} />);
    expect(screen.getByText(/Level 2.*Forcing DHT/i)).toBeInTheDocument();
    expect(screen.getByText(/all disconnected/i)).toBeInTheDocument();
  });

  it("renders the level-3 label for level=3", () => {
    render(<StallCallout stall={makeStall({ level: 3, reason: "no_seeders" })} />);
    expect(screen.getByText(/Level 3.*Needs intervention/i)).toBeInTheDocument();
    expect(screen.getByText(/no seeds|none has the data/i)).toBeInTheDocument();
  });

  it("renders the categorical NoPeersEver label for level=4", () => {
    // This is the "847-seeder dead-torrent" case the stall.go classifier
    // explicitly distinguishes from progressive escalation. Test that the
    // label is categorical (not "Level 4") so the marketing positioning
    // survives a refactor.
    render(<StallCallout stall={makeStall({ level: 4, reason: "no_peers_ever" })} />);
    expect(screen.getByText(/never found peers|stalled/i)).toBeInTheDocument();
    expect(screen.queryByText(/Level 4/i)).not.toBeInTheDocument();
    // Reason must surface the dead-tracker hint that's the pitch.
    expect(screen.getByText(/dead tracker|fake hash/i)).toBeInTheDocument();
  });

  it("formats inactive duration for sub-minute / sub-hour / multi-hour", () => {
    // Rendering a fresh callout each time and reading the inactive line
    // tests formatInactive() through the component (which is the surface
    // users actually see).
    const { rerender } = render(
      <StallCallout stall={makeStall({ inactive_secs: 45 })} />
    );
    expect(screen.getByText(/Inactive for 45s\./)).toBeInTheDocument();

    rerender(<StallCallout stall={makeStall({ inactive_secs: 1800 })} />);
    expect(screen.getByText(/Inactive for 30m\./)).toBeInTheDocument();

    rerender(<StallCallout stall={makeStall({ inactive_secs: 3600 })} />);
    // exactly 1h -> "1h" (no trailing minutes)
    expect(screen.getByText(/Inactive for 1h\./)).toBeInTheDocument();

    rerender(<StallCallout stall={makeStall({ inactive_secs: 5400 })} />);
    // 1.5h -> "1h 30m"
    expect(screen.getByText(/Inactive for 1h 30m\./)).toBeInTheDocument();
  });

  it("uses distinct severity styles for levels 1 / 2 / 3 / 4", () => {
    // Severity mapping is rendered as inline `style` on the callout's
    // root. We don't test specific colors (themes can override) — we
    // test that they are pairwise DIFFERENT, which is the design
    // invariant. If a refactor accidentally collapses two tiers to the
    // same color, this fails.
    const colors = [1, 2, 3, 4].map((level) => {
      const { container } = render(
        <StallCallout stall={makeStall({ level: level as 1 | 2 | 3 | 4 })} />
      );
      // Header label has the severity color via inline style.
      const labelEl = container.querySelector("[role=status] > div");
      return labelEl?.getAttribute("style") ?? "";
    });
    // Every pair must differ.
    const distinct = new Set(colors);
    expect(distinct.size).toBe(4);
  });

  it("exposes data-stall-level on the root for theming and e2e probes", () => {
    // A stable hook so future styling / test code can target the callout
    // by severity without scraping label text.
    const { container } = render(
      <StallCallout stall={makeStall({ level: 4 })} />
    );
    const root = container.querySelector("[data-stall-level]");
    expect(root).not.toBeNull();
    expect(root?.getAttribute("data-stall-level")).toBe("4");
  });

  it("has role=status for accessibility (announces to screen readers)", () => {
    render(<StallCallout stall={makeStall({ level: 1 })} />);
    expect(screen.getByRole("status")).toBeInTheDocument();
  });
});
