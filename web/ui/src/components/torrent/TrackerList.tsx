import { useState } from "react";
import { Copy, Check } from "lucide-react";
import type { TrackerInfo } from "@/api/torrents";

// TrackerList — static list of configured trackers from the torrent's
// metainfo. v1 does NOT show live announce status (last announce, reported
// peers, errors) — see plans/haul-torrent-detail-enhancements.md §6.1 for why.
//
// Each row: "tier 0 · udp://tracker.example.com:1337" with a copy-on-hover
// button. Mono font for URLs so they're easy to read.

interface TrackerListProps {
  trackers: TrackerInfo[];
}

export default function TrackerList({ trackers }: TrackerListProps) {
  if (trackers.length === 0) {
    return (
      <p
        style={{
          margin: 0,
          fontSize: 12,
          color: "var(--color-text-muted)",
          fontStyle: "italic",
        }}
      >
        No trackers configured (DHT / PEX only).
      </p>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      {trackers.map((tr, i) => (
        <TrackerRow key={`${tr.tier}-${tr.url}-${i}`} tracker={tr} />
      ))}
    </div>
  );
}

function TrackerRow({ tracker }: { tracker: TrackerInfo }) {
  const [copied, setCopied] = useState(false);
  const [hovered, setHovered] = useState(false);

  async function handleCopy(e: React.MouseEvent) {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(tracker.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard can fail in unsecured contexts — ignore silently
    }
  }

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        display: "flex",
        alignItems: "center",
        gap: 10,
        padding: "7px 12px",
        background: "var(--color-bg-surface)",
        border: "1px solid var(--color-border-subtle)",
        borderRadius: 6,
        fontSize: 12,
      }}
    >
      <span
        style={{
          fontSize: 10,
          fontWeight: 600,
          color: "var(--color-text-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          flexShrink: 0,
        }}
      >
        tier {tracker.tier}
      </span>
      <span
        style={{
          flex: 1,
          color: "var(--color-text-secondary)",
          fontFamily: "var(--font-family-mono)",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
        title={tracker.url}
      >
        {tracker.url}
      </span>
      <button
        onClick={handleCopy}
        style={{
          background: "none",
          border: "none",
          padding: 4,
          borderRadius: 4,
          cursor: "pointer",
          color: copied ? "var(--color-success)" : "var(--color-text-muted)",
          display: "flex",
          opacity: hovered || copied ? 1 : 0,
          transition: "opacity 120ms ease",
        }}
        title={copied ? "Copied!" : "Copy URL"}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
    </div>
  );
}
