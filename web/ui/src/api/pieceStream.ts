import { useEffect, useRef } from "react";

// One batch from the server's live piece stream.
export interface PieceUpdate {
  done?: number[];                        // pieces that just finished, in order
  progress?: { i: number; f: number }[];  // in-progress pieces: index and fraction downloaded
}

interface Handlers {
  onUpdate: (u: PieceUpdate) => void;
  onLive: (live: boolean) => void;
}

// usePieceStream keeps a WebSocket open to a torrent's live piece stream
// while enabled, reconnecting if it drops. onLive reports whether the
// stream is connected, so the bar can fall back to its once-a-second
// snapshots when it isn't.
export function usePieceStream(hash: string, enabled: boolean, handlers: Handlers): void {
  const ref = useRef(handlers);
  ref.current = handlers;

  useEffect(() => {
    if (!enabled || !hash) return;
    let stopped = false;
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let delay = 1000;

    function connect() {
      if (stopped) return;
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${window.location.host}/api/v1/torrents/${hash}/pieces/live`);
      ws.onopen = () => {
        delay = 1000;
        ref.current.onLive(true);
      };
      ws.onmessage = (ev) => {
        try {
          ref.current.onUpdate(JSON.parse(ev.data) as PieceUpdate);
        } catch {
          // Not a piece update; nothing to draw.
        }
      };
      ws.onclose = () => {
        ref.current.onLive(false);
        if (stopped) return;
        timer = setTimeout(connect, delay);
        delay = Math.min(delay * 2, 15_000);
      };
      ws.onerror = () => ws?.close();
    }

    connect();
    return () => {
      stopped = true;
      if (timer !== null) clearTimeout(timer);
      ws?.close();
      ref.current.onLive(false);
    };
  }, [hash, enabled]);
}
