import { useState } from "react";
import { Copy, Check, Trash2, Plus } from "lucide-react";
import { toast } from "sonner";
import type { TrackerInfo } from "@/api/torrents";
import { useAddTrackers, useRemoveTracker } from "@/api/torrents";

// TrackerList — list of configured trackers with add/remove. Static
// announce metadata only (no live "last announce" / "reported peers"
// — anacrolix's pinned version doesn't expose those publicly). Each
// row has a copy + delete button on hover; an "Add tracker URL" form
// at the bottom accepts newline-separated URLs and pastes from the
// clipboard.

interface TrackerListProps {
  trackers: TrackerInfo[];
  hash: string;
}

export default function TrackerList({ trackers, hash }: TrackerListProps) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      {trackers.length === 0 ? (
        <p style={{ margin: 0, fontSize: 12, color: "var(--color-text-muted)", fontStyle: "italic" }}>
          No trackers configured (DHT / PEX only).
        </p>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          {trackers.map((tr, i) => (
            <TrackerRow key={`${tr.tier}-${tr.url}-${i}`} tracker={tr} hash={hash} />
          ))}
        </div>
      )}
      <AddTrackerForm hash={hash} />
    </div>
  );
}

function TrackerRow({ tracker, hash }: { tracker: TrackerInfo; hash: string }) {
  const [copied, setCopied] = useState(false);
  const [hovered, setHovered] = useState(false);
  const remove = useRemoveTracker(hash);

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

  function handleRemove(e: React.MouseEvent) {
    e.stopPropagation();
    remove.mutate(tracker.url, {
      onSuccess: () => toast.success("Tracker removed"),
      onError: (err) => toast.error((err as Error).message),
    });
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
        style={iconBtnStyle(hovered || copied, copied ? "var(--color-success)" : "var(--color-text-muted)")}
        title={copied ? "Copied!" : "Copy URL"}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
      <button
        onClick={handleRemove}
        disabled={remove.isPending}
        style={iconBtnStyle(hovered, "var(--color-danger)")}
        title="Remove tracker"
      >
        <Trash2 size={13} />
      </button>
    </div>
  );
}

const iconBtnStyle = (visible: boolean, color: string): React.CSSProperties => ({
  background: "none",
  border: "none",
  padding: 4,
  borderRadius: 4,
  cursor: "pointer",
  color,
  display: "flex",
  opacity: visible ? 1 : 0,
  transition: "opacity 120ms ease",
});

function AddTrackerForm({ hash }: { hash: string }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const add = useAddTrackers(hash);

  function handleAdd() {
    const urls = text
      .split(/\n/)
      .map((s) => s.trim())
      .filter(Boolean);
    if (urls.length === 0) return;
    add.mutate(urls, {
      onSuccess: () => {
        toast.success(`Added ${urls.length} tracker${urls.length === 1 ? "" : "s"}`);
        setText("");
        setOpen(false);
      },
      onError: (err) => toast.error((err as Error).message),
    });
  }

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        style={{
          alignSelf: "flex-start",
          display: "inline-flex",
          alignItems: "center",
          gap: 4,
          padding: "5px 10px",
          background: "transparent",
          border: "1px dashed var(--color-border-default)",
          borderRadius: 6,
          color: "var(--color-text-secondary)",
          fontSize: 12,
          cursor: "pointer",
        }}
      >
        <Plus size={12} /> Add tracker
      </button>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="One URL per line — e.g. udp://tracker.example.com:1337"
        rows={3}
        style={{
          padding: "8px 10px",
          borderRadius: 6,
          border: "1px solid var(--color-border-default)",
          background: "var(--color-bg-surface)",
          color: "var(--color-text-primary)",
          fontFamily: "var(--font-family-mono)",
          fontSize: 12,
          resize: "vertical",
          outline: "none",
        }}
        autoFocus
      />
      <div style={{ display: "flex", gap: 6, justifyContent: "flex-end" }}>
        <button
          onClick={() => {
            setOpen(false);
            setText("");
          }}
          style={{
            padding: "5px 12px",
            borderRadius: 6,
            border: "1px solid var(--color-border-default)",
            background: "transparent",
            color: "var(--color-text-secondary)",
            fontSize: 12,
            cursor: "pointer",
          }}
        >
          Cancel
        </button>
        <button
          onClick={handleAdd}
          disabled={add.isPending || !text.trim()}
          style={{
            padding: "5px 12px",
            borderRadius: 6,
            border: "none",
            background: !text.trim() ? "var(--color-bg-subtle)" : "var(--color-accent)",
            color: !text.trim() ? "var(--color-text-muted)" : "var(--color-accent-fg)",
            fontSize: 12,
            fontWeight: 500,
            cursor: !text.trim() ? "not-allowed" : "pointer",
          }}
        >
          {add.isPending ? "Adding…" : "Add"}
        </button>
      </div>
    </div>
  );
}
