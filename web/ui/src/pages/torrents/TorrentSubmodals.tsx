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

// ModalShell adds the title + compact sizing the submodals share; the
// backdrop / ESC / click-away behavior comes from the shared Modal.
function ModalShell({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <Modal onClose={onClose} width={420} innerStyle={{ background: "var(--color-bg-elevated)", padding: 20 }}>
      <h3 style={{ margin: "0 0 14px", fontSize: 14, fontWeight: 600, color: "var(--color-text-primary)" }}>{title}</h3>
      {children}
    </Modal>
  );
}

function ModalActions({ onClose, onSubmit, submitLabel, pending }: { onClose: () => void; onSubmit: () => void; submitLabel: string; pending: boolean }) {
  return (
    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
      <button
        onClick={onClose}
        style={{
          padding: "7px 14px",
          fontSize: 13,
          borderRadius: 6,
          border: "1px solid var(--color-border-default)",
          background: "transparent",
          color: "var(--color-text-secondary)",
          cursor: "pointer",
        }}
      >
        Cancel
      </button>
      <button
        onClick={onSubmit}
        disabled={pending}
        style={{
          padding: "7px 14px",
          fontSize: 13,
          fontWeight: 500,
          borderRadius: 6,
          border: "none",
          background: "var(--color-accent)",
          color: "white",
          cursor: pending ? "not-allowed" : "pointer",
          opacity: pending ? 0.6 : 1,
        }}
      >
        {pending ? "Saving…" : submitLabel}
      </button>
    </div>
  );
}

export function CategorySubmodal({ t, onClose }: { t: TorrentInfo; onClose: () => void }) {
  const [value, setValue] = useState(t.category ?? "");
  const setCategory = useSetTorrentCategory();
  const submit = () => {
    setCategory.mutate(
      { hash: t.info_hash, category: value.trim() },
      {
        onSuccess: () => {
          toast.success(value.trim() ? `Category set to "${value.trim()}"` : "Category cleared");
          onClose();
        },
        onError: (e) => toast.error((e as Error).message),
      },
    );
  };
  return (
    <ModalShell title="Set category" onClose={onClose}>
      <p style={{ margin: "0 0 10px", fontSize: 12, color: "var(--color-text-muted)" }}>
        Type a category name (or leave empty to clear).
      </p>
      <input
        autoFocus
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="e.g. tv, movies, music"
        style={inputStyle}
        onKeyDown={(e) => { if (e.key === "Enter") submit(); }}
      />
      <ModalActions onClose={onClose} onSubmit={submit} submitLabel="Save" pending={setCategory.isPending} />
    </ModalShell>
  );
}

export function TagsSubmodal({ t, onClose }: { t: TorrentInfo; onClose: () => void }) {
  const [addInput, setAddInput] = useState("");
  const addTags = useAddTorrentTags();
  const removeTags = useRemoveTorrentTags();
  const tags = t.tags ?? [];

  const submitAdd = () => {
    const tag = addInput.trim();
    if (!tag) return;
    addTags.mutate(
      { hash: t.info_hash, tags: [tag] },
      {
        onSuccess: () => {
          setAddInput("");
          toast.success(`Tag "${tag}" added`);
        },
        onError: (e) => toast.error((e as Error).message),
      },
    );
  };

  return (
    <ModalShell title="Edit tags" onClose={onClose}>
      <p style={{ margin: "0 0 10px", fontSize: 12, color: "var(--color-text-muted)" }}>
        Click a tag to remove it. Type below to add a new one.
      </p>
      {tags.length > 0 && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 12 }}>
          {tags.map((tag) => (
            <button
              key={tag}
              onClick={() =>
                removeTags.mutate(
                  { hash: t.info_hash, tags: [tag] },
                  { onSuccess: () => toast.success(`Tag "${tag}" removed`), onError: (e) => toast.error((e as Error).message) },
                )
              }
              style={{
                padding: "3px 8px 3px 10px",
                fontSize: 12,
                borderRadius: 12,
                background: "var(--color-bg-subtle)",
                color: "var(--color-text-primary)",
                border: "1px solid var(--color-border-subtle)",
                cursor: "pointer",
                display: "inline-flex",
                alignItems: "center",
                gap: 4,
              }}
              title={`Remove tag "${tag}"`}
            >
              {tag} <X size={10} />
            </button>
          ))}
        </div>
      )}
      <input
        autoFocus
        value={addInput}
        onChange={(e) => setAddInput(e.target.value)}
        placeholder="New tag name"
        style={inputStyle}
        onKeyDown={(e) => { if (e.key === "Enter") submitAdd(); }}
      />
      <ModalActions onClose={onClose} onSubmit={submitAdd} submitLabel="Add" pending={addTags.isPending} />
    </ModalShell>
  );
}

export function LocationSubmodal({ t, onClose }: { t: TorrentInfo; onClose: () => void }) {
  const [value, setValue] = useState(t.save_path ?? "");
  const setLocation = useSetTorrentLocation();
  const submit = () => {
    const path = value.trim();
    if (!path) return;
    setLocation.mutate(
      { hash: t.info_hash, path },
      {
        onSuccess: () => {
          toast.success("Move started");
          onClose();
        },
        onError: (e) => toast.error((e as Error).message),
      },
    );
  };
  return (
    <ModalShell title="Move torrent data" onClose={onClose}>
      <p style={{ margin: "0 0 10px", fontSize: 12, color: "var(--color-text-muted)" }}>
        New save path. Files will be moved on disk — large torrents take time.
      </p>
      <input
        autoFocus
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="/downloads/..."
        style={inputStyle}
        onKeyDown={(e) => { if (e.key === "Enter") submit(); }}
      />
      <ModalActions onClose={onClose} onSubmit={submit} submitLabel="Move" pending={setLocation.isPending} />
    </ModalShell>
  );
}

const inputStyle: React.CSSProperties = {
  width: "100%",
  padding: "8px 12px",
  fontSize: 13,
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "var(--color-bg-surface)",
  color: "var(--color-text-primary)",
  outline: "none",
  boxSizing: "border-box",
};
