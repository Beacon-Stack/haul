import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { Download, Upload, HardDrive, Activity, AlertTriangle, X } from "lucide-react";
import { useTorrents } from "@/api/torrents";
import { useStats } from "@/api/stats";

function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return "0 B/s";
  if (bytesPerSec < 1024) return `${bytesPerSec} B/s`;
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`;
  return `${(bytesPerSec / (1024 * 1024)).toFixed(1)} MB/s`;
}

function formatBytes(bytes: number): string {
  if (bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export default function Dashboard() {
  const { data: torrents } = useTorrents();
  const { data: stats } = useStats();

  const downloading = torrents?.filter((t) => t.status === "downloading" && !t.stalled_at) ?? [];
  const queued = torrents?.filter((t) => t.status === "queued") ?? [];
  const seeding = torrents?.filter((t) => t.status === "seeding" || t.status === "completed") ?? [];
  // Auto-stalled torrents are excluded from "Paused" so the dashboard
  // distinguishes "user paused this" (3) from "Haul auto-paused this
  // because it's stuck" (1) — that signal is what the new card surfaces.
  const paused = torrents?.filter((t) => t.status === "paused" && !t.stalled_at) ?? [];
  const stalled = torrents?.filter((t) => !!t.stalled_at) ?? [];

  const totalDown = torrents?.reduce((sum, t) => sum + t.download_rate, 0) ?? 0;
  const totalUp = torrents?.reduce((sum, t) => sum + t.upload_rate, 0) ?? 0;
  const totalSize = torrents?.reduce((sum, t) => sum + t.size, 0) ?? 0;

  // Banner dismissal — sessionStorage so it stays dismissed for this
  // browsing session but reappears on tomorrow's first visit. Keyed
  // on the count so adding a new stalled torrent re-shows the banner
  // even if the user dismissed it earlier.
  const [bannerDismissed, setBannerDismissed] = useState<number | null>(null);
  useEffect(() => {
    const v = sessionStorage.getItem("haul:stalled-banner-dismissed");
    setBannerDismissed(v ? parseInt(v, 10) : null);
  }, []);
  const showBanner = stalled.length > 0 && bannerDismissed !== stalled.length;
  const dismissBanner = () => {
    sessionStorage.setItem("haul:stalled-banner-dismissed", String(stalled.length));
    setBannerDismissed(stalled.length);
  };

  const cards = [
    { label: "Downloading", value: downloading.length, icon: Download, color: "var(--color-status-downloading)" },
    { label: "Seeding", value: seeding.length, icon: Upload, color: "var(--color-status-seeding)" },
    { label: "Paused", value: paused.length, icon: Activity, color: "var(--color-status-paused)" },
    {
      label: "Needs attention",
      value: stalled.length,
      icon: AlertTriangle,
      color: stalled.length > 0 ? "var(--color-status-stalled)" : "var(--color-text-muted)",
      to: stalled.length > 0 ? "/?status=stalled" : undefined,
    },
    { label: "Total Size", value: formatBytes(totalSize), icon: HardDrive, color: "var(--color-text-secondary)" },
  ];

  return (
    <div style={{ padding: 24, maxWidth: 900 }}>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0, letterSpacing: "-0.01em" }}>
          Dashboard
        </h1>
        <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>
          {stats?.version ? `Haul ${stats.version}` : ""}
        </p>
      </div>

      {/* Stalled banner — surfaces auto-paused torrents that need a call. */}
      {showBanner && (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "10px 14px",
            marginBottom: 16,
            background: "color-mix(in srgb, var(--color-status-stalled) 10%, transparent)",
            border: "1px solid color-mix(in srgb, var(--color-status-stalled) 35%, transparent)",
            borderRadius: 8,
            fontSize: 13,
            color: "var(--color-text-secondary)",
          }}
        >
          <AlertTriangle size={15} style={{ color: "var(--color-status-stalled)", flexShrink: 0 }} />
          <span style={{ flex: 1 }}>
            <strong style={{ color: "var(--color-text-primary)" }}>
              {stalled.length} {stalled.length === 1 ? "torrent" : "torrents"} need attention
            </strong>
            {" — "}
            paused automatically because Haul couldn't find peers.{" "}
            <Link to="/?status=stalled" style={{ color: "var(--color-status-stalled)", textDecoration: "underline" }}>
              Review
            </Link>
            .
          </span>
          <button
            onClick={dismissBanner}
            aria-label="Dismiss"
            style={{
              background: "none",
              border: "none",
              cursor: "pointer",
              color: "var(--color-text-muted)",
              padding: 2,
              display: "flex",
              alignItems: "center",
            }}
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Speed bar */}
      <div
        style={{
          display: "flex",
          gap: 24,
          padding: "14px 20px",
          background: "var(--color-bg-surface)",
          borderRadius: 8,
          border: "1px solid var(--color-border-subtle)",
          marginBottom: 20,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Download size={14} style={{ color: "var(--color-status-downloading)" }} />
          <span style={{ fontSize: 14, fontWeight: 600, color: "var(--color-text-primary)" }}>{formatSpeed(totalDown)}</span>
          <span style={{ fontSize: 11, color: "var(--color-text-muted)" }}>down</span>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Upload size={14} style={{ color: "var(--color-status-seeding)" }} />
          <span style={{ fontSize: 14, fontWeight: 600, color: "var(--color-text-primary)" }}>{formatSpeed(totalUp)}</span>
          <span style={{ fontSize: 11, color: "var(--color-text-muted)" }}>up</span>
        </div>
      </div>

      {/* Stat cards */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(180px, 1fr))", gap: 14 }}>
        {cards.map(({ label, value, icon: Icon, color, to }) => {
          const cardStyle: React.CSSProperties = {
            background: "var(--color-bg-surface)",
            border: "1px solid var(--color-border-subtle)",
            borderRadius: 8,
            padding: "16px 20px",
            textDecoration: "none",
            color: "inherit",
            display: "block",
            cursor: to ? "pointer" : "default",
          };
          const inner = (
            <>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
                <Icon size={14} style={{ color }} />
                <span style={{ fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)" }}>
                  {label}
                </span>
              </div>
              <div style={{ fontSize: 22, fontWeight: 700, color: "var(--color-text-primary)" }}>{value}</div>
            </>
          );
          if (to) {
            return <Link key={label} to={to} style={cardStyle}>{inner}</Link>;
          }
          return <div key={label} style={cardStyle}>{inner}</div>;
        })}
      </div>

      {/* Active downloads list + queued */}
      {(downloading.length > 0 || queued.length > 0) && (
        <div style={{ marginTop: 24 }}>
          <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.04em", color: "var(--color-text-muted)", margin: "0 0 10px" }}>
            Active Downloads
          </h2>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {[...downloading, ...queued].map((t) => {
              const isQueued = t.status === "queued";
              return (
                <div
                  key={t.info_hash}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 12,
                    padding: "10px 14px",
                    background: "var(--color-bg-surface)",
                    border: "1px solid var(--color-border-subtle)",
                    borderRadius: 6,
                    opacity: isQueued ? 0.55 : 1,
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 2 }}>
                      <div style={{ fontSize: 13, fontWeight: 500, color: "var(--color-text-primary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1, minWidth: 0 }}>
                        {t.name}
                      </div>
                      {isQueued && (
                        <span style={{
                          fontSize: 10,
                          padding: "1px 6px",
                          borderRadius: 4,
                          fontWeight: 600,
                          flexShrink: 0,
                          background: "color-mix(in srgb, var(--color-status-queued) 15%, transparent)",
                          color: "var(--color-status-queued)",
                          textTransform: "uppercase",
                          letterSpacing: "0.04em",
                        }}>
                          Queued
                        </span>
                      )}
                    </div>
                    <div style={{ display: "flex", gap: 12, marginTop: 2 }}>
                      <span style={{ fontSize: 11, color: "var(--color-text-muted)" }}>{(t.progress * 100).toFixed(1)}%</span>
                      {!isQueued && <span style={{ fontSize: 11, color: "var(--color-status-downloading)" }}>{formatSpeed(t.download_rate)}</span>}
                      <span style={{ fontSize: 11, color: "var(--color-text-muted)" }}>{t.seeds} seeds</span>
                    </div>
                  </div>
                  {/* Progress bar */}
                  <div style={{ width: 100, height: 4, borderRadius: 2, background: "var(--color-bg-subtle)", flexShrink: 0 }}>
                    <div
                      style={{
                        width: `${Math.min(t.progress * 100, 100)}%`,
                        height: "100%",
                        borderRadius: 2,
                        background: isQueued ? "var(--color-status-queued)" : "var(--color-status-downloading)",
                      }}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
