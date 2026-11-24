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

export default function AddModal({ onClose }: { onClose: () => void }) {
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
    <Modal onClose={onClose} width={480} innerStyle={{ background: "var(--color-bg-elevated)", padding: 24 }}>
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
            <span style={{ color: "var(--color-text-muted)" }}>{formatTorrentFileBytes(file.size)}</span>
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
    </Modal>
  );
}

// Torrent Row

// All status colour and label rendering goes through torrentVisual() in
// @/lib/torrentStatus. Do NOT add inline colour switches here — drift between
// the badge, the bar, and the filter pills is exactly the bug that helper
// was extracted to fix.
