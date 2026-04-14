import { useState, useEffect, useRef } from "react";
import { Activity, Download, Upload, Trash2, AlertTriangle, CheckCircle } from "lucide-react";

interface ActivityEvent {
  type: string;
  timestamp: string;
  info_hash?: string;
  data?: Record<string, unknown>;
}

const EVENT_ICONS: Record<string, React.ElementType> = {
  torrent_added: Download,
  torrent_completed: CheckCircle,
  torrent_removed: Trash2,
  torrent_failed: AlertTriangle,
  torrent_stalled: AlertTriangle,
  torrent_state_changed: Activity,
  speed_update: Upload,
};

const EVENT_COLORS: Record<string, string> = {
  torrent_added: "var(--color-accent)",
  torrent_completed: "var(--color-success)",
  torrent_removed: "var(--color-text-muted)",
  torrent_failed: "var(--color-danger)",
  torrent_stalled: "var(--color-warning)",
  torrent_state_changed: "var(--color-text-secondary)",
};

function formatTime(ts: string): string {
  const d = new Date(ts);
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function eventLabel(type: string): string {
  return type.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

export default function ActivityPage() {
  const [events, setEvents] = useState<ActivityEvent[]>([]);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${window.location.host}/api/v1/ws`);
    wsRef.current = ws;

    ws.onmessage = (ev) => {
      try {
        const event: ActivityEvent = JSON.parse(ev.data);
        if (event.type === "speed_update" || event.type === "health_update") return;
        setEvents((prev) => [event, ...prev].slice(0, 200));
      } catch {}
    };

    return () => ws.close();
  }, []);

  return (
    <div style={{ padding: 24, maxWidth: 900, margin: "0 auto" }}>
      <div style={{ marginBottom: 20 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>Activity</h1>
        <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>
          Live event stream — {events.length} event{events.length !== 1 ? "s" : ""}
        </p>
      </div>

      {events.length === 0 && (
        <div style={{ textAlign: "center", padding: "60px 0" }}>
          <Activity size={32} style={{ color: "var(--color-text-muted)", marginBottom: 12 }} />
          <p style={{ fontSize: 14, color: "var(--color-text-secondary)", fontWeight: 500 }}>No activity yet</p>
          <p style={{ fontSize: 13, color: "var(--color-text-muted)", margin: "6px 0 0" }}>Events will appear here in real-time as torrents are added, completed, or changed.</p>
        </div>
      )}

      {events.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          {events.map((e, i) => {
            const Icon = EVENT_ICONS[e.type] || Activity;
            const color = EVENT_COLORS[e.type] || "var(--color-text-muted)";
            const name = (e.data?.name as string) || e.info_hash?.slice(0, 8) || "";

            return (
              <div
                key={`${e.timestamp}-${i}`}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                  padding: "8px 14px",
                  background: "var(--color-bg-surface)",
                  border: "1px solid var(--color-border-subtle)",
                  borderRadius: 6,
                }}
              >
                <Icon size={14} style={{ color, flexShrink: 0 }} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ fontSize: 13, fontWeight: 500, color: "var(--color-text-primary)" }}>
                    {eventLabel(e.type)}
                  </span>
                  {name && (
                    <span style={{ fontSize: 12, color: "var(--color-text-muted)", marginLeft: 8 }}>
                      {name}
                    </span>
                  )}
                </div>
                <span style={{ fontSize: 11, color: "var(--color-text-muted)", flexShrink: 0 }}>
                  {formatTime(e.timestamp)}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
