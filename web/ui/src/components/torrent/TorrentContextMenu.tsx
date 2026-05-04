// TorrentContextMenu — right-click action menu for a single torrent.
//
// Why this exists: qBittorrent users expect right-click parity, and
// stuffing every action into the row's icon strip gets unreadable past
// 3 buttons. The menu is portaled to document.body so it floats above
// every other layer (including the row's hover styles, the filter
// panel, and modal dialogs that DON'T own focus).
//
// Closes on:
//   - outside click (mousedown on anything outside the menu)
//   - ESC keydown
//   - scroll (the anchor moves so the menu's position becomes wrong)
//   - viewport resize (same)
//   - any action selected
//
// Submodals: Category, Tags, and Move-location each open a small
// inline modal driven by local state in the parent (TorrentList).
// Wiring those modals lives outside this file.

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";
import {
  Pause,
  Play,
  Zap,
  RefreshCw,
  Radio,
  FolderOpen,
  Tag as TagIcon,
  Move,
  Copy,
  Trash2,
  Search,
} from "lucide-react";
import { useConfirm } from "@beacon-shared/ConfirmDialog";
import {
  type TorrentInfo,
  usePauseTorrent,
  useResumeTorrent,
  useDeleteTorrent,
  useForceStartTorrent,
  useRecheckTorrent,
  useReannounceTorrent,
  useResearchTorrent,
} from "@/api/torrents";

export interface ContextMenuTarget {
  torrent: TorrentInfo;
  x: number;
  y: number;
}

interface Props {
  target: ContextMenuTarget;
  onClose: () => void;
  // Callback the parent uses to open its category/tags/location
  // submodals. Decoupling like this keeps this component free of
  // form state — it just signals intent and goes away.
  onOpenSubmodal: (kind: "category" | "tags" | "location", t: TorrentInfo) => void;
}

const ITEM_HEIGHT = 30;
const MENU_PAD_Y = 4;
const ESTIMATED_MENU_WIDTH = 220;

export default function TorrentContextMenu({ target, onClose, onOpenSubmodal }: Props) {
  const { torrent: t } = target;
  const confirm = useConfirm();
  const menuRef = useRef<HTMLDivElement>(null);

  const pause = usePauseTorrent();
  const resume = useResumeTorrent();
  const del = useDeleteTorrent();
  const forceStart = useForceStartTorrent();
  const recheck = useRecheckTorrent();
  const reannounce = useReannounceTorrent();
  const research = useResearchTorrent();

  // Close on outside click / ESC / scroll / resize.
  useEffect(() => {
    const onDocMouseDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    const onScroll = () => onClose();
    const onResize = () => onClose();
    document.addEventListener("mousedown", onDocMouseDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onResize);
    return () => {
      document.removeEventListener("mousedown", onDocMouseDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onResize);
    };
  }, [onClose]);

  // Reposition if the menu would overflow the viewport. The first
  // render places it at click coords; this effect nudges it left/up
  // to keep it on screen. Using max(0, …) so we never push off the
  // top-left.
  const [pos, setPos] = useState({ x: target.x, y: target.y });
  useEffect(() => {
    if (!menuRef.current) return;
    const rect = menuRef.current.getBoundingClientRect();
    const overflowX = target.x + rect.width - window.innerWidth;
    const overflowY = target.y + rect.height - window.innerHeight;
    setPos({
      x: Math.max(8, target.x - Math.max(0, overflowX) - 8),
      y: Math.max(8, target.y - Math.max(0, overflowY) - 8),
    });
  }, [target.x, target.y]);

  // Action wrappers — each closes the menu after firing.
  const run = (fn: () => void) => () => {
    fn();
    onClose();
  };

  const isPaused = t.status === "paused";
  const isStalled = !!t.stalled_at;
  const canPause = !isPaused;
  const canResume = isPaused;
  const isComplete = t.status === "completed" || t.status === "seeding";

  const handleCopy = (label: string, value: string) => {
    void navigator.clipboard.writeText(value).then(
      () => toast.success(`${label} copied`),
      () => toast.error(`Failed to copy ${label.toLowerCase()}`),
    );
  };

  // Build a magnet URI from the hash + name. Real magnets carry
  // tracker URLs, but anacrolix accepts the bare-hash form and
  // self-discovers via DHT. Good enough for "I want to re-add this
  // somewhere else" use cases.
  const buildMagnet = () => {
    const dn = encodeURIComponent(t.name);
    return `magnet:?xt=urn:btih:${t.info_hash}&dn=${dn}`;
  };

  const handleRemove = async (deleteFiles: boolean) => {
    onClose();
    const ok = await confirm({
      title: deleteFiles ? "Remove and delete files" : "Remove torrent",
      message: deleteFiles
        ? `Remove "${t.name}" and PERMANENTLY delete its downloaded files?`
        : `Remove "${t.name}" from Haul? Files on disk are kept.`,
      confirmLabel: deleteFiles ? "Delete files" : "Remove",
      danger: true,
    });
    if (!ok) return;
    del.mutate(
      { hash: t.info_hash, deleteFiles },
      {
        onSuccess: () => toast.success(deleteFiles ? "Removed and files deleted" : "Removed"),
        onError: (e) => toast.error((e as Error).message),
      },
    );
  };

  const handleRecheck = async () => {
    onClose();
    const ok = await confirm({
      title: "Force recheck",
      message: `Re-verify all downloaded data for "${t.name}"? This can take a while for large torrents.`,
      confirmLabel: "Recheck",
    });
    if (!ok) return;
    recheck.mutate(t.info_hash, {
      onSuccess: () => toast.success("Recheck started"),
      onError: (e) => toast.error((e as Error).message),
    });
  };

  return createPortal(
    <div
      ref={menuRef}
      role="menu"
      style={{
        position: "fixed",
        top: pos.y,
        left: pos.x,
        minWidth: ESTIMATED_MENU_WIDTH,
        background: "var(--color-bg-elevated)",
        border: "1px solid var(--color-border-default)",
        borderRadius: 8,
        boxShadow: "0 10px 30px rgba(0, 0, 0, 0.4)",
        padding: `${MENU_PAD_Y}px 0`,
        zIndex: 9999,
        fontSize: 13,
        userSelect: "none",
      }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {/* Status group */}
      {canResume && (
        <Item
          icon={Play}
          label={isStalled ? "Resume (clear stalled)" : "Resume"}
          onClick={run(() =>
            resume.mutate(t.info_hash, {
              onSuccess: () => toast.success(isStalled ? "Resumed — stalled cleared" : "Resumed"),
              onError: (e) => toast.error((e as Error).message),
            }),
          )}
        />
      )}
      {canPause && (
        <Item
          icon={Pause}
          label="Pause"
          onClick={run(() =>
            pause.mutate(t.info_hash, {
              onSuccess: () => toast.success("Paused"),
              onError: (e) => toast.error((e as Error).message),
            }),
          )}
        />
      )}
      <Item
        icon={Zap}
        label="Force start (bypass queue)"
        onClick={run(() =>
          forceStart.mutate(t.info_hash, {
            onSuccess: () => toast.success("Force-started"),
            onError: (e) => toast.error((e as Error).message),
          }),
        )}
      />

      {/* Re-search — gated on stalled + arr-requested. The most useful
          action when a torrent has gone dead, so it lives near the top. */}
      {isStalled && (t.requester === "pilot" || t.requester === "prism") && (
        <>
          <Divider />
          <Item
            icon={Search}
            label={`Re-search via ${t.requester === "pilot" ? "Pilot" : "Prism"}`}
            onClick={run(() =>
              research.mutate(t.info_hash, {
                onSuccess: (r) => {
                  if (r.result === "grabbed") {
                    toast.success(`Grabbed alternative: ${r.release_title}`);
                  } else {
                    toast.error(r.reason || "No alternative releases available");
                  }
                },
                onError: (e) => toast.error((e as Error).message),
              }),
            )}
          />
        </>
      )}

      <Divider />

      {/* Engine actions */}
      <Item icon={RefreshCw} label="Force recheck…" onClick={handleRecheck} />
      <Item
        icon={Radio}
        label="Force reannounce"
        onClick={run(() =>
          reannounce.mutate(t.info_hash, {
            onSuccess: () => toast.success("Reannounced to trackers"),
            onError: (e) => toast.error((e as Error).message),
          }),
        )}
      />

      <Divider />

      {/* Identity */}
      <Item icon={FolderOpen} label="Category…" onClick={run(() => onOpenSubmodal("category", t))} />
      <Item icon={TagIcon} label="Tags…" onClick={run(() => onOpenSubmodal("tags", t))} />
      <Item
        icon={Move}
        label="Move location…"
        // Move during active download is messy — anacrolix supports it
        // but it's the most common foot-gun. Disable when downloading
        // unless it's already paused/complete; the user can pause first.
        disabled={!isComplete && !isPaused}
        title={!isComplete && !isPaused ? "Pause before moving" : undefined}
        onClick={run(() => onOpenSubmodal("location", t))}
      />

      <Divider />

      {/* Copy */}
      <Item icon={Copy} label="Copy name" onClick={run(() => handleCopy("Name", t.name))} />
      <Item icon={Copy} label="Copy hash" onClick={run(() => handleCopy("Hash", t.info_hash))} />
      <Item icon={Copy} label="Copy magnet" onClick={run(() => handleCopy("Magnet", buildMagnet()))} />
      <Item icon={Copy} label="Copy save path" onClick={run(() => handleCopy("Save path", t.save_path))} />

      <Divider />

      {/* Destructive */}
      <Item
        icon={Trash2}
        label="Remove (keep files)"
        onClick={() => handleRemove(false)}
        danger
      />
      <Item
        icon={Trash2}
        label="Remove and delete files"
        onClick={() => handleRemove(true)}
        danger
      />
    </div>,
    document.body,
  );
}

function Divider() {
  return (
    <div
      style={{
        height: 1,
        background: "var(--color-border-subtle)",
        margin: "4px 0",
      }}
    />
  );
}

interface ItemProps {
  icon: React.ElementType;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  danger?: boolean;
  title?: string;
}

function Item({ icon: Icon, label, onClick, disabled, danger, title }: ItemProps) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={title}
      role="menuitem"
      style={{
        display: "flex",
        alignItems: "center",
        gap: 10,
        width: "100%",
        height: ITEM_HEIGHT,
        padding: "0 14px",
        background: "transparent",
        border: "none",
        textAlign: "left",
        fontSize: 13,
        color: disabled
          ? "var(--color-text-muted)"
          : danger
          ? "var(--color-danger)"
          : "var(--color-text-primary)",
        cursor: disabled ? "not-allowed" : "pointer",
        opacity: disabled ? 0.5 : 1,
      }}
      onMouseEnter={(e) => {
        if (disabled) return;
        (e.currentTarget as HTMLButtonElement).style.background = "var(--color-bg-subtle)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLButtonElement).style.background = "transparent";
      }}
    >
      <Icon size={14} style={{ flexShrink: 0 }} />
      <span style={{ flex: 1 }}>{label}</span>
    </button>
  );
}
