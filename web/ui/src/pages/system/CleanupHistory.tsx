import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { ArrowLeft, RotateCcw, Trash2 } from "lucide-react";
import {
  useCleanupHistory,
  useRestoreCleanupHistory,
  usePurgeCleanupHistory,
  type CleanupHistoryEntry,
} from "@/api/adminDiagnostics";

// Settings → System → Cleanup History. Soft-deleted rows from the
// Diagnostics tab live here for the configured retention window
// (default 30 days). Each row can be restored back to its source table.
// A manual purge button hard-deletes rows older than N days, in case
// the operator wants to clean out the trash early.

export default function CleanupHistoryPage() {
  const [purgeDays, setPurgeDays] = useState(30);
  const { data: entries, isLoading, error } = useCleanupHistory({ limit: 200 });
  const restore = useRestoreCleanupHistory();
  const purge = usePurgeCleanupHistory();

  if (isLoading) {
    return <Wrapper><div style={emptyStyle}>Loading…</div></Wrapper>;
  }
  if (error) {
    return <Wrapper><div style={errorStyle}>Failed to load cleanup history.</div></Wrapper>;
  }

  return (
    <Wrapper>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 20 }}>
        <div style={{ fontSize: 13, color: "var(--color-text-secondary)" }}>
          {entries && entries.length > 0
            ? `${entries.length} soft-deleted row${entries.length === 1 ? "" : "s"} within retention.`
            : "No soft-deleted rows. Cleanups from the diagnostics tab will appear here."}
        </div>
        <PurgeControl
          purgeDays={purgeDays}
          onPurgeDaysChange={setPurgeDays}
          loading={purge.isPending}
          onPurge={() => {
            purge.mutate(purgeDays, {
              onSuccess: (res) => toast.success(`Permanently deleted ${res.rows_purged} cleanup_history row${res.rows_purged === 1 ? "" : "s"}`),
              onError: (e) => toast.error((e as Error).message),
            });
          }}
        />
      </div>

      {entries && entries.length > 0 && (
        <div
          style={{
            background: "var(--color-bg-elevated)",
            border: "1px solid var(--color-border-default)",
            borderRadius: 8,
            overflow: "hidden",
          }}
        >
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
            <thead>
              <tr style={{ textAlign: "left", color: "var(--color-text-muted)", background: "var(--color-bg-subtle)" }}>
                <th style={cellStyle}>Deleted</th>
                <th style={cellStyle}>Diagnostic</th>
                <th style={cellStyle}>Table</th>
                <th style={cellStyle}>Row</th>
                <th style={{ ...cellStyle, width: 1 }}></th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <Row
                  key={e.id}
                  entry={e}
                  loading={restore.isPending && restore.variables === e.id}
                  onRestore={() => {
                    restore.mutate(e.id, {
                      onSuccess: (res) => {
                        if (res.restored) toast.success("Restored");
                        else toast.warning(res.reason ?? "Could not restore");
                      },
                      onError: (err) => toast.error((err as Error).message),
                    });
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Wrapper>
  );
}

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ padding: "24px 32px", maxWidth: 1100, margin: "0 auto" }}>
      <Link
        to="/system/diagnostics"
        style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--color-text-muted)", marginBottom: 12 }}
      >
        <ArrowLeft size={12} /> Diagnostics
      </Link>
      <h1 style={{ margin: "0 0 6px", fontSize: 22, fontWeight: 600, color: "var(--color-text-primary)" }}>
        Cleanup history
      </h1>
      <p style={{ margin: "0 0 20px", color: "var(--color-text-secondary)", fontSize: 13 }}>
        Soft-deleted rows from the diagnostics tab. Stored as JSONB snapshots; restorable until purged.
      </p>
      {children}
    </div>
  );
}

function Row({ entry, loading, onRestore }: { entry: CleanupHistoryEntry; loading: boolean; onRestore: () => void }) {
  const when = new Date(entry.deleted_at).toLocaleString();
  const pkPreview = entry.source_pk.length > 16 ? `${entry.source_pk.slice(0, 12)}…` : entry.source_pk;
  return (
    <tr style={{ borderTop: "1px solid var(--color-border-subtle)" }}>
      <td style={{ ...cellStyle, color: "var(--color-text-secondary)", fontSize: 12 }}>{when}</td>
      <td style={cellStyle}>{entry.diagnostic}</td>
      <td style={{ ...cellStyle, fontFamily: "var(--font-family-mono)", fontSize: 11 }}>{entry.source_table}</td>
      <td style={{ ...cellStyle, fontFamily: "var(--font-family-mono)", fontSize: 11 }}>{pkPreview}</td>
      <td style={{ ...cellStyle, whiteSpace: "nowrap" }}>
        <button onClick={onRestore} disabled={loading} style={restoreButtonStyle}>
          {loading ? "…" : <><RotateCcw size={12} /> Restore</>}
        </button>
      </td>
    </tr>
  );
}

function PurgeControl(props: {
  purgeDays: number;
  onPurgeDaysChange: (v: number) => void;
  loading: boolean;
  onPurge: () => void;
}) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
      <label style={{ fontSize: 12, color: "var(--color-text-muted)" }}>Purge older than</label>
      <input
        type="number"
        min={1}
        value={props.purgeDays}
        onChange={(e) => props.onPurgeDaysChange(Math.max(1, parseInt(e.target.value || "1", 10)))}
        style={{
          width: 60,
          padding: "4px 8px",
          background: "var(--color-bg-elevated)",
          border: "1px solid var(--color-border-default)",
          borderRadius: 6,
          color: "var(--color-text-primary)",
          fontSize: 12,
        }}
      />
      <span style={{ fontSize: 12, color: "var(--color-text-muted)" }}>days</span>
      <button onClick={props.onPurge} disabled={props.loading} style={purgeButtonStyle}>
        <Trash2 size={12} /> {props.loading ? "Purging…" : "Purge now"}
      </button>
    </div>
  );
}

const cellStyle: React.CSSProperties = { padding: "8px 12px" };
const emptyStyle: React.CSSProperties = {
  padding: 40,
  textAlign: "center",
  color: "var(--color-text-secondary)",
  background: "var(--color-bg-elevated)",
  borderRadius: 8,
  fontSize: 14,
};
const errorStyle: React.CSSProperties = { ...emptyStyle, color: "var(--color-danger)" };
const restoreButtonStyle: React.CSSProperties = {
  padding: "4px 10px",
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "transparent",
  color: "var(--color-text-primary)",
  fontSize: 12,
  cursor: "pointer",
  display: "inline-flex",
  alignItems: "center",
  gap: 4,
};
const purgeButtonStyle: React.CSSProperties = {
  padding: "5px 12px",
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "var(--color-bg-subtle)",
  color: "var(--color-text-primary)",
  fontSize: 12,
  cursor: "pointer",
  display: "inline-flex",
  alignItems: "center",
  gap: 4,
};
