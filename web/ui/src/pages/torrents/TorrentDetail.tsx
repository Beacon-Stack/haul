import { useParams, Link } from "react-router-dom";
import {
  useTorrent,
  useTorrentFiles,
  usePauseTorrent,
  useResumeTorrent,
  useDeleteTorrent,
  useTorrentPeers,
  useTorrentPieces,
  useTorrentTrackers,
  useTorrentStall,
} from "@/api/torrents";
import { Pause, Play, Trash2, FileText, Link2, Hash, Download } from "lucide-react";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { useConfirm } from "@beacon-shared/ConfirmDialog";
import { torrentVisual } from "@/lib/torrentStatus";
import PieceBar from "@/components/torrent/PieceBar";
import PeerList from "@/components/torrent/PeerList";
import TrackerList from "@/components/torrent/TrackerList";
import CollapsibleSection from "@/components/torrent/CollapsibleSection";
import StallCallout from "@/components/torrent/StallCallout";

function formatBytes(b: number): string {
  if (b <= 0) return "0 B";
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatSpeed(b: number): string {
  if (b <= 0) return "0 B/s";
  if (b < 1024) return `${b} B/s`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB/s`;
  return `${(b / (1024 * 1024)).toFixed(1)} MB/s`;
}

// Status colours / labels come from the canonical helper in
// @/lib/torrentStatus. Don't add inline switches — keep this in sync with
// TorrentList by routing every status read through torrentVisual().

export default function TorrentDetail() {
  const { hash } = useParams<{ hash: string }>();
  const { data: t, isLoading } = useTorrent(hash ?? "", { detailPage: true });
  const { data: files } = useTorrentFiles(hash ?? "");
  const { data: peersResp } = useTorrentPeers(hash ?? "");
  const { data: pieces } = useTorrentPieces(hash ?? "");
  const { data: trackersResp } = useTorrentTrackers(hash ?? "");
  const pause = usePauseTorrent();
  const resume = useResumeTorrent();
  const del = useDeleteTorrent();
  const navigate = useNavigate();
  const confirm = useConfirm();

  const peers = peersResp?.peers ?? [];
  const trackers = trackersResp?.trackers ?? [];
  // Stall classification — drives the callout banner above the facts grid.
  // The hook polls /api/v1/torrents/{hash}/stall every 5s; render the
  // callout only when the backend has explicitly classified the torrent
  // as stalled (zero-init or absent → no callout).
  const { data: stall } = useTorrentStall(hash ?? "");

  async function handleDelete() {
    if (!t) return;
    if (
      await confirm({
        title: "Delete torrent and files",
        message: `Delete "${t.name}" AND its downloaded files on disk? This cannot be undone.`,
        confirmLabel: "Delete everything",
      })
    ) {
      del.mutate(
        { hash: t.info_hash, deleteFiles: true },
        { onSuccess: () => { toast.success("Removed"); navigate("/"); } },
      );
    }
  }

  if (isLoading) {
    return (
      <div style={{ padding: 24 }}>
        <div className="skeleton" style={{ height: 16, width: 80, borderRadius: 4, marginBottom: 16 }} />
        <div className="skeleton" style={{ height: 24, width: 300, borderRadius: 4, marginBottom: 24 }} />
        <div style={{ display: "flex", gap: 12 }}>
          {[1, 2, 3, 4].map((i) => <div key={i} className="skeleton" style={{ height: 60, flex: 1, borderRadius: 6 }} />)}
        </div>
      </div>
    );
  }

  if (!t) {
    return (
      <div style={{ padding: 24 }}>
        <Link to="/" style={{ fontSize: 13, color: "var(--color-accent)", textDecoration: "none" }}>← Torrents</Link>
        <p style={{ marginTop: 24, fontSize: 13, color: "var(--color-text-muted)" }}>Torrent not found.</p>
      </div>
    );
  }

  const visual = torrentVisual(t);
  const facts = [
    { label: "Status", value: visual.label, color: visual.color },
    { label: "Size", value: formatBytes(t.size) },
    { label: "Downloaded", value: formatBytes(t.downloaded) },
    { label: "Uploaded", value: formatBytes(t.uploaded) },
    { label: "Down Speed", value: formatSpeed(t.download_rate) },
    { label: "Up Speed", value: formatSpeed(t.upload_rate) },
    { label: "Seeds", value: String(t.seeds) },
    { label: "Peers", value: String(t.peers) },
    { label: "Ratio", value: t.seed_ratio.toFixed(2) },
    { label: "Progress", value: `${(t.progress * 100).toFixed(1)}%` },
  ];

  return (
    <div style={{ padding: 24, maxWidth: 900 }}>
      <Link
        to="/"
        style={{ fontSize: 13, color: "var(--color-text-muted)", textDecoration: "none", display: "inline-block", marginBottom: 16 }}
        onMouseEnter={(e) => { (e.currentTarget as HTMLAnchorElement).style.color = "var(--color-text-primary)"; }}
        onMouseLeave={(e) => { (e.currentTarget as HTMLAnchorElement).style.color = "var(--color-text-muted)"; }}
      >
        ← Torrents
      </Link>

      {/* Header */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 20, gap: 16 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 18, fontWeight: 600, color: "var(--color-text-primary)", lineHeight: 1.3 }}>{t.name}</h1>
          <p style={{ margin: "4px 0 0", fontSize: 12, color: "var(--color-text-muted)", fontFamily: "var(--font-family-mono)" }}>{t.info_hash}</p>
        </div>
        <div style={{ display: "flex", gap: 6, flexShrink: 0 }}>
          <button
            onClick={() => copyToClipboard(buildMagnet(t.info_hash, t.name), "Magnet link copied")}
            title="Copy magnet link"
            style={ghostBtnStyle}
          >
            <Link2 size={13} /> Magnet
          </button>
          <button
            onClick={() => copyToClipboard(t.info_hash, "Info hash copied")}
            title="Copy info hash"
            style={ghostBtnStyle}
          >
            <Hash size={13} /> Hash
          </button>
          <a
            href={`/api/v1/torrents/${t.info_hash}/torrent_file`}
            download
            title="Download .torrent file"
            style={{ ...ghostBtnStyle, textDecoration: "none" }}
          >
            <Download size={13} /> .torrent
          </a>
          {t.status === "paused" ? (
            <button onClick={() => resume.mutate(t.info_hash)} style={btnStyle("var(--color-accent)")}>
              <Play size={13} /> Resume
            </button>
          ) : (
            <button onClick={() => pause.mutate(t.info_hash)} style={btnStyle("var(--color-status-paused)")}>
              <Pause size={13} /> Pause
            </button>
          )}
          <button
            onClick={handleDelete}
            style={btnStyle("var(--color-danger)")}
          >
            <Trash2 size={13} /> Delete
          </button>
        </div>
      </div>

      {/* Progress bar */}
      <div style={{ marginBottom: 24 }}>
        <div style={{ height: 6, borderRadius: 3, background: "var(--color-bg-subtle)" }}>
          <div style={{ width: `${Math.min(t.progress * 100, 100)}%`, height: "100%", borderRadius: 3, background: visual.color, transition: "width 0.3s" }} />
        </div>
      </div>

      {/* Stall callout — surfaces multi-level classification (level + reason
          + inactive duration) when /api/v1/torrents/{hash}/stall reports a
          non-zero stall. The "Status: Stalled" facts cell only signals THAT
          a torrent is stalled; this banner explains WHY. */}
      {stall?.stalled && <StallCallout stall={stall} />}

      {/* Facts grid */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(140px, 1fr))", gap: 10, marginBottom: 28 }}>
        {facts.map(({ label, value, color }) => (
          <div key={label} style={{ background: "var(--color-bg-surface)", border: "1px solid var(--color-border-subtle)", borderRadius: 6, padding: "10px 14px" }}>
            <div style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)", marginBottom: 2 }}>{label}</div>
            <div style={{ fontSize: 14, fontWeight: 500, color: color || "var(--color-text-primary)", textTransform: "capitalize" }}>{value}</div>
          </div>
        ))}
      </div>

      {/* Pieces — the canvas widget. Auto-collapses on 100% complete to
          get the flashy assembly visual out of the way once the job's done,
          but users can still click to re-expand and inspect which pieces
          hashed correctly etc. */}
      {pieces && (
        <CollapsibleSection
          label="Pieces"
          count={pieces.num_pieces > 0 ? pieces.num_pieces.toLocaleString() : undefined}
          defaultOpen={t.progress < 1}
        >
          <PieceBar pieces={pieces} files={files ?? []} progress={t.progress} />
        </CollapsibleSection>
      )}

      {/* Peers — the "who are we talking to" table. Default open even for
          completed torrents because seeding peers are still meaningful. */}
      <CollapsibleSection label="Peers" count={peers.length}>
        <PeerList peers={peers} />
      </CollapsibleSection>

      {/* Trackers — configured list only, no live status. Default closed
          because the data is static and rarely inspected. */}
      <CollapsibleSection label="Trackers" count={trackers.length} defaultOpen={false}>
        <TrackerList trackers={trackers} hash={t.info_hash} />
      </CollapsibleSection>

      {/* Meta */}
      <div style={{ display: "flex", gap: 12, flexWrap: "wrap", marginBottom: 28 }}>
        {t.category && (
          <div style={{ fontSize: 11, padding: "3px 10px", borderRadius: 4, background: "var(--color-accent-muted)", color: "var(--color-accent)", fontWeight: 500 }}>
            {t.category}
          </div>
        )}
        {t.tags?.map((tag) => (
          <div key={tag} style={{ fontSize: 11, padding: "3px 10px", borderRadius: 4, background: "var(--color-bg-subtle)", color: "var(--color-text-secondary)", fontWeight: 500 }}>
            {tag}
          </div>
        ))}
      </div>

      {/* Paths */}
      <div style={{ marginBottom: 28 }}>
        <div style={{ fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.04em", color: "var(--color-text-muted)", marginBottom: 8 }}>Paths</div>
        <div style={{ fontSize: 12, color: "var(--color-text-secondary)", fontFamily: "var(--font-family-mono)", lineHeight: 1.8 }}>
          <div>Save: {t.save_path}</div>
          <div>Content: {t.content_path}</div>
        </div>
      </div>

      {/* Files */}
      {files && files.length > 0 && (
        <div>
          <div style={{ fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.04em", color: "var(--color-text-muted)", marginBottom: 8 }}>
            Files ({files.length})
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            {files.map((f) => (
              <div
                key={f.index}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  padding: "8px 12px",
                  background: "var(--color-bg-surface)",
                  border: "1px solid var(--color-border-subtle)",
                  borderRadius: 6,
                }}
              >
                <FileText size={13} style={{ color: "var(--color-text-muted)", flexShrink: 0 }} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: 12, color: "var(--color-text-primary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontFamily: "var(--font-family-mono)" }}>
                    {f.path}
                  </div>
                </div>
                <span style={{ fontSize: 11, color: "var(--color-text-muted)", flexShrink: 0 }}>{formatBytes(f.size)}</span>
                <span style={{ fontSize: 10, padding: "1px 6px", borderRadius: 3, fontWeight: 500, flexShrink: 0,
                  background: f.priority === "skip" ? "var(--color-bg-subtle)" : f.priority === "high" ? "color-mix(in srgb, var(--color-warning) 15%, transparent)" : "color-mix(in srgb, var(--color-success) 15%, transparent)",
                  color: f.priority === "skip" ? "var(--color-text-muted)" : f.priority === "high" ? "var(--color-warning)" : "var(--color-success)",
                }}>
                  {f.priority}
                </span>
                {/* File progress */}
                <div style={{ width: 50, height: 3, borderRadius: 2, background: "var(--color-bg-subtle)", flexShrink: 0 }}>
                  <div style={{ width: `${f.progress * 100}%`, height: "100%", borderRadius: 2, background: "var(--color-status-downloading)" }} />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function btnStyle(color: string): React.CSSProperties {
  return {
    display: "flex",
    alignItems: "center",
    gap: 5,
    padding: "6px 12px",
    borderRadius: 6,
    border: `1px solid ${color}`,
    background: `color-mix(in srgb, ${color} 10%, transparent)`,
    color,
    fontSize: 12,
    fontWeight: 500,
    cursor: "pointer",
  };
}

// Quieter button variant for the copy / export actions — same shape
// but neutral colors so the primary Pause/Delete still pop.
const ghostBtnStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 5,
  padding: "6px 12px",
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "transparent",
  color: "var(--color-text-secondary)",
  fontSize: 12,
  fontWeight: 500,
  cursor: "pointer",
};

// buildMagnet composes a minimal magnet URI from the info hash + name.
// We don't include trackers — they're per-torrent and would need a
// separate fetch; the recipient's client picks up trackers via DHT/PEX
// once the magnet resolves.
function buildMagnet(infoHash: string, name: string): string {
  const dn = encodeURIComponent(name);
  return `magnet:?xt=urn:btih:${infoHash}&dn=${dn}`;
}

// copyToClipboard wraps the navigator.clipboard API with a fallback for
// non-secure contexts (HTTP localhost is fine; HTTP on a LAN IP isn't).
async function copyToClipboard(text: string, successMsg: string) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      // Fallback: synthesize a hidden textarea, select, execCommand. Works
      // in Firefox / Safari over plain HTTP where the clipboard API
      // refuses. Yes execCommand is deprecated; the clipboard API is the
      // only real option in secure contexts and execCommand is the only
      // option in non-secure ones.
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      document.body.removeChild(ta);
    }
    toast.success(successMsg);
  } catch (e) {
    toast.error(`Couldn't copy to clipboard: ${(e as Error).message}`);
  }
}
