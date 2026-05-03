import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";

// ── Types ─────────────────────────────────────────────────────────────

export interface DiagnosticSummary {
  name: string;
  description: string;
  row_count: number;
}

export interface DiagnosticRow {
  id: string;
  summary: string;
  why_flagged: string;
  suggested_action: string;
}

export interface DiagnosticDetail {
  name: string;
  rows: DiagnosticRow[];
}

export interface CleanupResult {
  rows_deleted: number;
  // Populated only in soft mode — used to wire an Undo action on the
  // success toast that calls restore for each id.
  history_entry_ids?: number[];
}

export type CleanupMode = "soft" | "hard";

export interface CleanupHistoryEntry {
  id: number;
  diagnostic: string;
  source_table: string;
  source_pk: string;
  row_data: unknown;
  deleted_at: string;
  request_id: string;
  actor_key_prefix: string;
}

// ── Diagnostics ───────────────────────────────────────────────────────

export function useDiagnostics() {
  return useQuery({
    queryKey: ["admin", "diagnostics"],
    queryFn: () => apiFetch<DiagnosticSummary[]>("/admin/diagnostics"),
    refetchInterval: 30_000, // counts shift only when something happens
  });
}

export function useDiagnostic(name: string | null) {
  return useQuery({
    queryKey: ["admin", "diagnostics", name],
    queryFn: () => apiFetch<DiagnosticDetail>(`/admin/diagnostics/${name}`),
    enabled: !!name,
  });
}

export function useCleanupDiagnostic() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, body }: { name: string; body: { ids?: string[]; all?: boolean; mode: CleanupMode } }) =>
      apiFetch<CleanupResult>(`/admin/diagnostics/${name}/cleanup`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin", "diagnostics"] });
      qc.invalidateQueries({ queryKey: ["admin", "cleanup-history"] });
      qc.invalidateQueries({ queryKey: ["torrents"] });
    },
  });
}

// ── Cleanup history ───────────────────────────────────────────────────

export function useCleanupHistory(filter?: { diagnostic?: string; source_table?: string; limit?: number }) {
  const params = new URLSearchParams();
  if (filter?.diagnostic) params.set("diagnostic", filter.diagnostic);
  if (filter?.source_table) params.set("source_table", filter.source_table);
  if (filter?.limit) params.set("limit", String(filter.limit));
  const q = params.toString();
  return useQuery({
    queryKey: ["admin", "cleanup-history", filter ?? {}],
    queryFn: () => apiFetch<CleanupHistoryEntry[]>(`/admin/cleanup-history${q ? `?${q}` : ""}`),
    refetchInterval: 30_000,
  });
}

export function useRestoreCleanupHistory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) =>
      apiFetch<{ restored: boolean; reason?: string }>(`/admin/cleanup-history/${id}/restore`, { method: "POST" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin", "cleanup-history"] });
      qc.invalidateQueries({ queryKey: ["admin", "diagnostics"] });
      qc.invalidateQueries({ queryKey: ["torrents"] });
    },
  });
}

export function usePurgeCleanupHistory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (olderThanDays: number) =>
      apiFetch<{ rows_purged: number }>("/admin/cleanup-history/purge", {
        method: "POST",
        body: JSON.stringify({ older_than_days: olderThanDays }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "cleanup-history"] }),
  });
}
