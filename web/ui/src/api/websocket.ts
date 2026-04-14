import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";

function buildWsUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/api/v1/ws`;
}

export function useWebSocket() {
  const qc = useQueryClient();
  const retryDelay = useRef(1000);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let stopped = false;

    function connect() {
      if (stopped) return;
      const ws = new WebSocket(buildWsUrl());

      ws.onopen = () => {
        retryDelay.current = 1000;
      };

      ws.onmessage = (ev) => {
        try {
          const event = JSON.parse(ev.data);
          if (event.type?.startsWith("torrent_") || event.type === "speed_update") {
            qc.invalidateQueries({ queryKey: ["torrents"] });
            qc.invalidateQueries({ queryKey: ["stats"] });
          }
        } catch {}
      };

      ws.onclose = () => {
        if (stopped) return;
        const delay = retryDelay.current;
        retryDelay.current = Math.min(delay * 2, 30_000);
        timerRef.current = setTimeout(connect, delay);
      };

      ws.onerror = () => ws.close();
    }

    connect();
    return () => {
      stopped = true;
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    };
  }, [qc]);
}
