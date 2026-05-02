import { useState, useMemo, useRef } from "react";
import { Link } from "react-router-dom";
import { Plus, Pause, Play, Trash2, Search, Download, Upload, HardDrive, GripVertical, Shield, ShieldOff, FileUp, X } from "lucide-react";
import { toast } from "sonner";
import { useConfirm } from "@beacon-shared/ConfirmDialog";
import { useTorrents, useAddTorrent, useDeleteTorrent, usePauseTorrent, useResumeTorrent, useReorderTorrents, useSetTorrentPriority, type TorrentInfo } from "@/api/torrents";
import { useHealth } from "@/api/health";
import { useSettings } from "@/api/settings";
import { torrentVisual, visualByKey, type TorrentVisualKey } from "@/lib/torrentStatus";
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { validateTorrentFile, readTorrentFileAsDataURI, formatBytes as formatFileBytes } from "./torrentFile";

function formatSpeed(b: number): string {
  if (b <= 0) return "-";
  if (b < 1024) return `${b} B/s`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB/s`;
  return `${(b / (1024 * 1024)).toFixed(1)} MB/s`;
}

function formatBytes(b: number): string {
  if (b <= 0) return "-";
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`;
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatETA(secs: number): string {
  if (secs <= 0) return "-";
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`;
}

// ── Filter types ──────────────────────────────────────────────────────────

// "active" is the new default — it shows every torrent except the ones the
// user is done with. "completed" pulls all finished torrents (literal
// `completed` status AND `seeding` status) into one bucket so the user can
// archive-browse them on demand. Seeding stays as its own filter so the user
// can quickly see what's currently uploading.
type StatusFilter =
  | "active"
  | "downloading"
  | "seeding"
  | "completed"
  | "paused"
  | "stalled"
  | "failed"
  | "queued";

type SortField = "manual" | "name" | "added" | "size" | "progress" | "speed";

const STATUS_FILTERS: { key: StatusFilter; label: string; color: string }[] = [
  { key: "active",      label: "Active",      color: "var(--color-text-secondary)" },
  { key: "downloading", label: "Downloading", color: visualByKey("downloading").color },
  { key: "seeding",     label: "Seeding",     color: visualByKey("seeding").color },
  { key: "completed",   label: "Completed",   color: visualByKey("completed").color },
  { key: "paused",      label: "Paused",      color: visualByKey("paused").color },
  { key: "stalled",     label: "Stalled",     color: visualByKey("stalled").color },
  { key: "failed",      label: "Failed",      color: visualByKey("failed").color },
  { key: "queued",      label: "Queued",      color: visualByKey("queued").color },
];

// isFinished reports whether a torrent has finished downloading. Both
// `completed` (= 100% paused, the default when pause_on_complete=true) and
// `seeding` (= 100% actively uploading) count as finished — both should be
// hidden from the default Active view.
function isFinished(t: TorrentInfo): boolean {
  return t.status === "completed" || t.status === "seeding";
}

function matchesStatus(t: TorrentInfo, filter: StatusFilter): boolean {
  switch (filter) {
    case "active":      return !isFinished(t);
    case "downloading": return t.status === "downloading";
    case "seeding":     return t.status === "seeding";
    case "completed":   return isFinished(t);
    case "paused":      return t.status === "paused";
    case "stalled":     return t.status === "downloading" && t.stalled;
    case "failed":      return t.status === "failed";
    case "queued":      return t.status === "queued";
  }
}

function countForStatus(torrents: TorrentInfo[], filter: StatusFilter): number {
  return torrents.filter((t) => matchesStatus(t, filter)).length;
}

function sortTorrents(torrents: TorrentInfo[], field: SortField): TorrentInfo[] {
  if (field === "manual") return torrents; // preserve server order (by priority)
  return [...torrents].sort((a, b) => {
    switch (field) {
      case "name": return a.name.localeCompare(b.name);
      case "added": return new Date(b.added_at).getTime() - new Date(a.added_at).getTime();
      case "size": return b.size - a.size;
      case "progress": return b.progress - a.progress;
      case "speed": return (b.download_rate + b.upload_rate) - (a.download_rate + a.upload_rate);
      default: return 0;
    }
  });
}

// ── Add Modal ──────────────────────────────────────────────────────────────

function AddModal({ onClose }: { onClose: () => void }) {
  const [uri, setUri] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const addTorrent = useAddTorrent();

  // handlePickedFile is the single entry point for both the OS picker
  // (via Browse) and drag-drop. Runs the synchronous validation immediately
  // so the user sees errors before any async work.
  function handlePickedFile(picked: File) {
    setFileError(null);
    const v = validateTorrentFile(picked);
    if (!v.ok) {
      setFile(null);
      setFileError(v.error);
      return;
    }
    setFile(picked);
    setUri(""); // file takes precedence — clear URI to avoid ambiguity
  }

  function clearFile() {
    setFile(null);
    setFileError(null);
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  async function handleAdd() {
    const trimmedUri = uri.trim();
    // File takes precedence when both are somehow present.
    if (file) {
      try {
        const dataURI = await readTorrentFileAsDataURI(file);
        addTorrent.mutate(
          { uri: dataURI },
          {
            onSuccess: (t) => {
              toast.success(`Added: ${t.name || file.name}`);
              onClose();
            },
            onError: (e) => toast.error((e as Error).message),
          }
        );
      } catch (e) {
        setFileError((e as Error).message);
      }
      return;
    }
    if (!trimmedUri) return;
    addTorrent.mutate(
      { uri: trimmedUri },
      {
        onSuccess: (t) => {
          toast.success(`Added: ${t.name || "torrent"}`);
          onClose();
        },
        onError: (e) => toast.error((e as Error).message),
      }
    );
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragOver(false);
    const dropped = e.dataTransfer.files?.[0];
    if (dropped) handlePickedFile(dropped);
  }

  const canSubmit = (file !== null || uri.trim().length > 0) && !fileError && !addTorrent.isPending;

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 100 }}
      onClick={onClose}
    >
      <div
        style={{ background: "var(--color-bg-elevated)", borderRadius: 10, border: "1px solid var(--color-border-default)", padding: 24, width: 480, maxWidth: "90vw", boxShadow: "var(--shadow-modal)" }}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 style={{ margin: "0 0 16px", fontSize: 16, fontWeight: 600, color: "var(--color-text-primary)" }}>Add Torrent</h3>

        <input
          value={uri}
          onChange={(e) => setUri(e.target.value)}
          placeholder="Magnet link or .torrent URL"
          autoFocus
          disabled={file !== null}
          onKeyDown={(e) => e.key === "Enter" && canSubmit && handleAdd()}
          style={{
            width: "100%",
            padding: "10px 12px",
            borderRadius: 6,
            border: "1px solid var(--color-border-default)",
            background: file !== null ? "var(--color-bg-subtle)" : "var(--color-bg-surface)",
            color: file !== null ? "var(--color-text-muted)" : "var(--color-text-primary)",
            fontSize: 13,
            fontFamily: "var(--font-family-mono)",
            outline: "none",
            opacity: file !== null ? 0.6 : 1,
          }}
        />

        <div style={{ display: "flex", alignItems: "center", gap: 12, margin: "14px 0", color: "var(--color-text-muted)", fontSize: 11 }}>
          <div style={{ flex: 1, height: 1, background: "var(--color-border-subtle)" }} />
          <span style={{ textTransform: "uppercase", letterSpacing: 0.5 }}>or</span>
          <div style={{ flex: 1, height: 1, background: "var(--color-border-subtle)" }} />
        </div>

        {/* Themed dropzone — drag a .torrent here OR click to open the OS picker. */}
        <div
          onClick={() => fileInputRef.current?.click()}
          onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
          style={{
            border: `2px dashed ${dragOver ? "var(--color-accent)" : "var(--color-border-default)"}`,
            borderRadius: 8,
            padding: 20,
            textAlign: "center",
            cursor: "pointer",
            background: dragOver ? "var(--color-accent-muted)" : "var(--color-bg-subtle)",
            transition: "border-color 120ms, background 120ms",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 6,
          }}
        >
          <FileUp size={20} style={{ color: "var(--color-text-secondary)" }} />
          <div style={{ fontSize: 13, color: "var(--color-text-primary)" }}>
            Drop a .torrent file here
          </div>
          <div style={{ fontSize: 11, color: "var(--color-text-muted)" }}>
            or <span style={{ color: "var(--color-accent)", textDecoration: "underline" }}>browse</span> your computer
          </div>
        </div>

        <input
          ref={fileInputRef}
          type="file"
          accept=".torrent,application/x-bittorrent"
          style={{ display: "none" }}
          onChange={(e) => {
            const picked = e.target.files?.[0];
            if (picked) handlePickedFile(picked);
          }}
        />

        {file && !fileError && (
          <div
            style={{
              marginTop: 10,
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: "8px 10px",
              borderRadius: 6,
              background: "var(--color-bg-surface)",
              border: "1px solid var(--color-border-subtle)",
              fontSize: 12,
              color: "var(--color-text-primary)",
            }}
          >
            <FileUp size={14} style={{ color: "var(--color-success)" }} />
            <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontFamily: "var(--font-family-mono)" }}>
              {file.name}
            </span>
            <span style={{ color: "var(--color-text-muted)" }}>{formatFileBytes(file.size)}</span>
            <button
              onClick={clearFile}
              aria-label="Remove file"
              style={{ background: "transparent", border: "none", color: "var(--color-text-muted)", cursor: "pointer", padding: 2, display: "flex", alignItems: "center" }}
            >
              <X size={14} />
            </button>
          </div>
        )}

        {fileError && (
          <div
            style={{
              marginTop: 10,
              padding: "8px 10px",
              borderRadius: 6,
              background: "var(--color-bg-surface)",
              border: "1px solid var(--color-danger)",
              fontSize: 12,
              color: "var(--color-danger)",
            }}
          >
            {fileError}
          </div>
        )}

        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
          <button
            onClick={onClose}
            style={{ padding: "7px 14px", borderRadius: 6, border: "1px solid var(--color-border-default)", background: "transparent", color: "var(--color-text-secondary)", fontSize: 13, cursor: "pointer" }}
          >
            Cancel
          </button>
          <button
            onClick={handleAdd}
            disabled={!canSubmit}
            style={{
              padding: "7px 16px",
              borderRadius: 6,
              border: "none",
              background: !canSubmit ? "var(--color-bg-subtle)" : "var(--color-accent)",
              color: !canSubmit ? "var(--color-text-muted)" : "var(--color-accent-fg)",
              fontSize: 13,
              fontWeight: 500,
              cursor: !canSubmit ? "not-allowed" : "pointer",
            }}
          >
            {addTorrent.isPending ? "Adding..." : "Add"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Torrent Row ────────────────────────────────────────────────────────────

// All status colour and label rendering goes through torrentVisual() in
// @/lib/torrentStatus. Do NOT add inline colour switches here — drift between
// the badge, the bar, and the filter pills is exactly the bug that helper
// was extracted to fix.

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

function TorrentCard({ t, draggable }: { t: TorrentInfo; draggable: boolean }) {
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
      style={{
        ...style,
        background: "var(--color-bg-surface)",
        border: isDragging ? "1px solid var(--color-accent)" : "1px solid var(--color-border-subtle)",
        borderRadius: 8,
        overflow: "hidden",
      }}
    >
      {/* Row 1: Drag handle + Name + Status badge + Actions */}
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

      {/* Row 2: Stats grid — evenly distributed */}
      <div style={{
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit, minmax(70px, 1fr))",
        gap: "4px 12px",
        padding: "10px 16px",
      }}>
        <StatCell label="Size" value={formatBytes(t.size)} />
        {isDownloading && <StatCell label="↓ Speed" value={t.download_rate > 0 ? formatSpeed(t.download_rate) : "0"} color={t.download_rate > 0 ? "var(--color-accent)" : "var(--color-text-muted)"} />}
        {isDownloading && <StatCell label="ETA" value={t.download_rate > 0 ? formatETA(t.eta) : "∞"} />}
        {isDownloading && <StatCell label="Left" value={amountLeft > 0 ? formatBytes(amountLeft) : "0"} />}
        {(isDownloading || isSeeding) && <StatCell label="↑ Speed" value={formatSpeed(t.upload_rate)} color="var(--color-success)" />}
        <StatCell label="Seeds" value={String(t.seeds)} color={t.seeds <= 1 && isDownloading ? "var(--color-warning)" : undefined} />
        <StatCell label="Peers" value={String(t.peers)} />
        <StatCell label="Ratio" value={t.seed_ratio > 0 ? t.seed_ratio.toFixed(2) : "0.00"} />
        <StatCell label="Uploaded" value={formatBytes(t.uploaded)} />
        <StatCell label="Active" value={formatTimeActive(t.added_at)} />
      </div>

      {/* Row 3: Progress bar with percentage */}
      <div style={{ padding: "0 16px 0", display: "flex", alignItems: "center", gap: 10 }}>
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

      {/* Bottom spacer */}
      <div style={{ height: 10 }} />
    </div>
  );
}

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

const selectStyle: React.CSSProperties = {
  padding: "5px 10px",
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "var(--color-bg-elevated)",
  color: "var(--color-text-primary)",
  fontSize: 12,
  outline: "none",
};

// ── Page ───────────────────────────────────────────────────────────────────

export default function TorrentList() {
  const { data: torrents, isLoading } = useTorrents();
  const { data: settings } = useSettings();
  const [showAdd, setShowAdd] = useState(false);
  const [statusFilters, setStatusFilters] = useState<Set<StatusFilter>>(new Set(["active"]));
  const [categoryFilters, setCategoryFilters] = useState<Set<string>>(new Set());
  const [tagFilters, setTagFilters] = useState<Set<string>>(new Set());
  const [sortField, setSortField] = useState<SortField>("manual");
  const [search, setSearch] = useState("");
  const [showFilters, setShowFilters] = useState(false);
  const [localOrder, setLocalOrder] = useState<string[] | null>(null);

  // Derive unique categories and tags from torrents.
  const categories = useMemo(() => {
    if (!torrents) return [];
    const cats = new Set<string>();
    for (const t of torrents) {
      if (t.category) cats.add(t.category);
    }
    return [...cats].sort();
  }, [torrents]);

  const tags = useMemo(() => {
    if (!torrents) return [];
    const tagSet = new Set<string>();
    for (const t of torrents) {
      for (const tag of t.tags ?? []) tagSet.add(tag);
    }
    return [...tagSet].sort();
  }, [torrents]);

  // Apply all filters.
  const filtered = useMemo(() => {
    if (!torrents) return [];
    let result = torrents;

    // Status filter (multi-select). Unlike a generic "all" sentinel that
    // would mean "skip filtering", "active" is a real predicate (everything
    // except finished torrents) so we always apply the filter regardless of
    // which keys are selected.
    result = result.filter((t) => {
      for (const f of statusFilters) {
        if (matchesStatus(t, f)) return true;
      }
      return false;
    });

    // Category filter (multi-select).
    if (categoryFilters.size > 0) {
      result = result.filter((t) => categoryFilters.has(t.category));
    }

    // Tag filter (multi-select).
    if (tagFilters.size > 0) {
      result = result.filter((t) => t.tags?.some((tag) => tagFilters.has(tag)));
    }

    // Search.
    if (search) {
      const q = search.toLowerCase();
      result = result.filter((t) => t.name.toLowerCase().includes(q));
    }

    let sorted = sortTorrents(result, sortField);

    // Apply optimistic local order from drag-and-drop.
    if (localOrder && sortField === "manual") {
      const orderMap = new Map(localOrder.map((h, i) => [h, i]));
      sorted = [...sorted].sort((a, b) => {
        const ai = orderMap.get(a.info_hash) ?? 9999;
        const bi = orderMap.get(b.info_hash) ?? 9999;
        return ai - bi;
      });
    }

    return sorted;
  }, [torrents, statusFilters, categoryFilters, tagFilters, sortField, search, localOrder]);

  const reorder = useReorderTorrents();
  const setPriority = useSetTorrentPriority();

  // maxActiveDownloads: 0 means unlimited (no divider).
  const maxActiveDownloads = useMemo(() => {
    const raw = settings?.["max_active_downloads"];
    if (!raw) return 0;
    const n = Number(raw);
    return isNaN(n) ? 0 : n;
  }, [settings]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const hashes = filtered.map((t) => t.info_hash);
    const oldIndex = hashes.indexOf(active.id as string);
    const newIndex = hashes.indexOf(over.id as string);
    if (oldIndex === -1 || newIndex === -1) return;

    const newOrder = arrayMove(hashes, oldIndex, newIndex);

    // Optimistic local reorder.
    setLocalOrder(newOrder);
    setSortField("manual");

    // Use the per-torrent priority endpoint so the backend can re-run the
    // queue gate immediately (the old bulk /reorder doesn't trigger it).
    // Priority is 1-indexed positional rank in the new order.
    setPriority.mutate({ hash: active.id as string, priority: newIndex + 1 });
    // Also send the full order for consistency with the existing reorder path.
    reorder.mutate(newOrder);
  }

  const { data: health } = useHealth();
  const totalDown = torrents?.reduce((sum, t) => sum + t.download_rate, 0) ?? 0;
  const totalUp = torrents?.reduce((sum, t) => sum + t.upload_rate, 0) ?? 0;
  const downloading = torrents?.filter((t) => t.status === "downloading").length ?? 0;
  const seeding = torrents?.filter((t) => t.status === "seeding" || t.status === "completed").length ?? 0;
  const paused = torrents?.filter((t) => t.status === "paused").length ?? 0;
  const totalSize = torrents?.reduce((sum, t) => sum + t.size, 0) ?? 0;

  return (
    <div style={{ padding: 24, maxWidth: 1000 }}>
      {/* Header */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>Dashboard</h1>
          {health && (
            <span style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              fontSize: 10,
              fontWeight: 600,
              padding: "3px 8px",
              borderRadius: 4,
              background: health.vpn_active
                ? "color-mix(in srgb, var(--color-success) 15%, transparent)"
                : "color-mix(in srgb, var(--color-danger) 15%, transparent)",
              color: health.vpn_active ? "var(--color-success)" : "var(--color-danger)",
            }}>
              {health.vpn_active ? <Shield size={11} /> : <ShieldOff size={11} />}
              {health.vpn_active ? "VPN" : "No VPN"}
            </span>
          )}
        </div>
        <button
          onClick={() => setShowAdd(true)}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            padding: "7px 14px",
            borderRadius: 6,
            border: "none",
            background: "var(--color-accent)",
            color: "var(--color-accent-fg)",
            fontSize: 13,
            fontWeight: 500,
            cursor: "pointer",
          }}
        >
          <Plus size={14} /> Add Torrent
        </button>
      </div>

      {/* Global stats cards */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(140px, 1fr))", gap: 10, marginBottom: 16 }}>
        {[
          { label: "Download", value: formatSpeed(totalDown), icon: Download, color: "var(--color-accent)" },
          { label: "Upload", value: formatSpeed(totalUp), icon: Upload, color: "var(--color-success)" },
          { label: "Active", value: String(downloading), icon: Download, color: "var(--color-accent)" },
          { label: "Seeding", value: String(seeding), icon: Upload, color: "var(--color-success)" },
          { label: "Paused", value: String(paused), icon: Pause, color: "var(--color-status-paused)" },
          { label: "Total Size", value: formatBytes(totalSize), icon: HardDrive, color: "var(--color-text-secondary)" },
        ].map(({ label, value, icon: Icon, color }) => (
          <div
            key={label}
            style={{
              background: "var(--color-bg-surface)",
              border: "1px solid var(--color-border-subtle)",
              borderRadius: 8,
              padding: "12px 14px",
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
              <Icon size={12} style={{ color }} />
              <span style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)" }}>{label}</span>
            </div>
            <div style={{ fontSize: 18, fontWeight: 700, color: "var(--color-text-primary)" }}>{value}</div>
          </div>
        ))}
      </div>

      {/* Status filter pills */}
      <div style={{ display: "flex", gap: 6, marginBottom: 12, flexWrap: "wrap" }}>
        {STATUS_FILTERS.map(({ key, label, color }) => {
          const count = torrents ? countForStatus(torrents, key) : 0;
          const active = statusFilters.has(key);
          return (
            <button
              key={key}
              onClick={() => {
                setStatusFilters((prev) => {
                  // "active" is the catch-all default — clicking it always
                  // resets to single-select Active. Any other key toggles
                  // and removes "active" so the two are mutually exclusive.
                  if (key === "active") return new Set(["active"]);
                  const next = new Set(prev);
                  next.delete("active");
                  if (next.has(key)) {
                    next.delete(key);
                  } else {
                    next.add(key);
                  }
                  return next.size === 0 ? new Set(["active"]) : next;
                });
              }}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 6,
                padding: "5px 12px",
                borderRadius: 6,
                border: active ? `1px solid ${color}` : "1px solid var(--color-border-default)",
                background: active ? `color-mix(in srgb, ${color} 12%, transparent)` : "transparent",
                color: active ? color : "var(--color-text-muted)",
                fontSize: 12,
                fontWeight: active ? 600 : 400,
                cursor: "pointer",
                opacity: count === 0 && key !== "active" ? 0.5 : 1,
              }}
            >
              <div style={{
                width: 6,
                height: 6,
                borderRadius: "50%",
                background: active ? color : "var(--color-text-muted)",
              }} />
              {label}
              <span style={{
                fontSize: 10,
                fontWeight: 600,
                color: active ? color : "var(--color-text-muted)",
                minWidth: 14,
                textAlign: "center",
              }}>
                {count}
              </span>
            </button>
          );
        })}
      </div>

      {/* Controls row */}
      <div style={{ display: "flex", gap: 8, marginBottom: showFilters ? 0 : 16, alignItems: "center", flexWrap: "wrap" }}>
        {/* Sort dropdown */}
        <select
          value={sortField}
          onChange={(e) => { setSortField(e.target.value as SortField); setLocalOrder(null); }}
          style={selectStyle}
        >
          <option value="manual">Sort: Manual</option>
          <option value="added">Sort: Added</option>
          <option value="name">Sort: Name</option>
          <option value="size">Sort: Size</option>
          <option value="progress">Sort: Progress</option>
          <option value="speed">Sort: Speed</option>
        </select>

        {/* Spacer */}
        <div style={{ flex: 1 }} />

        {/* Search */}
        <div style={{ position: "relative" }}>
          <Search size={13} style={{ position: "absolute", left: 8, top: "50%", transform: "translateY(-50%)", color: "var(--color-text-muted)" }} />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search..."
            style={{
              padding: "5px 10px 5px 28px",
              borderRadius: 6,
              border: "1px solid var(--color-border-default)",
              background: "var(--color-bg-elevated)",
              color: "var(--color-text-primary)",
              fontSize: 12,
              outline: "none",
              width: 160,
            }}
          />
        </div>

        {/* Filters toggle */}
        <button
            onClick={() => setShowFilters((v) => !v)}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 5,
              padding: "5px 10px",
              borderRadius: 6,
              border: `1px solid ${showFilters || categoryFilters.size > 0 || tagFilters.size > 0 ? "var(--color-accent)" : "var(--color-border-default)"}`,
              background: showFilters || categoryFilters.size > 0 || tagFilters.size > 0 ? "var(--color-accent-muted)" : "transparent",
              color: showFilters || categoryFilters.size > 0 || tagFilters.size > 0 ? "var(--color-accent)" : "var(--color-text-muted)",
              fontSize: 12,
              cursor: "pointer",
              fontWeight: categoryFilters.size > 0 || tagFilters.size > 0 ? 600 : 400,
            }}
          >
            {showFilters ? "▲" : "▼"} Filters
            {(categoryFilters.size > 0 || tagFilters.size > 0) && (
              <span style={{ fontSize: 9, background: "var(--color-accent)", color: "#fff", borderRadius: "50%", width: 14, height: 14, display: "flex", alignItems: "center", justifyContent: "center", fontWeight: 700 }}>
                {categoryFilters.size + tagFilters.size}
              </span>
            )}
          </button>

        {/* Result count */}
        <span style={{ fontSize: 11, color: "var(--color-text-muted)" }}>
          {filtered.length}{!statusFilters.has("active") || statusFilters.size > 1 || categoryFilters.size > 0 || tagFilters.size > 0 || search ? ` of ${torrents?.length ?? 0}` : ""} torrent{filtered.length !== 1 ? "s" : ""}
        </span>
      </div>

      {/* Collapsible filter drawer */}
      {showFilters && (
        <div
          style={{
            margin: "8px 0 16px",
            padding: "14px 16px",
            background: "var(--color-bg-surface)",
            border: "1px solid var(--color-border-subtle)",
            borderRadius: 8,
            display: "flex",
            flexDirection: "column",
            gap: 12,
          }}
        >
          {/* Categories */}
          {categories.length > 0 && (
            <div>
              <div style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)", marginBottom: 6 }}>
                Category
              </div>
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                {categories.map((cat) => {
                  const active = categoryFilters.has(cat);
                  const count = torrents?.filter((t) => t.category === cat).length ?? 0;
                  return (
                    <button
                      key={cat}
                      onClick={() => setCategoryFilters((prev) => {
                        const next = new Set(prev);
                        if (next.has(cat)) next.delete(cat); else next.add(cat);
                        return next;
                      })}
                      style={{
                        padding: "4px 10px",
                        borderRadius: 5,
                        border: active ? "1px solid var(--color-accent)" : "1px solid var(--color-border-default)",
                        background: active ? "var(--color-accent-muted)" : "transparent",
                        color: active ? "var(--color-accent)" : "var(--color-text-secondary)",
                        fontSize: 11,
                        fontWeight: active ? 600 : 400,
                        cursor: "pointer",
                        display: "flex",
                        alignItems: "center",
                        gap: 5,
                      }}
                    >
                      {cat}
                      <span style={{ fontSize: 9, color: active ? "var(--color-accent)" : "var(--color-text-muted)" }}>{count}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Tags */}
          {tags.length > 0 && (
            <div>
              <div style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)", marginBottom: 6 }}>
                Tags
              </div>
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                {tags.map((tag) => {
                  const active = tagFilters.has(tag);
                  const count = torrents?.filter((t) => t.tags?.includes(tag)).length ?? 0;
                  return (
                    <button
                      key={tag}
                      onClick={() => setTagFilters((prev) => {
                        const next = new Set(prev);
                        if (next.has(tag)) next.delete(tag); else next.add(tag);
                        return next;
                      })}
                      style={{
                        padding: "4px 10px",
                        borderRadius: 5,
                        border: active ? "1px solid var(--color-accent)" : "1px solid var(--color-border-default)",
                        background: active ? "var(--color-accent-muted)" : "transparent",
                        color: active ? "var(--color-accent)" : "var(--color-text-secondary)",
                        fontSize: 11,
                        fontWeight: active ? 600 : 400,
                        cursor: "pointer",
                        display: "flex",
                        alignItems: "center",
                        gap: 5,
                      }}
                    >
                      {tag}
                      <span style={{ fontSize: 9, color: active ? "var(--color-accent)" : "var(--color-text-muted)" }}>{count}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Empty state */}
          {categories.length === 0 && tags.length === 0 && (
            <p style={{ margin: 0, fontSize: 12, color: "var(--color-text-muted)" }}>
              No categories or tags yet. Assign them to torrents and they'll appear here as filter chips.
            </p>
          )}

          {/* Clear filters */}
          {(categoryFilters.size > 0 || tagFilters.size > 0) && (
            <button
              onClick={() => { setCategoryFilters(new Set()); setTagFilters(new Set()); }}
              style={{ alignSelf: "flex-start", background: "none", border: "none", color: "var(--color-accent)", fontSize: 11, cursor: "pointer", padding: 0 }}
            >
              Clear all filters
            </button>
          )}
        </div>
      )}

      {/* Loading */}
      {isLoading && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {[1, 2, 3].map((i) => <div key={i} className="skeleton" style={{ height: 56, borderRadius: 6 }} />)}
        </div>
      )}

      {/* Empty state */}
      {!isLoading && filtered.length === 0 && (
        <div style={{ textAlign: "center", padding: "60px 0" }}>
          {torrents && torrents.length > 0 ? (
            <>
              <p style={{ fontSize: 14, color: "var(--color-text-secondary)", fontWeight: 500 }}>No matching torrents</p>
              <p style={{ fontSize: 13, color: "var(--color-text-muted)", margin: "6px 0 0" }}>
                Try a different filter or{" "}
                <button onClick={() => { setStatusFilters(new Set(["active"])); setCategoryFilters(new Set()); setTagFilters(new Set()); setSearch(""); }} style={{ background: "none", border: "none", color: "var(--color-accent)", cursor: "pointer", fontSize: 13 }}>
                  clear all filters
                </button>
              </p>
            </>
          ) : (
            <>
              <p style={{ fontSize: 14, color: "var(--color-text-secondary)", fontWeight: 500 }}>No torrents</p>
              <p style={{ fontSize: 13, color: "var(--color-text-muted)", margin: "6px 0 0" }}>Click "Add Torrent" to get started</p>
            </>
          )}
        </div>
      )}

      {/* Torrent list with drag-and-drop */}
      {filtered.length > 0 && (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext items={filtered.map((t) => t.info_hash)} strategy={verticalListSortingStrategy}>
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              {filtered.map((t, i) => {
                // Cap-line divider: only show in manual sort mode when the
                // setting is non-zero. We insert it between the last active
                // slot and the first queued one — i.e. before index
                // maxActiveDownloads when that position is within the list.
                const showCapLine =
                  sortField === "manual" &&
                  maxActiveDownloads > 0 &&
                  i === maxActiveDownloads &&
                  i < filtered.length;

                return (
                  <div key={t.info_hash}>
                    {showCapLine && (
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 10,
                          margin: "2px 0",
                        }}
                      >
                        <div style={{ flex: 1, height: 1, background: "var(--color-status-queued)", opacity: 0.4 }} />
                        <span style={{
                          fontSize: 10,
                          fontWeight: 600,
                          textTransform: "uppercase",
                          letterSpacing: "0.06em",
                          color: "var(--color-status-queued)",
                          opacity: 0.7,
                          flexShrink: 0,
                        }}>
                          Queue limit — {maxActiveDownloads} active
                        </span>
                        <div style={{ flex: 1, height: 1, background: "var(--color-status-queued)", opacity: 0.4 }} />
                      </div>
                    )}
                    <TorrentCard t={t} draggable={sortField === "manual"} />
                  </div>
                );
              })}
            </div>
          </SortableContext>
        </DndContext>
      )}

      {showAdd && <AddModal onClose={() => setShowAdd(false)} />}
    </div>
  );
}
