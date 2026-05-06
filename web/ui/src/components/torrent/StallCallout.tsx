// StallCallout renders a banner above the torrent detail facts grid when
// the backend has classified the torrent as stalled. It surfaces the
// multi-level classification (level + reason + inactive duration) that
// /api/v1/torrents/{hash}/stall returns — without this callout the detail
// page only shows the rolled-up "Status: Stalled" badge, hiding why.
//
// Severity colour cues come from Haul's existing palette in index.css:
//   level 1 → --color-warning      (yellow — first reannounce trigger)
//   level 2 → --color-status-paused (orange — escalated, force DHT)
//   level 3 → --color-status-failed (red    — archive candidate)
//   level 4 → --color-status-failed (red, plus "DEAD TORRENT" label —
//             the headline marketing case: zero peers ever observed)
//
// Reason → human-readable copy lives in REASON_COPY below. Add cases here
// when stall.go grows new reason strings; the default fallback is the raw
// reason so unknown values still render rather than crash.

import type { StallInfo } from "@/api/torrents";

interface SeverityVisual {
  color: string;
  label: string;
}

// severityVisual maps the stall level to a colour token from index.css and
// a short label. Level 4 (no_peers_ever) gets a distinct label because it's
// not really an escalation rung — it's the "this torrent never had a single
// peer" classification, semantically separate from L1/L2/L3.
function severityVisual(level: StallInfo["level"]): SeverityVisual {
  switch (level) {
    case 1:
      return { color: "var(--color-warning)", label: "Stalled (Level 1)" };
    case 2:
      return { color: "var(--color-status-paused)", label: "Stalled (Level 2)" };
    case 3:
      return { color: "var(--color-status-failed)", label: "Stalled (Level 3)" };
    case 4:
      return { color: "var(--color-status-failed)", label: "Dead torrent" };
    default:
      // Level 0 should never reach this component (gated by stalled=true)
      // but render something legible if it does.
      return { color: "var(--color-status-stalled)", label: "Stalled" };
  }
}

const REASON_COPY: Record<string, string> = {
  no_peers_ever: "no peers ever observed",
  no_peers: "lost all peers",
  no_seeders: "peers connected but no seeders",
  no_data_received: "peers connected but no data received",
};

function reasonCopy(reason: string): string {
  return REASON_COPY[reason] ?? reason;
}

// formatInactive renders inactive_secs as "Xh Ym" / "Xm Ys" / "Xs" — the
// same convention TorrentList uses for its Active timer.
function formatInactive(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) {
    const m = Math.floor(secs / 60);
    const s = secs % 60;
    return s > 0 ? `${m}m ${s}s` : `${m}m`;
  }
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

interface Props {
  stall: StallInfo;
}

export default function StallCallout({ stall }: Props) {
  const sev = severityVisual(stall.level);
  return (
    <div
      role="status"
      aria-label="Stall classification"
      data-testid="stall-callout"
      style={{
        marginBottom: 16,
        padding: "10px 14px",
        borderRadius: 6,
        border: `1px solid ${sev.color}`,
        background: `color-mix(in srgb, ${sev.color} 10%, transparent)`,
        color: sev.color,
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 12,
        flexWrap: "wrap",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
        <span
          style={{
            fontSize: 11,
            fontWeight: 700,
            textTransform: "uppercase",
            letterSpacing: "0.06em",
            padding: "2px 8px",
            borderRadius: 4,
            background: sev.color,
            color: "var(--color-bg-base)",
            flexShrink: 0,
          }}
        >
          {sev.label}
        </span>
        <span style={{ fontSize: 13, fontWeight: 500 }}>{reasonCopy(stall.reason)}</span>
      </div>
      <span
        style={{
          fontSize: 12,
          fontWeight: 500,
          color: "var(--color-text-secondary)",
          fontFamily: "var(--font-family-mono)",
          flexShrink: 0,
        }}
      >
        Inactive for {formatInactive(stall.inactive_secs)}
      </span>
    </div>
  );
}
