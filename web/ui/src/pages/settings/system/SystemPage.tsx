import { Link } from "react-router-dom";
import { History, Stethoscope } from "lucide-react";
import { useHealth } from "@/api/health";
import PageHeader from "@beacon-shared/PageHeader";
import SystemCard from "@beacon-shared/SystemCard";
import SystemInfo from "@beacon-shared/SystemInfo";

function formatDiskBytes(b: number): string {
  const GB = 1024 ** 3;
  if (b <= 0) return "0 B";
  if (b < 1024 * GB) return `${(b / GB).toFixed(1)} GB`;
  return `${(b / (1024 * GB)).toFixed(2)} TB`;
}

function EngineHealth() {
  const { data: health, isLoading, error } = useHealth();
  const diskPercent =
    health && health.disk_total_bytes > 0
      ? ((health.disk_total_bytes - health.disk_free_bytes) / health.disk_total_bytes) * 100
      : 0;

  return (
    <SystemCard title="Health">
      {isLoading && <div className="skeleton" style={{ height: 40, margin: 16, borderRadius: 4 }} />}
      {error && <div style={{ padding: 16, fontSize: 13, color: "var(--color-text-secondary)" }}>Failed to load health data.</div>}
      {health && (
        <div style={{ padding: 16, display: "flex", flexDirection: "column", gap: 16 }}>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: 12 }}>
            {[
              { label: "Engine", value: health.engine_status, color: health.engine_status === "healthy" ? "var(--color-success)" : "var(--color-danger)" },
              { label: "VPN", value: health.vpn_active ? "Protected" : "Not active", color: health.vpn_active ? "var(--color-success)" : "var(--color-danger)" },
              { label: "External IP", value: health.external_ip || "Unknown" },
              { label: "Peers", value: String(health.peers_connected) },
              { label: "Stalled", value: String(health.stalled_count), color: health.stalled_count > 0 ? "var(--color-warning)" : undefined },
            ].map(({ label, value, color }) => (
              <div key={label} style={{ background: "var(--color-bg-elevated)", borderRadius: 6, padding: "10px 14px" }}>
                <div style={{ fontSize: 10, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--color-text-muted)", marginBottom: 2 }}>{label}</div>
                <div style={{ fontSize: 14, fontWeight: 500, color: color || "var(--color-text-primary)", textTransform: "capitalize" }}>{value}</div>
              </div>
            ))}
          </div>
          <div>
            <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 6 }}>
              <span style={{ fontSize: 12, color: "var(--color-text-muted)" }}>
                Disk: {formatDiskBytes(health.disk_total_bytes - health.disk_free_bytes)} used of {formatDiskBytes(health.disk_total_bytes)}
              </span>
              <span style={{ fontSize: 12, color: "var(--color-text-secondary)", fontWeight: 500 }}>{diskPercent.toFixed(1)}%</span>
            </div>
            <div style={{ height: 6, borderRadius: 3, background: "var(--color-bg-subtle)" }}>
              <div
                style={{
                  width: `${Math.min(diskPercent, 100)}%`,
                  height: "100%",
                  borderRadius: 3,
                  background: diskPercent > 90 ? "var(--color-danger)" : diskPercent > 75 ? "var(--color-warning)" : "var(--color-accent)",
                }}
              />
            </div>
            <div style={{ fontSize: 11, color: "var(--color-text-muted)", marginTop: 4 }}>{formatDiskBytes(health.disk_free_bytes)} free</div>
          </div>
        </div>
      )}
    </SystemCard>
  );
}

const linkRow = {
  display: "flex",
  alignItems: "center",
  gap: 10,
  padding: "10px 16px",
  fontSize: 13,
  color: "var(--color-text-primary)",
  textDecoration: "none",
} as const;

function Maintenance() {
  return (
    <SystemCard title="Maintenance" subtitle="Find and clean up stale or orphaned records.">
      <Link to="/system/diagnostics" style={{ ...linkRow, borderBottom: "1px solid var(--color-border-subtle)" }}>
        <Stethoscope size={15} color="var(--color-text-muted)" /> Diagnostics
      </Link>
      <Link to="/system/cleanup-history" style={linkRow}>
        <History size={15} color="var(--color-text-muted)" /> Cleanup history
      </Link>
    </SystemCard>
  );
}

export default function SystemPage() {
  return (
    <div style={{ padding: 24, maxWidth: 900 }}>
      <PageHeader title="System" description="What's running and how it's doing." />
      <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
        <SystemInfo app="Haul" service="haul" image="ghcr.io/beacon-stack/haul" />
        <EngineHealth />
        <Maintenance />
      </div>
    </div>
  );
}
