import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "./client";

export interface SettingsMap {
  [key: string]: string;
}

export function useSettings() {
  return useQuery({
    queryKey: ["settings"],
    queryFn: async () => {
      const resp = await apiFetch<{ settings: SettingsMap }>("/settings");
      return resp.settings;
    },
  });
}

export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (settings: SettingsMap) =>
      apiFetch<{ settings: SettingsMap }>("/settings", {
        method: "PUT",
        body: JSON.stringify({ settings }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
  });
}
