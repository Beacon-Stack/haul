import { useState, useMemo, useRef, useEffect } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Plus, Pause, Play, Trash2, Search, Download, Upload, HardDrive, GripVertical, Shield, ShieldOff, FileUp, X } from "lucide-react";
import { toast } from "sonner";
import { useConfirm } from "@beacon-shared/ConfirmDialog";
import { useTorrents, useAddTorrent, useDeleteTorrent, usePauseTorrent, useResumeTorrent, useReorderTorrents, useSetTorrentPriority, useSetTorrentCategory, useAddTorrentTags, useRemoveTorrentTags, useSetTorrentLocation, type TorrentInfo } from "@/api/torrents";
import TorrentContextMenu, { type ContextMenuTarget } from "@/components/torrent/TorrentContextMenu";
import { useHealth } from "@/api/health";
import { useSettings } from "@/api/settings";
import { torrentVisual, visualByKey, type TorrentVisualKey } from "@/lib/torrentStatus";
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { validateTorrentFile, readTorrentFileAsDataURI, formatTorrentFileBytes } from "./torrentFile";
import Modal from "@beacon-shared/Modal";
import { formatBytes, formatSpeed } from "@/lib/format";

function formatETA(secs: number): string {
  if (secs <= 0) return "-";
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`;
}

// Filter types

const iconBtnStyle: React.CSSProperties = {
  background: "transparent",
  border: "none",
  color: "var(--color-text-muted)",
  cursor: "pointer",
  padding: 4,
  borderRadius: 4,
  display: "flex",
  alignItems: "center",
};

function StatCell({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div>
      <div style={{ fontSize: 10, color: "var(--color-text-muted)", textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 500, marginBottom: 1 }}>{label}</div>
      <div style={{ fontSize: 13, color: color || "var(--color-text-secondary)", fontWeight: 500 }}>{value}</div>
    </div>
  );
}

function formatTimeActive(addedAt: string): string {
  const diff = Math.floor((Date.now() - new Date(addedAt).getTime()) / 1000);
  if (diff < 60) return `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ${Math.floor((diff % 3600) / 60)}m`;
  return `${Math.floor(diff / 86400)}d ${Math.floor((diff % 86400) / 3600)}h`;
}

export default function TorrentCard({ t, draggable, onContextMenu }: {
  t: TorrentInfo;
  draggable: boolean;
  onContextMenu?: (t: TorrentInfo, x: number, y: number) => void;
}) {
  const pause = usePauseTorrent();
  const resume = useResumeTorrent();
  const del = useDeleteTorrent();
  const confirm = useConfirm();

  async function handleRemove() {
    if (
      await confirm({
        title: "Remove torrent",
        message: `Remove "${t.name}" from Haul? Files on disk are kept.`,
        confirmLabel: "Remove",
      })
    ) {
      del.mutate({ hash: t.info_hash, deleteFiles: false });
    }
  }

  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: t.info_hash });

  const isQueued = t.status === "queued";
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition: transition || "transform 200ms ease",
    // Dragging always wins; queued cards are visually demoted at 55% opacity
    // so the user can immediately see the active/queued boundary.
    opacity: isDragging ? 0.5 : isQueued ? 0.55 : 1,
    zIndex: isDragging ? 10 : 0,
    position: "relative" as const,
  };

  const visual = torrentVisual(t);
  const isDownloading = t.status === "downloading";
  const isSeeding = t.status === "seeding" || t.status === "completed";
  const amountLeft = t.size - t.downloaded;

  return (
    <div
      ref={setNodeRef}
      onContextMenu={(e) => {
        if (!onContextMenu) return;
        e.preventDefault();
        onContextMenu(t, e.clientX, e.clientY);
      }}
      style={{
        ...style,
        background: "var(--color-bg-surface)",
        border: isDragging ? "1px solid var(--color-accent)" : "1px solid var(--color-border-subtle)",
        borderRadius: 8,
        overflow: "hidden",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "12px 16px 0" }}>
        {/* Drag handle — only in manual sort mode */}
        {draggable && (
          <div
            {...attributes}
            {...listeners}
            style={{
              cursor: isDragging ? "grabbing" : "grab",
              color: "var(--color-text-muted)",
              display: "flex",
              alignItems: "center",
              padding: "2px 0",
              flexShrink: 0,
              touchAction: "none",
            }}
          >
            <GripVertical size={14} />
          </div>
        )}
        <Link
          to={`/torrents/${t.info_hash}`}
          style={{
            flex: 1,
            fontSize: 14,
            fontWeight: 500,
            color: "var(--color-text-primary)",
            textDecoration: "none",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            display: "block",
          }}
        >
          {t.name}
        </Link>

        <span style={{
          fontSize: 10,
          padding: "2px 8px",
          borderRadius: 4,
          fontWeight: 600,
          flexShrink: 0,
          background: `color-mix(in srgb, ${visual.color} 15%, transparent)`,
          color: visual.color,
          textTransform: "uppercase",
          letterSpacing: "0.04em",
        }}>
          {visual.label}
        </span>

        <div style={{ display: "flex", gap: 2, flexShrink: 0 }}>
          {t.status === "paused" ? (
            <button onClick={() => resume.mutate(t.info_hash)} title="Resume" style={iconBtnStyle}><Play size={14} /></button>
          ) : (
            <button onClick={() => pause.mutate(t.info_hash)} title="Pause" style={iconBtnStyle}><Pause size={14} /></button>
          )}
          <button onClick={handleRemove} title="Remove" style={{ ...iconBtnStyle, color: "var(--color-danger)" }}><Trash2 size={14} /></button>
        </div>
      </div>

      <div style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit, minmax(70px, 1fr))",
        gap: "4px 12px",
        padding: "10px 16px",
      }}>
        <StatCell label="Size" value={formatBytes(t.size)} />
        {isDownloading && <StatCell label="↓ Speed" value={formatSpeed(t.download_rate)} color={t.download_rate > 0 ? "var(--color-accent)" : "var(--color-text-muted)"} />}
        {isDownloading && <StatCell label="ETA" value={t.download_rate > 0 ? formatETA(t.eta) : "∞"} />}
        {isDownloading && <StatCell label="Left" value={formatBytes(amountLeft)} />}
        {(isDownloading || isSeeding) && <StatCell label="↑ Speed" value={formatSpeed(t.upload_rate)} color="var(--color-success)" />}
        <StatCell label="Seeds" value={String(t.seeds)} color={t.seeds <= 1 && isDownloading ? "var(--color-warning)" : undefined} />
        <StatCell label="Peers" value={String(t.peers)} />
        <StatCell label="Ratio" value={t.seed_ratio > 0 ? t.seed_ratio.toFixed(2) : "0.00"} />
        <StatCell label="Uploaded" value={formatBytes(t.uploaded)} />
        <StatCell label="Active" value={formatTimeActive(t.added_at)} />
      </div>

      <div style={{ padding: "0 16px 10px", display: "flex", alignItems: "center", gap: 10 }}>
        <div style={{ flex: 1, height: 6, borderRadius: 3, background: "var(--color-bg-subtle)" }}>
          <div style={{
            width: `${Math.min(t.progress * 100, 100)}%`,
            height: "100%",
            borderRadius: 3,
            background: visual.color,
            transition: "width 0.5s ease, background 0.3s ease",
          }} />
        </div>
        <span style={{ fontSize: 11, fontWeight: 600, color: visual.color, flexShrink: 0, minWidth: 40, textAlign: "right" }}>
          {(t.progress * 100).toFixed(1)}%
        </span>
      </div>
    </div>
  );
}
