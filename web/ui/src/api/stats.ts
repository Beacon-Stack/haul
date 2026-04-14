import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";

export interface AppStats {
  version: string;
  total_torrents: number;
}

export function useStats() {
  return useQuery({
    queryKey: ["stats"],
    queryFn: () => apiFetch<AppStats>("/stats"),
    refetchInterval: 5000,
  });
}
