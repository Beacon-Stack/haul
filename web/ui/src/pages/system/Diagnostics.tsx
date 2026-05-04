import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { AlertTriangle, ChevronDown, ChevronRight, Trash2, History, FileText } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import {
  useDiagnostics,
  useDiagnostic,
  useCleanupDiagnostic,
  type DiagnosticSummary,
  type CleanupMode,
} from "@/api/adminDiagnostics";
import { apiFetch } from "@/api/client";

// Settings → System → Diagnostics. Lists every named diagnostic the
// service knows about. Each card expands inline to show matching rows
// and a "Clean up all" action. Soft delete is the default; a checkbox
// in the confirmation modal switches to permanent.

export default function DiagnosticsPage() {
  const { data: summaries, isLoading, error } = useDiagnostics();

  if (isLoading) {
    return <Wrapper><div style={emptyStyle}>Loading…</div></Wrapper>;
  }
  if (error) {
    return (
      <Wrapper>
        <div style={errorStyle}>
          Failed to load diagnostics. The endpoint may be disabled — set
          <code style={inlineCodeStyle}>HAUL_ADMIN_DIAGNOSTICS_ENABLED=true</code> in your env to enable.
        </div>
      </Wrapper>
    );
  }

  const total = (summaries ?? []).reduce((s, d) => s + d.row_count, 0);

  return (
    <Wrapper>
      {total === 0 ? (
        <div style={{ ...emptyStyle, color: "var(--color-success)" }}>
          ✓ All clean. No diagnostics matched any rows.
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {(summaries ?? []).map((d) => (
            <DiagnosticCard key={d.name} summary={d} />
          ))}
        </div>
      )}
    </Wrapper>
  );
}

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ padding: "24px 32px", maxWidth: 1100, margin: "0 auto" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", marginBottom: 24 }}>
        <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600, color: "var(--color-text-primary)" }}>
          System diagnostics
        </h1>
        <div style={{ display: "flex", gap: 14 }}>
          <Link
            to="/system/logs"
            style={{ color: "var(--color-text-secondary)", fontSize: 13, display: "inline-flex", alignItems: "center", gap: 4 }}
          >
            <FileText size={14} /> Logs
          </Link>
          <Link
            to="/system/cleanup-history"
            style={{ color: "var(--color-text-secondary)", fontSize: 13, display: "inline-flex", alignItems: "center", gap: 4 }}
          >
            <History size={14} /> Cleanup history
          </Link>
        </div>
      </div>
      <p style={{ margin: "0 0 16px", color: "var(--color-text-secondary)", fontSize: 13 }}>
        Each card detects rows that look stale or orphaned. Cleanup is soft by default — deleted rows go to the cleanup history and can be restored within the retention window.
      </p>
      {children}
    </div>
  );
}

function DiagnosticCard({ summary }: { summary: DiagnosticSummary }) {
  const [open, setOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [hardDelete, setHardDelete] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const detail = useDiagnostic(open ? summary.name : null);
  const cleanup = useCleanupDiagnostic();
  const qc = useQueryClient();

  // undoSoftCleanup re-issues a restore request for each cleanup_history
  // ID returned from the just-completed soft delete. Restore endpoints
  // are independent (one fail doesn't block others); use Promise.allSettled
  // so a PK-conflict on one row doesn't tank the rest.
  async function undoSoftCleanup(historyIDs: number[]) {
    const results = await Promise.allSettled(
      historyIDs.map((id) =>
        apiFetch<{ restored: boolean }>(`/admin/cleanup-history/${id}/restore`, { method: "POST" })
      )
    );
    const ok = results.filter((r) => r.status === "fulfilled" && (r.value as { restored: boolean }).restored).length;
    qc.invalidateQueries({ queryKey: ["admin", "diagnostics"] });
    qc.invalidateQueries({ queryKey: ["admin", "cleanup-history"] });
    qc.invalidateQueries({ queryKey: ["torrents"] });
    if (ok === historyIDs.length) {
      toast.success(`Restored ${ok} row${ok === 1 ? "" : "s"}`);
    } else {
      toast.warning(`Restored ${ok} of ${historyIDs.length} rows (others may already exist)`);
    }
  }

  const isFlagged = summary.row_count > 0;
  const rows = detail.data?.rows ?? [];
  const allSelected = rows.length > 0 && selected.size === rows.length;
  const someSelected = selected.size > 0 && !allSelected;
  // What gets cleaned up: explicit selection if any, otherwise "all".
  const cleanupTargets = selected.size > 0
    ? { ids: Array.from(selected) }
    : { all: true };
  const cleanupCount = selected.size > 0 ? selected.size : rows.length;

  function toggleRow(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected((prev) =>
      prev.size === rows.length ? new Set() : new Set(rows.map((r) => r.id))
    );
  }

  return (
    <div
      style={{
        background: "var(--color-bg-elevated)",
        border: `1px solid ${isFlagged ? "var(--color-warning)" : "var(--color-border-default)"}`,
        borderRadius: 8,
        overflow: "hidden",
      }}
    >
      <div
        style={{ padding: "14px 18px", display: "flex", alignItems: "center", gap: 12, cursor: "pointer" }}
        onClick={() => setOpen((v) => !v)}
      >
        {open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        <div style={{ flex: 1 }}>
          <div style={{ fontWeight: 600, fontSize: 14, color: "var(--color-text-primary)" }}>{summary.description}</div>
          <div style={{ fontSize: 12, color: "var(--color-text-muted)", marginTop: 2 }}>{summary.name}</div>
        </div>
        <div
          style={{
            padding: "3px 10px",
            borderRadius: 999,
            background: isFlagged ? "var(--color-warning)" : "var(--color-bg-subtle)",
            color: isFlagged ? "var(--color-bg-base)" : "var(--color-text-muted)",
            fontSize: 12,
            fontWeight: 600,
            minWidth: 28,
            textAlign: "center",
          }}
        >
          {summary.row_count}
        </div>
      </div>

      {open && (
        <div style={{ borderTop: "1px solid var(--color-border-subtle)", padding: 18 }}>
          {detail.isLoading ? (
            <div style={{ color: "var(--color-text-muted)", fontSize: 13 }}>Loading rows…</div>
          ) : rows.length ? (
            <>
              <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
                <thead>
                  <tr style={{ textAlign: "left", color: "var(--color-text-muted)" }}>
                    <th style={{ ...cellStyle, width: 28 }}>
                      <input
                        type="checkbox"
                        checked={allSelected}
                        ref={(el) => { if (el) el.indeterminate = someSelected; }}
                        onChange={toggleAll}
                        title={allSelected ? "Deselect all" : "Select all"}
                        style={{ cursor: "pointer" }}
                      />
                    </th>
                    <th style={cellStyle}>ID</th>
                    <th style={cellStyle}>Summary</th>
                    <th style={cellStyle}>Why</th>
                    <th style={{ ...cellStyle, width: 1 }}></th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => {
                    const isSelected = selected.has(r.id);
                    return (
                      <tr
                        key={r.id}
                        onClick={() => toggleRow(r.id)}
                        style={{
                          borderTop: "1px solid var(--color-border-subtle)",
                          background: isSelected ? "var(--color-accent-muted)" : "transparent",
                          cursor: "pointer",
                        }}
                      >
                        <td style={{ ...cellStyle, width: 28 }}>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => toggleRow(r.id)}
                            onClick={(e) => e.stopPropagation()}
                            style={{ cursor: "pointer" }}
                          />
                        </td>
                        <td style={{ ...cellStyle, fontFamily: "var(--font-family-mono)", fontSize: 11 }}>
                          {r.id.length > 16 ? `${r.id.slice(0, 12)}…` : r.id}
                        </td>
                        <td style={cellStyle}>{r.summary}</td>
                        <td style={{ ...cellStyle, color: "var(--color-text-muted)", fontSize: 12 }}>{r.why_flagged}</td>
                        <td style={{ ...cellStyle, whiteSpace: "nowrap", textAlign: "right" }}>
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              setSelected(new Set([r.id]));
                              setConfirmOpen(true);
                            }}
                            title="Clean up just this row"
                            style={iconButtonStyle}
                          >
                            <Trash2 size={13} />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              <div style={{ marginTop: 14, display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <div style={{ fontSize: 12, color: "var(--color-text-muted)" }}>
                  {selected.size > 0
                    ? `${selected.size} selected`
                    : `Click rows or use the checkboxes to select. Clean up acts on selection — or all rows when nothing is selected.`}
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  {selected.size > 0 && (
                    <button onClick={() => setSelected(new Set())} style={cancelButtonStyle}>
                      Clear selection
                    </button>
                  )}
                  <button onClick={() => setConfirmOpen(true)} style={dangerButtonStyle}>
                    <Trash2 size={14} />
                    {selected.size > 0
                      ? ` Clean up ${selected.size} selected`
                      : ` Clean up all ${rows.length}`}
                  </button>
                </div>
              </div>
            </>
          ) : (
            <div style={{ color: "var(--color-text-muted)", fontSize: 13 }}>No matching rows.</div>
          )}
        </div>
      )}

      {confirmOpen && detail.data && (
        <ConfirmModal
          name={summary.description}
          rowCount={cleanupCount}
          suggested={detail.data.rows[0]?.suggested_action ?? "Delete the matching rows"}
          hardDelete={hardDelete}
          onHardDeleteChange={setHardDelete}
          onCancel={() => {
            setConfirmOpen(false);
            setHardDelete(false);
          }}
          onConfirm={() => {
            const mode: CleanupMode = hardDelete ? "hard" : "soft";
            cleanup.mutate(
              { name: summary.name, body: { ...cleanupTargets, mode } },
              {
                onSuccess: (res) => {
                  if (mode === "hard") {
                    toast.success(
                      `Permanently deleted ${res.rows_deleted} row${res.rows_deleted === 1 ? "" : "s"}`
                    );
                  } else {
                    // Soft delete — surface an Undo action that calls
                    // restore on each captured history id. 10s window
                    // (sonner default ~4s is too tight for "did I mean
                    // to do that?").
                    const historyIDs = res.history_entry_ids ?? [];
                    toast.success(
                      `Moved ${res.rows_deleted} row${res.rows_deleted === 1 ? "" : "s"} to cleanup history`,
                      historyIDs.length > 0
                        ? {
                            duration: 10_000,
                            action: {
                              label: "Undo",
                              onClick: () => { void undoSoftCleanup(historyIDs); },
                            },
                          }
                        : undefined
                    );
                  }
                  setConfirmOpen(false);
                  setHardDelete(false);
                  setSelected(new Set()); // selection is stale after delete
                },
                onError: (e) => toast.error((e as Error).message),
              }
            );
          }}
          loading={cleanup.isPending}
        />
      )}
    </div>
  );
}

function ConfirmModal(props: {
  name: string;
  rowCount: number;
  suggested: string;
  hardDelete: boolean;
  onHardDeleteChange: (v: boolean) => void;
  onCancel: () => void;
  onConfirm: () => void;
  loading: boolean;
}) {
  return (
    <div style={modalBackdropStyle} onClick={props.onCancel}>
      <div style={modalStyle} onClick={(e) => e.stopPropagation()}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
          <AlertTriangle size={20} color="var(--color-warning)" />
          <h3 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: "var(--color-text-primary)" }}>
            Clean up {props.rowCount} row{props.rowCount === 1 ? "" : "s"}?
          </h3>
        </div>
        <div style={{ fontSize: 13, color: "var(--color-text-secondary)", marginBottom: 14 }}>
          {props.name} — {props.suggested}.
        </div>
        <label
          style={{
            display: "flex",
            alignItems: "flex-start",
            gap: 8,
            padding: 10,
            background: props.hardDelete ? "var(--color-bg-subtle)" : "transparent",
            border: `1px solid ${props.hardDelete ? "var(--color-danger)" : "var(--color-border-subtle)"}`,
            borderRadius: 6,
            fontSize: 12,
            color: "var(--color-text-primary)",
            cursor: "pointer",
            marginBottom: 16,
          }}
        >
          <input
            type="checkbox"
            checked={props.hardDelete}
            onChange={(e) => props.onHardDeleteChange(e.target.checked)}
            style={{ marginTop: 2 }}
          />
          <span>
            <strong>Permanently delete</strong> (cannot be undone). Default is soft-delete: rows move to the
            cleanup history and can be restored within the retention window.
          </span>
        </label>
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={props.onCancel} disabled={props.loading} style={cancelButtonStyle}>
            Cancel
          </button>
          <button onClick={props.onConfirm} disabled={props.loading} style={dangerButtonStyle}>
            {props.loading ? "Cleaning…" : props.hardDelete ? "Delete permanently" : "Move to cleanup history"}
          </button>
        </div>
      </div>
    </div>
  );
}

const cellStyle: React.CSSProperties = { padding: "6px 8px" };
const emptyStyle: React.CSSProperties = {
  padding: 40,
  textAlign: "center",
  color: "var(--color-text-secondary)",
  background: "var(--color-bg-elevated)",
  borderRadius: 8,
  fontSize: 14,
};
const errorStyle: React.CSSProperties = { ...emptyStyle, color: "var(--color-danger)" };
const inlineCodeStyle: React.CSSProperties = {
  margin: "0 4px",
  padding: "1px 6px",
  background: "var(--color-bg-subtle)",
  borderRadius: 4,
  fontFamily: "var(--font-family-mono)",
  fontSize: 12,
};
const modalBackdropStyle: React.CSSProperties = {
  position: "fixed",
  inset: 0,
  background: "rgba(0,0,0,0.6)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  zIndex: 100,
};
const modalStyle: React.CSSProperties = {
  background: "var(--color-bg-elevated)",
  borderRadius: 10,
  border: "1px solid var(--color-border-default)",
  padding: 24,
  width: 480,
  maxWidth: "90vw",
  boxShadow: "var(--shadow-modal)",
};
const cancelButtonStyle: React.CSSProperties = {
  padding: "7px 14px",
  borderRadius: 6,
  border: "1px solid var(--color-border-default)",
  background: "transparent",
  color: "var(--color-text-secondary)",
  fontSize: 13,
  cursor: "pointer",
};
const dangerButtonStyle: React.CSSProperties = {
  padding: "7px 14px",
  borderRadius: 6,
  border: "none",
  background: "var(--color-danger)",
  color: "var(--color-bg-base)",
  fontSize: 13,
  fontWeight: 500,
  cursor: "pointer",
  display: "inline-flex",
  alignItems: "center",
  gap: 6,
};
const iconButtonStyle: React.CSSProperties = {
  background: "transparent",
  border: "none",
  color: "var(--color-text-muted)",
  cursor: "pointer",
  padding: 4,
  display: "inline-flex",
  alignItems: "center",
};
