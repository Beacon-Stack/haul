import { useState, type ReactNode } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";

// CollapsibleSection — a labelled, toggleable block shared by the detail
// page's Pieces / Peers / Trackers sections. Keeps the header row visually
// consistent with Haul's existing uppercase section labels (see the "PATHS"
// and "FILES" labels on TorrentDetail) and adds a count badge + chevron.
//
// Kept deliberately minimal: no animation on expand/collapse (instant toggle
// matches the rest of Haul's UI and avoids jank at the 1s poll cadence).

interface CollapsibleSectionProps {
  label: string;
  count?: number | string;
  defaultOpen?: boolean;
  children: ReactNode;
  // When set, forces the open state and disables user toggling. Used for the
  // piece bar which auto-collapses on 100% torrents but still lets the user
  // click the header to expand.
  forceClosed?: boolean;
}

export default function CollapsibleSection({
  label,
  count,
  defaultOpen = true,
  children,
  forceClosed = false,
}: CollapsibleSectionProps) {
  const [open, setOpen] = useState(defaultOpen && !forceClosed);

  return (
    <div style={{ marginBottom: 24 }}>
      <button
        onClick={() => setOpen((v) => !v)}
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          width: "100%",
          background: "none",
          border: "none",
          padding: "0 0 8px",
          cursor: "pointer",
          color: "var(--color-text-muted)",
          fontSize: 11,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.04em",
          textAlign: "left",
        }}
      >
        {open ? (
          <ChevronDown size={12} strokeWidth={2.5} />
        ) : (
          <ChevronRight size={12} strokeWidth={2.5} />
        )}
        <span>{label}</span>
        {count !== undefined && (
          <span
            style={{
              padding: "1px 7px",
              borderRadius: 9,
              background: "var(--color-bg-subtle)",
              color: "var(--color-text-secondary)",
              fontSize: 10,
              fontWeight: 600,
              letterSpacing: "0.02em",
            }}
          >
            {count}
          </span>
        )}
      </button>
      {open && <div>{children}</div>}
    </div>
  );
}
