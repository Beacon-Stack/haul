import { useParams, Link } from "react-router-dom";
import {
  ArrowLeft,
  Activity,
  CheckCircle,
  Trash2,
  Download,
  AlertTriangle,
  Pause,
  Play,
} from "lucide-react";
import { ExternalLink } from "lucide-react";
import { useActivityEvents, useHistoryByHash, usePeerServices, type ActivityEvent, type HistoryRecord } from "@/api/activity";
import { useTorrent } from "@/api/torrents";
import { formatBytes, formatDate } from "@/shared/utils";

const EVENT_DISPLAY: Record<string, { label: string; color: string; Icon: React.ElementType }> = {
  torrent_added: { label: "Added", color: "var(--color-accent)", Icon: Download },
  torrent_completed: { label: "Completed", color: "var(--color-success)", Icon: CheckCircle },
  torrent_removed: { label: "Removed", color: "var(--color-text-muted)", Icon: Trash2 },
  torrent_failed: { label: "Failed", color: "var(--color-danger)", Icon: AlertTriangle },
  torrent_stalled: { label: "Stalled", color: "var(--color-warning)", Icon: AlertTriangle },
  torrent_state_changed: { label: "State changed", color: "var(--color-text-secondary)", Icon: Activity },
  torrent_paused: { label: "Paused", color: "var(--color-text-muted)", Icon: Pause },
  torrent_resumed: { label: "Resumed", color: "var(--color-accent)", Icon: Play },
};

export default function ActivityDetailPage() {
  const { hash = "" } = useParams<{ hash: string }>();

  // Pull both the live torrent (when present) and the event log. The
  // live record gives us the active state + canonical metadata; the
  // event log gives us the full lifecycle. Either may 404 — a torrent
  // that was hard-deleted from `torrents` still has its event trail.
  const liveQuery = useTorrent(hash);
  const eventsQuery = useActivityEvents(hash);
  const historyQuery = useHistoryByHash(hash);

  const live = liveQuery.data;
  const history = historyQuery.data;
  const events = eventsQuery.data ?? [];

  // Pull header metadata from the live record when available, else
  // synthesise from the first/last events. Required because liveQuery
  // returns 404 once a torrent is removed.
  const headerName = live?.name || history?.name || nameFromEvents(events) || hash.slice(0, 12);
  const headerSize = live?.size ?? sizeFromEvents(events);

  return (
    <div style={{ padding: "24px 32px", maxWidth: 1100, margin: "0 auto" }}>
      <Link
        to="/activity"
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 4,
          fontSize: 13,
          color: "var(--color-text-secondary)",
          textDecoration: "none",
          marginBottom: 12,
        }}
      >
        <ArrowLeft size={14} /> Back to activity
      </Link>

      <div style={{ marginBottom: 24 }}>
        <h1
          style={{
            margin: 0,
            fontSize: 20,
            fontWeight: 600,
            color: "var(--color-text-primary)",
            wordBreak: "break-word",
          }}
        >
          {headerName}
        </h1>
        <div
          style={{
            display: "flex",
            gap: 16,
            marginTop: 6,
            fontSize: 12,
            color: "var(--color-text-muted)",
            fontVariantNumeric: "tabular-nums",
            flexWrap: "wrap",
          }}
        >
          <span style={{ fontFamily: "ui-monospace, monospace" }}>{hash}</span>
          {headerSize !== undefined && headerSize > 0 && <span>· {formatBytes(headerSize)}</span>}
          {live && <span>· Status: {live.status}</span>}
        </div>
      </div>

      {/* Live actions — when the torrent still exists in the engine */}
      {live && (
        <Link
          to={`/torrents/${hash}`}
          style={{
            display: "inline-block",
            padding: "6px 12px",
            fontSize: 12,
            fontWeight: 500,
            borderRadius: 6,
            border: "1px solid var(--color-accent)",
            background: "var(--color-accent)",
            color: "#fff",
            textDecoration: "none",
            marginBottom: 16,
          }}
        >
          Open live torrent →
        </Link>
      )}

      {/* Requester back-reference */}
      <RequesterCard hash={hash} live={live} history={history} />

      {/* Event timeline */}
      <div
        style={{
          marginTop: 16,
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 8,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            padding: "10px 14px",
            background: "var(--color-bg-elevated)",
            borderBottom: "1px solid var(--color-border-subtle)",
            fontSize: 11,
            fontWeight: 600,
            textTransform: "uppercase",
            letterSpacing: 0.4,
            color: "var(--color-text-muted)",
          }}
        >
          Event timeline
        </div>

        {eventsQuery.isLoading && (
          <div style={{ padding: 24, fontSize: 13, color: "var(--color-text-muted)", textAlign: "center" }}>
            Loading…
          </div>
        )}
        {!eventsQuery.isLoading && events.length === 0 && (
          <div style={{ padding: 32, fontSize: 13, color: "var(--color-text-muted)", textAlign: "center" }}>
            No events recorded yet.
          </div>
        )}
        {events.map((e, i) => (
          <EventRow key={e.id} event={e} isLast={i === events.length - 1} />
        ))}
      </div>
    </div>
  );
}

function EventRow({ event, isLast }: { event: ActivityEvent; isLast: boolean }) {
  const display = EVENT_DISPLAY[event.event_type] ?? {
    label: event.event_type,
    color: "var(--color-text-secondary)",
    Icon: Activity,
  };
  const { Icon, color, label } = display;

  // Render payload as a JSON-ish key list. Skip the noisy "name"
  // field — it's always the torrent name and clutters the view.
  const payloadEntries: [string, unknown][] = [];
  if (event.payload && typeof event.payload === "object") {
    for (const [k, v] of Object.entries(event.payload)) {
      if (k === "name") continue;
      payloadEntries.push([k, v]);
    }
  }

  return (
    <div
      style={{
        display: "flex",
        gap: 12,
        padding: "12px 14px",
        borderBottom: isLast ? "none" : "1px solid var(--color-border-subtle)",
      }}
    >
      <Icon size={14} style={{ color, flexShrink: 0, marginTop: 2 }} />
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
          <span style={{ fontSize: 13, fontWeight: 500, color: "var(--color-text-primary)" }}>
            {label}
          </span>
          <span style={{ fontSize: 11, color: "var(--color-text-muted)", flexShrink: 0 }}>
            {formatDate(event.occurred_at, true)}
          </span>
        </div>
        {payloadEntries.length > 0 && (
          <div style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: "2px 12px" }}>
            {payloadEntries.map(([k, v]) => (
              <span
                key={k}
                style={{ fontSize: 11, color: "var(--color-text-muted)" }}
              >
                <span style={{ fontWeight: 500 }}>{k}:</span> {formatPayloadValue(v)}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function formatPayloadValue(v: unknown): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (v === null || v === undefined) return "—";
  return JSON.stringify(v);
}

interface LiveLike {
  category?: string;
  save_path?: string;
}

function RequesterCard({
  hash,
  live,
  history,
}: {
  hash: string;
  live: LiveLike | undefined;
  history: HistoryRecord | undefined;
}) {
  // Prefer the history record's save_path/category — it survives a
  // remove operation. Fall back to the live record when history is
  // absent (DB nil-short-circuit, etc).
  const savePath = history?.save_path ?? live?.save_path;
  const category = history?.category ?? live?.category;

  const rows: [string, string | undefined][] = [
    ["Info hash", hash],
    ["Save path", savePath],
    ["Category", category],
  ];

  if (history?.added_at) rows.push(["Added", history.added_at]);
  if (history?.completed_at) rows.push(["Completed", history.completed_at]);
  if (history?.removed_at) rows.push(["Removed", history.removed_at]);

  return (
    <>
      {history?.requester && <RequesterBadge h={history} />}

      <div
        style={{
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 8,
          padding: "12px 14px",
          display: "grid",
          gridTemplateColumns: "auto 1fr",
          rowGap: 6,
          columnGap: 16,
          fontSize: 12,
        }}
      >
        {rows.map(([k, v]) =>
          v ? (
            <Row key={k} k={k} v={v} />
          ) : null,
        )}
      </div>
    </>
  );
}

function RequesterBadge({ h }: { h: HistoryRecord }) {
  const { data: peers } = usePeerServices();

  const isPilot = h.requester === "pilot";
  const isPrism = h.requester === "prism";
  const label = isPilot ? "Open in Pilot" : isPrism ? "Open in Prism" : `Open in ${h.requester}`;

  // Pulse hands us each service's APIURL (the base URL of the Go
  // binary, which also serves the UI from /). The registered URL is
  // often a docker-internal hostname ("http://ddf771b21e64:8383")
  // that the user's browser can't resolve, so we rewrite the host
  // to whatever the user is currently using to reach Haul, keeping
  // the registered port + path. This makes deep-links work in the
  // common docker-compose setup (one host, services on different
  // ports) without forcing users to set ADVERTISE_HOST.
  const baseURL = browserReachableURL(h.requester ? peers?.[h.requester] : undefined);
  let deepLink: string | undefined;
  if (baseURL && isPilot && h.series_id) {
    deepLink = h.episode_id
      ? `${baseURL}/series/${h.series_id}/episodes/${h.episode_id}`
      : `${baseURL}/series/${h.series_id}`;
  } else if (baseURL && isPrism && h.movie_id) {
    deepLink = `${baseURL}/movies/${h.movie_id}`;
  }

  const cardStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: 8,
    padding: "10px 14px",
    marginBottom: 10,
    background: "color-mix(in srgb, var(--color-accent) 8%, transparent)",
    border: "1px solid color-mix(in srgb, var(--color-accent) 30%, transparent)",
    borderRadius: 8,
    fontSize: 12,
    color: "var(--color-text-secondary)",
    flexWrap: "wrap",
    textDecoration: "none",
    cursor: deepLink ? "pointer" : "default",
  };

  const inner = (
    <>
      <span style={{ fontWeight: 600, color: "var(--color-accent)", display: "inline-flex", alignItems: "center", gap: 4 }}>
        {label}
        {deepLink && <ExternalLink size={12} />}
      </span>
      {h.tmdb_id ? <span>· TMDB <code>{h.tmdb_id}</code></span> : null}
      {h.season ? <span>· S{String(h.season).padStart(2, "0")}{h.episode ? `E${String(h.episode).padStart(2, "0")}` : ""}</span> : null}
      {h.series_id && <span>· Series <code>{h.series_id.slice(0, 8)}</code></span>}
      {h.movie_id && <span>· Movie <code>{h.movie_id.slice(0, 8)}</code></span>}
      {h.episode_id && <span>· Episode <code>{h.episode_id.slice(0, 8)}</code></span>}
    </>
  );

  if (deepLink) {
    return (
      <a href={deepLink} target="_blank" rel="noopener noreferrer" style={cardStyle}>
        {inner}
      </a>
    );
  }
  return <div style={cardStyle}>{inner}</div>;
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <>
      <span style={{ color: "var(--color-text-muted)" }}>{k}</span>
      <span style={{ color: "var(--color-text-secondary)", fontFamily: "ui-monospace, monospace", wordBreak: "break-all" }}>{v}</span>
    </>
  );
}

// browserReachableURL takes a Pulse-registered base URL and returns
// one the user's browser can actually open. The registered URL may
// use a docker-internal hostname (a container ID or a compose
// service name) that only resolves from inside the docker network.
// If the hostname doesn't look browser-resolvable, we substitute
// the current page's hostname and keep the registered port — which
// matches the typical "one host, services on different ports"
// compose deployment.
function browserReachableURL(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    return undefined;
  }
  if (looksBrowserResolvable(parsed.hostname)) return parsed.toString().replace(/\/$/, "");
  parsed.hostname = window.location.hostname;
  return parsed.toString().replace(/\/$/, "");
}

function looksBrowserResolvable(host: string): boolean {
  // localhost and any dotted name (real DNS or IP) are trusted as-is.
  // Bare names like "pilot" or container IDs ("ddf771b21e64") are not.
  return host === "localhost" || host.includes(".");
}

function nameFromEvents(events: ActivityEvent[]): string | undefined {
  for (const e of events) {
    if (e.payload && typeof e.payload === "object" && "name" in e.payload) {
      const n = (e.payload as Record<string, unknown>).name;
      if (typeof n === "string" && n) return n;
    }
  }
  return undefined;
}

function sizeFromEvents(events: ActivityEvent[]): number | undefined {
  for (const e of events) {
    if (e.payload && typeof e.payload === "object" && "size" in e.payload) {
      const s = (e.payload as Record<string, unknown>).size;
      if (typeof s === "number") return s;
    }
  }
  return undefined;
}
