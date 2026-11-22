import { Link } from "react-router-dom";
import { History, Stethoscope } from "lucide-react";
import { useHealth } from "@/api/health";
import PageHeader from "@beacon-shared/PageHeader";
import SystemCard from "@beacon-shared/SystemCard";
import HealthCard, { type HealthLine, type HealthOverall } from "@beacon-shared/HealthCard";
import SystemInfo from "@beacon-shared/SystemInfo";

function formatDiskBytes(b: number): string {
  const GB = 1024 ** 3;
  if (b <= 0) return "0 B";
  if (b < 1024 * GB) return `${(b / GB).toFixed(1)} GB`;
  return `${(b / (1024 * GB)).toFixed(2)} TB`;
}

function EngineHealth() {
  const { data: h, isLoading, error, refetch } = useHealth();
  let overall: HealthOverall | undefined;
  let lines: HealthLine[] = [];
  if (h) {
    const used = h.disk_total_bytes - h.disk_free_bytes;
    const diskPct = h.disk_total_bytes > 0 ? (used / h.disk_total_bytes) * 100 : 0;
    const engineOk = h.engine_status === "healthy";
    lines = [
      { name: "Engine", level: engineOk ? "ok" : "error", message: h.engine_status },
      { name: "VPN", level: h.vpn_active ? "ok" : "error", message: h.vpn_active ? `Protected (${h.external_ip || "IP unknown"})` : "Not active" },
      { name: "Stalled torrents", level: h.stalled_count > 0 ? "warning" : "ok", message: String(h.stalled_count) },
      { name: "Peers", level: "ok", message: String(h.peers_connected) },
      {
        name: "Disk",
        level: diskPct > 90 ? "warning" : "ok",
        message: `${formatDiskBytes(used)} used of ${formatDiskBytes(h.disk_total_bytes)} (${formatDiskBytes(h.disk_free_bytes)} free)`,
      },
    ];
    overall = lines.some((l) => l.level === "error") ? "unhealthy" : lines.some((l) => l.level === "warning") ? "degraded" : "healthy";
  }
  return (
    <HealthCard
      overall={overall}
      lines={lines}
      loading={isLoading}
      failed={!!error}
      onCheck={async () => {
        const r = await refetch();
        if (r.error) throw r.error;
      }}
    />
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
