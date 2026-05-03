import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";

export interface TorrentInfo {
  info_hash: string;
  name: string;
  status: string;
  save_path: string;
  category: string;
  tags: string[] | null;
  size: number;
  downloaded: number;
  uploaded: number;
  progress: number;
  download_rate: number;
  upload_rate: number;
  seeds: number;
  peers: number;
  seed_ratio: number;
  eta: number;
  added_at: string;
  completed_at?: string;
  content_path: string;
  sequential: boolean;
  // True when the backend's stall detector has classified this torrent as
  // inactive (no bytes for >= stall_timeout, default 120s, OR never observed
  // a peer past firstPeerTimeout). Always false unless status === "downloading".
  // The frontend renders this as a distinct "Stalled" state; never compute it
  // client-side.
  stalled: boolean;
}

export interface TorrentFile {
  index: number;
  path: string;
  size: number;
  priority: string;
  progress: number;
}

// ── Torrent detail types ─────────────────────────────────────────────────────
//
// These power the enhanced detail page — peer list, piece bar, tracker list.
// See plans/haul-torrent-detail-enhancements.md for the full design. The
// shapes here mirror internal/core/torrent/session.go one-for-one.

export interface PeerInfo {
  addr: string;          // "1.2.3.4:54321"
  client: string;        // "qBittorrent 4.5.0" or "unknown"
  network: string;       // "tcp" or "utp"
  encrypted: boolean;
  progress: number;      // 0..1
  download_rate: number; // bytes/sec inbound
  upload_rate: number;   // bytes/sec outbound (best-effort; 0 from backend in v1)
  downloaded: number;    // total bytes read from this peer
  uploaded: number;      // total bytes written to this peer
}

export type PieceRunState = "complete" | "partial" | "checking" | "missing";

export interface PieceStateRun {
  length: number;
  state: PieceRunState;
}

export interface PiecesInfo {
  num_pieces: number;
  piece_size: number;
  runs: PieceStateRun[];
}

export interface TrackerInfo {
  tier: number;
  url: string;
}

export function useTorrents() {
  return useQuery({
    queryKey: ["torrents"],
    queryFn: () => apiFetch<TorrentInfo[]>("/torrents"),
    refetchInterval: 3000,
  });
}

// useTorrent supports a faster refetch interval for the detail page.
// Pass { detailPage: true } on the detail view to get 1s polling;
// everywhere else gets the default 2s.
export function useTorrent(hash: string, opts?: { detailPage?: boolean }) {
  return useQuery({
    queryKey: ["torrents", hash],
    queryFn: () => apiFetch<TorrentInfo>(`/torrents/${hash}`),
    enabled: !!hash,
    refetchInterval: opts?.detailPage ? 1000 : 2000,
  });
}

export function useTorrentFiles(hash: string) {
  return useQuery({
    queryKey: ["torrents", hash, "files"],
    queryFn: () => apiFetch<TorrentFile[]>(`/torrents/${hash}/files`),
    enabled: !!hash,
  });
}

// ── Detail-page hooks ────────────────────────────────────────────────────────
// All three poll at the detail-page cadence (1s) except trackers, which is
// static data from the metainfo — refetch rarely.

export function useTorrentPeers(hash: string) {
  return useQuery({
    queryKey: ["torrents", hash, "peers"],
    queryFn: () => apiFetch<{ peers: PeerInfo[] }>(`/torrents/${hash}/peers`),
    enabled: !!hash,
    refetchInterval: 1000,
  });
}

export function useTorrentPieces(hash: string) {
  return useQuery({
    queryKey: ["torrents", hash, "pieces"],
    queryFn: () => apiFetch<PiecesInfo>(`/torrents/${hash}/pieces`),
    enabled: !!hash,
    refetchInterval: 1000,
  });
}

export function useTorrentTrackers(hash: string) {
  return useQuery({
    queryKey: ["torrents", hash, "trackers"],
    queryFn: () => apiFetch<{ trackers: TrackerInfo[] }>(`/torrents/${hash}/trackers`),
    enabled: !!hash,
    refetchInterval: 30000, // static data, don't hammer
  });
}

export function useAddTrackers(hash: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (urls: string[]) =>
      apiFetch<unknown>(`/torrents/${hash}/trackers`, {
        method: "POST",
        body: JSON.stringify({ urls }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents", hash, "trackers"] }),
  });
}

export function useRemoveTracker(hash: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (url: string) =>
      apiFetch<unknown>(`/torrents/${hash}/trackers?url=${encodeURIComponent(url)}`, {
        method: "DELETE",
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents", hash, "trackers"] }),
  });
}

export function useAddTorrent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { uri: string; category?: string; save_path?: string; paused?: boolean }) =>
      apiFetch<TorrentInfo>("/torrents", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents"] }),
  });
}

export function useDeleteTorrent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ hash, deleteFiles }: { hash: string; deleteFiles: boolean }) =>
      apiFetch(`/torrents/${hash}?delete_files=${deleteFiles}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents"] }),
  });
}

export function usePauseTorrent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (hash: string) =>
      apiFetch(`/torrents/${hash}/pause`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents"] }),
  });
}

export function useResumeTorrent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (hash: string) =>
      apiFetch(`/torrents/${hash}/resume`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents"] }),
  });
}

export function useReorderTorrents() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (order: string[]) =>
      apiFetch("/torrents/reorder", {
        method: "PUT",
        body: JSON.stringify({ order }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents"] }),
  });
}

// useSetTorrentPriority sends the new rank for a single torrent.
// The backend re-runs the queue gate after this call; the next poll
// will reflect updated statuses for any torrents that crossed the cap.
export function useSetTorrentPriority() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ hash, priority }: { hash: string; priority: number }) =>
      apiFetch(`/torrents/${hash}/priority`, {
        method: "PUT",
        body: JSON.stringify({ priority }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["torrents"] }),
  });
}
