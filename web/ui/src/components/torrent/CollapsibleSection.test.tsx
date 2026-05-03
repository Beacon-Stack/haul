// CollapsibleSection.test.tsx — exercise + lock-in the toggle behavior of
// the section wrapper used on the torrent detail page (Pieces / Peers /
// Trackers). The component itself is small; the value of the test is
// proving that the React + RTL + jsdom test setup actually mounts and
// interacts with components, so any future component test in Haul has
// a working pattern to copy.
//
// What's tested (behavior, not implementation):
//   - The label shows up.
//   - Children render when defaultOpen=true.
//   - Children are removed (not hidden) when toggled closed.
//   - Clicking the header toggles back open.
//   - The count badge appears only when count is supplied.

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import CollapsibleSection from "./CollapsibleSection";

describe("<CollapsibleSection>", () => {
  it("renders the label", () => {
    render(
      <CollapsibleSection label="Pieces">
        <div>contents</div>
      </CollapsibleSection>
    );
    // The component uppercases the label via CSS, so the DOM still has
    // the original case. Match the original.
    expect(screen.getByText("Pieces")).toBeInTheDocument();
  });

  it("shows children when defaultOpen=true (the default)", () => {
    render(
      <CollapsibleSection label="Peers">
        <div>peer-row</div>
      </CollapsibleSection>
    );
    expect(screen.getByText("peer-row")).toBeInTheDocument();
  });

  it("hides children when defaultOpen=false", () => {
    render(
      <CollapsibleSection label="Trackers" defaultOpen={false}>
        <div>tracker-row</div>
      </CollapsibleSection>
    );
    expect(screen.queryByText("tracker-row")).not.toBeInTheDocument();
  });

  it("toggles open when the header is clicked", async () => {
    const user = userEvent.setup();
    render(
      <CollapsibleSection label="Files" defaultOpen={false}>
        <div>file-row</div>
      </CollapsibleSection>
    );
    expect(screen.queryByText("file-row")).not.toBeInTheDocument();

    // The header is a <button>; RTL finds it by accessible name.
    const header = screen.getByRole("button", { name: /files/i });
    await user.click(header);
    expect(screen.getByText("file-row")).toBeInTheDocument();

    await user.click(header);
    expect(screen.queryByText("file-row")).not.toBeInTheDocument();
  });

  it("renders the count badge only when count is provided", () => {
    const { rerender } = render(
      <CollapsibleSection label="A">
        <div />
      </CollapsibleSection>
    );
    // No count -> no badge. Use a regex that wouldn't match the label.
    expect(screen.queryByText(/^\d+$/)).not.toBeInTheDocument();

    rerender(
      <CollapsibleSection label="A" count={42}>
        <div />
      </CollapsibleSection>
    );
    expect(screen.getByText("42")).toBeInTheDocument();
  });

  it("forceClosed wins over defaultOpen=true at initial render", () => {
    render(
      <CollapsibleSection label="X" defaultOpen={true} forceClosed>
        <div>hidden</div>
      </CollapsibleSection>
    );
    expect(screen.queryByText("hidden")).not.toBeInTheDocument();
  });
});
