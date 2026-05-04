import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";

export interface ActivityItem {
  info_hash: string;
  name: string;
  category: string;
  save_path: string;
  size_bytes: number;
  resolution: string;
  added_at: string;
  completed_at?: string;
  removed_at?: string;
  requester?: string;
  movie_id?: string;
  series_id?: string;
  episode_id?: string;
  tmdb_id?: number;
  season?: number;
  episode?: number;
}

export interface ActivityListResponse {
  items: ActivityItem[];
  total: number;
}

export type ActivitySort =
  | "added_at"
  | "completed_at"
  | "removed_at"
  | "size_bytes"
  | "resolution"
  | "name";

export type ActivityStatus = "all" | "active" | "completed" | "removed";

export interface ActivityListParams {
  q?: string;
  status?: ActivityStatus;
  sort?: ActivitySort;
  order?: "asc" | "desc";
  limit?: number;
  offset?: number;
}

function buildQuery(params: ActivityListParams): string {
  const qs = new URLSearchParams();
  if (params.q) qs.set("q", params.q);
  if (params.status && params.status !== "all") qs.set("status", params.status);
  if (params.sort) qs.set("sort", params.sort);
  if (params.order) qs.set("order", params.order);
  if (params.limit !== undefined) qs.set("limit", String(params.limit));
  if (params.offset !== undefined) qs.set("offset", String(params.offset));
  const s = qs.toString();
  return s ? `?${s}` : "";
}

export function useActivityList(params: ActivityListParams) {
  return useQuery({
    queryKey: ["activity", params],
    queryFn: () => apiFetch<ActivityListResponse>(`/activity${buildQuery(params)}`),
    // Keep previous data on the screen while a search keystroke fires
    // a new request — avoids a flicker to "0 results" mid-debounce.
    placeholderData: (prev) => prev,
    staleTime: 2000,
  });
}

export interface ActivityEvent {
  id: number;
  info_hash: string;
  event_type: string;
  occurred_at: string;
  payload?: Record<string, unknown> | string;
}

export interface HistoryRecord {
  info_hash: string;
  name: string;
  save_path: string;
  category: string;
  added_at: string;
  completed_at?: string;
  removed_at?: string;
  requester?: string;
  movie_id?: string;
  series_id?: string;
  episode_id?: string;
  tmdb_id?: number;
  season?: number;
  episode?: number;
}

// useHistoryByHash fetches the durable history record for one
// torrent. Used on the detail page so requester metadata stays
// visible even after the live torrent has been removed (the live
// /torrents/{hash} endpoint 404s once removed_at is set; this one
// keeps returning the snapshot).
export function useHistoryByHash(hash: string) {
  return useQuery({
    queryKey: ["history", hash],
    queryFn: () => apiFetch<HistoryRecord>(`/history/by-hash/${hash}`),
    enabled: !!hash,
    // 404 is expected when the torrent has been hard-deleted from
    // the cleanup-history admin tab. Don't treat that as an error
    // banner — the detail page renders gracefully without it.
    throwOnError: false,
    retry: false,
  });
}

// usePeerServices returns the {service_name → api_url} map Pulse
// reports. Used to build deep-links from Haul's Activity page back
// to Pilot/Prism. Cached for 60s — services rarely change URL and
// re-querying every render would hammer Pulse on every page nav.
export function usePeerServices() {
  return useQuery({
    queryKey: ["peers"],
    queryFn: () => apiFetch<{ services: Record<string, string> }>("/peers").then((r) => r.services),
    staleTime: 60_000,
    // Pulse may be unreachable (Haul running standalone). Don't surface
    // that as a banner — the UI degrades to non-clickable badges.
    throwOnError: false,
    retry: false,
  });
}

export function useActivityEvents(hash: string, limit = 200) {
  return useQuery({
    queryKey: ["activity", hash, "events", limit],
    queryFn: () =>
      apiFetch<{ events: ActivityEvent[] }>(
        `/activity/${hash}/events?limit=${limit}`,
      ).then((r) => r.events),
    enabled: !!hash,
    refetchInterval: 5000,
  });
}
