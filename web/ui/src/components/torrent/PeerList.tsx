import { useMemo, useState } from "react";
import { Lock, ArrowUp, ArrowDown } from "lucide-react";
import type { PeerInfo } from "@/api/torrents";

// PeerList — compact table of connected peers, 6 columns, sortable by
// download/upload/progress. See plans/haul-torrent-detail-enhancements.md §5.
//
// No GeoIP, no encryption-type inspection, no per-block transfer detail —
// those are all future work. This is the "glanceable who am I talking to"
// table, not a debugging surface.

type SortField = "download_rate" | "upload_rate" | "progress";
type SortDir = "asc" | "desc";

interface PeerListProps {
  peers: PeerInfo[];
}

function formatSpeed(b: number): string {
  if (b <= 0) return "—";
  if (b < 1024) return `${b} B/s`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB/s`;
  return `${(b / (1024 * 1024)).toFixed(1)} MB/s`;
}

export default function PeerList({ peers }: PeerListProps) {
  const [sort, setSort] = useState<{ field: SortField; dir: SortDir } | null>({
    field: "download_rate",
    dir: "desc",
  });

  const sorted = useMemo(() => {
    if (!sort) return peers;
    return [...peers].sort((a, b) => {
      const av = a[sort.field];
      const bv = b[sort.field];
      return sort.dir === "desc" ? bv - av : av - bv;
    });
  }, [peers, sort]);

  function toggleSort(field: SortField) {
    setSort((prev) => {
      if (prev?.field !== field) return { field, dir: "desc" };
      if (prev.dir === "desc") return { field, dir: "asc" };
      return null; // third click → natural order
    });
  }

  if (peers.length === 0) {
    return (
      <p
        style={{
          margin: 0,
          fontSize: 12,
          color: "var(--color-text-muted)",
          fontStyle: "italic",
        }}
      >
        No connected peers.
      </p>
    );
  }

  // Cap the visible peer list height. A connected peer row measures ~34px
  // (8px padding top/bottom + 14-16px text). 20 rows × 34 = 680, plus the
  // ~28px header. When a torrent has more than 20 peers the container
  // scrolls internally rather than pushing the Trackers / Meta / Paths /
  // Files sections off the bottom of the page.
  const MAX_VISIBLE_ROWS = 20;
  const ROW_HEIGHT_PX = 34;
  const HEADER_HEIGHT_PX = 28;
  const maxHeight = MAX_VISIBLE_ROWS * ROW_HEIGHT_PX + HEADER_HEIGHT_PX;

  const thStyle: React.CSSProperties = {
    textAlign: "left",
    padding: "6px 10px",
    fontSize: 10,
    fontWeight: 600,
    letterSpacing: "0.06em",
    textTransform: "uppercase",
    color: "var(--color-text-muted)",
    borderBottom: "1px solid var(--color-border-subtle)",
    whiteSpace: "nowrap",
    // Sticky header — stays at the top while the rows scroll. Needs an
    // opaque background or content scrolls through it.
    position: "sticky",
    top: 0,
    background: "var(--color-bg-surface)",
    zIndex: 1,
  };

  const sortableThStyle = (field: SortField): React.CSSProperties => ({
    ...thStyle,
    cursor: "pointer",
    userSelect: "none",
    color: sort?.field === field ? "var(--color-accent)" : "var(--color-text-muted)",
  });

  const sortIcon = (field: SortField) => {
    if (sort?.field !== field) return null;
    return sort.dir === "desc" ? (
      <ArrowDown size={9} strokeWidth={2.5} />
    ) : (
      <ArrowUp size={9} strokeWidth={2.5} />
    );
  };

  return (
    <div
      style={{
        background: "var(--color-bg-surface)",
        border: "1px solid var(--color-border-subtle)",
        borderRadius: 6,
        maxHeight,
        overflowY: "auto",
      }}
    >
      <table style={{ width: "100%", borderCollapse: "collapse" }}>
        <thead>
          <tr>
            <th style={thStyle}>Address</th>
            <th style={thStyle}>Client</th>
            <th style={thStyle}>Flags</th>
            <th style={sortableThStyle("progress")} onClick={() => toggleSort("progress")}>
              <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                Progress {sortIcon("progress")}
              </span>
            </th>
            <th style={sortableThStyle("download_rate")} onClick={() => toggleSort("download_rate")}>
              <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                ↓ {sortIcon("download_rate")}
              </span>
            </th>
            <th style={sortableThStyle("upload_rate")} onClick={() => toggleSort("upload_rate")}>
              <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                ↑ {sortIcon("upload_rate")}
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((p) => (
            <PeerRow key={p.addr} peer={p} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function PeerRow({ peer }: { peer: PeerInfo }) {
  const tdStyle: React.CSSProperties = {
    padding: "8px 10px",
    fontSize: 12,
    color: "var(--color-text-secondary)",
    borderBottom: "1px solid var(--color-border-subtle)",
    whiteSpace: "nowrap",
  };

  return (
    <tr>
      <td style={{ ...tdStyle, fontFamily: "var(--font-family-mono)", color: "var(--color-text-primary)" }}>
        {peer.addr}
      </td>
      <td
        style={{
          ...tdStyle,
          maxWidth: 180,
          overflow: "hidden",
          textOverflow: "ellipsis",
        }}
        title={peer.client}
      >
        {peer.client}
      </td>
      <td style={tdStyle}>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          {peer.encrypted && (
            <Lock
              size={10}
              strokeWidth={2}
              style={{ color: "var(--color-success)" }}
              aria-label="encrypted"
            />
          )}
          <span
            style={{
              fontSize: 10,
              fontWeight: 600,
              padding: "1px 5px",
              borderRadius: 3,
              background: "var(--color-bg-subtle)",
              color: "var(--color-text-muted)",
              textTransform: "uppercase",
              letterSpacing: "0.03em",
            }}
          >
            {peer.network}
          </span>
        </span>
      </td>
      <td style={tdStyle}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div
            style={{
              width: 50,
              height: 4,
              borderRadius: 2,
              background: "var(--color-bg-subtle)",
              flexShrink: 0,
            }}
          >
            <div
              style={{
                width: `${Math.round(peer.progress * 100)}%`,
                height: "100%",
                borderRadius: 2,
                background: "var(--color-status-downloading)",
              }}
            />
          </div>
          <span style={{ fontSize: 11, color: "var(--color-text-muted)", minWidth: 32 }}>
            {Math.round(peer.progress * 100)}%
          </span>
        </div>
      </td>
      <td
        style={{
          ...tdStyle,
          color:
            peer.download_rate > 0 ? "var(--color-status-downloading)" : "var(--color-text-muted)",
          fontWeight: 500,
        }}
      >
        {formatSpeed(peer.download_rate)}
      </td>
      <td
        style={{
          ...tdStyle,
          color: peer.upload_rate > 0 ? "var(--color-success)" : "var(--color-text-muted)",
          fontWeight: 500,
        }}
      >
        {formatSpeed(peer.upload_rate)}
      </td>
    </tr>
  );
}
