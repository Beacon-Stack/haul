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
import AddModal from "./AddTorrentModal";
import TorrentCard from "./TorrentCard";
import { CategorySubmodal, TagsSubmodal, LocationSubmodal } from "./TorrentSubmodals";

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
    case "active":      return !isFinished(t) && !t.stalled_at;
    case "downloading": return t.status === "downloading" && !t.stalled_at;
    case "seeding":     return t.status === "seeding";
    case "completed":   return isFinished(t);
    // Auto-stalled (stalled_at set) shows up as Stalled, not Paused —
    // even though status === "paused" under the hood — so the user can
    // tell apart "I paused this" from "Haul paused this because it's dead".
    case "paused":      return t.status === "paused" && !t.stalled_at;
    case "stalled":     return !!t.stalled_at || (t.status === "downloading" && t.stalled);
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

// Add Modal

const selectStyle: React.CSSProperties = {
  padding: "5px 10px",
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "var(--color-bg-elevated)",
  color: "var(--color-text-primary)",
  fontSize: 12,
  outline: "none",
};

// Page

export default function TorrentList() {
  const { data: torrents, isLoading } = useTorrents();
  const { data: settings } = useSettings();
  const [showAdd, setShowAdd] = useState(false);
  // Seed the status filter from the URL query string so the
  // dashboard's "Needs attention" card and banner can deep-link
  // straight to the stalled-only view. Only consumed on the first
  // render — after that, user clicks on the filter pills take over.
  const [searchParams, setSearchParams] = useSearchParams();
  const initialStatus = searchParams.get("status");
  const [statusFilters, setStatusFilters] = useState<Set<StatusFilter>>(() => {
    if (initialStatus && ["active", "downloading", "seeding", "completed", "paused", "stalled", "failed", "queued"].includes(initialStatus)) {
      return new Set([initialStatus as StatusFilter]);
    }
    return new Set(["active"]);
  });
  // Strip the param after we've consumed it so a refresh reverts to
  // the default. Otherwise navigating away and back would silently
  // re-enter the stalled-only view.
  useEffect(() => {
    if (initialStatus) {
      const next = new URLSearchParams(searchParams);
      next.delete("status");
      setSearchParams(next, { replace: true });
    }
    // Run once on mount only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const [categoryFilters, setCategoryFilters] = useState<Set<string>>(new Set());
  const [tagFilters, setTagFilters] = useState<Set<string>>(new Set());
  // Right-click context-menu state. Hoisted to the page so a single
  // menu instance handles every row instead of N invisible menus.
  const [ctxMenu, setCtxMenu] = useState<ContextMenuTarget | null>(null);
  const [submodal, setSubmodal] = useState<{ kind: "category" | "tags" | "location"; t: TorrentInfo } | null>(null);
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
                    <TorrentCard
                      t={t}
                      draggable={sortField === "manual"}
                      onContextMenu={(target, x, y) => setCtxMenu({ torrent: target, x, y })}
                    />
                  </div>
                );
              })}
            </div>
          </SortableContext>
        </DndContext>
      )}

      {showAdd && <AddModal onClose={() => setShowAdd(false)} />}

      {ctxMenu && (
        <TorrentContextMenu
          target={ctxMenu}
          onClose={() => setCtxMenu(null)}
          onOpenSubmodal={(kind, t) => setSubmodal({ kind, t })}
        />
      )}

      {submodal?.kind === "category" && (
        <CategorySubmodal t={submodal.t} onClose={() => setSubmodal(null)} />
      )}
      {submodal?.kind === "tags" && (
        <TagsSubmodal t={submodal.t} onClose={() => setSubmodal(null)} />
      )}
      {submodal?.kind === "location" && (
        <LocationSubmodal t={submodal.t} onClose={() => setSubmodal(null)} />
      )}
    </div>
  );
}

// Right-click submodals
//
// Each is a small inline form; nothing fancy. The category modal
// double-duties as a category picker for both new categories and
// existing ones — typing a name that doesn't match selects "create new".
