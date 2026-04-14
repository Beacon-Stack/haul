import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";

export interface HealthReport {
  active_downloads: number;
  active_uploads: number;
  total_torrents: number;
  download_speed_bps: number;
  upload_speed_bps: number;
  disk_free_bytes: number;
  disk_total_bytes: number;
  stalled_count: number;
  engine_status: string;
  peers_connected: number;
  vpn_active: boolean;
  vpn_interface?: string;
  external_ip?: string;
}

export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: () => apiFetch<HealthReport>("/health"),
    refetchInterval: 5000,
  });
}
