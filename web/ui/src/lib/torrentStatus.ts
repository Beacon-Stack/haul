// Canonical visual representation of a torrent's state.
//
// This file is the SINGLE source of truth for how a torrent maps to a status
// label and colour in the UI. Every place that renders a torrent badge,
// progress bar colour, status text, or filter pill must call torrentVisual()
// — never inline its own switch statement.
//
// Why this matters: before extracting this helper, three independent functions
// in TorrentList.tsx (STATUS_COLORS map, progressBarColor(), statusLabel())
// had drifted out of sync. A "downloading" torrent could show one blue in the
// filter pill, a different blue in the row's text, and yet another colour in
// the progress bar — and the row would flip yellow whenever a connection
// briefly dropped to ≤1 seed mid-download. The canonical helper kills both
// classes of bug by computing the visual ONCE per torrent.
//
// Stall detection is server-side. We never re-derive "stalled" from
// download_rate or seed counts on the client — the backend's stall.go
// classifier owns that decision and we read t.stalled.

import type { TorrentInfo } from "@/api/torrents";

export type TorrentVisualKey =
  | "downloading"
  | "stalled"
  | "seeding"
  | "completed"
  | "paused"
  | "queued"
  | "failed";

export interface TorrentVisual {
  key: TorrentVisualKey;
  label: string;
  // CSS variable string. Use directly in style={{ color: visual.color }}.
  color: string;
}

// Static map for the visuals — uses var() refs so themes can override.
const VISUALS: Record<TorrentVisualKey, TorrentVisual> = {
  downloading: { key: "downloading", label: "Downloading", color: "var(--color-status-downloading)" },
  stalled:     { key: "stalled",     label: "Stalled",     color: "var(--color-status-stalled)" },
  seeding:     { key: "seeding",     label: "Seeding",     color: "var(--color-status-seeding)" },
  completed:   { key: "completed",   label: "Completed",   color: "var(--color-status-completed)" },
  paused:      { key: "paused",      label: "Paused",      color: "var(--color-status-paused)" },
  queued:      { key: "queued",      label: "Queued",      color: "var(--color-status-queued)" },
  failed:      { key: "failed",      label: "Failed",      color: "var(--color-status-failed)" },
};

// torrentVisual returns the canonical (label, color) for a torrent.
// The "stalled" override only applies when the backend has flagged it.
export function torrentVisual(t: Pick<TorrentInfo, "status" | "stalled">): TorrentVisual {
  if (t.status === "downloading" && t.stalled) {
    return VISUALS.stalled;
  }
  switch (t.status) {
    case "downloading":
      return VISUALS.downloading;
    case "seeding":
      return VISUALS.seeding;
    case "completed":
      return VISUALS.completed;
    case "paused":
      return VISUALS.paused;
    case "queued":
      return VISUALS.queued;
    case "failed":
      return VISUALS.failed;
    default:
      // Unknown status — fall back to "downloading" colours rather than crash.
      // If you see this in the wild, something on the backend has emitted a
      // new status string and this helper needs an update.
      return VISUALS.downloading;
  }
}

// visualByKey is for filter-pill rendering where you have a filter key
// (not a torrent), and want to colour the pill consistently with what
// matching rows look like.
export function visualByKey(key: TorrentVisualKey): TorrentVisual {
  return VISUALS[key];
}
