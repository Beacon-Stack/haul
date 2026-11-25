import { useEffect, useMemo, useRef, useState } from "react";
import type { PiecesInfo, TorrentFile } from "@/api/torrents";
import { usePieceStream } from "@/api/pieceStream";
import { formatBytes } from "@/lib/format";
import { usePieceAnimation } from "@/lib/displayPrefs";
import { computeFileSegments, findSegmentAt, type FileSegment } from "@/lib/pieceBarGeometry";
import { PieceBarEngine, type BarColors } from "./pieceBarEngine";

// PieceBar shows a torrent being assembled: one position per piece, each
// piece dropping into place the moment it finishes. The drawing and
// motion live in PieceBarEngine; this component feeds it the piece
// snapshots, the live stream, the bar's width and the theme, and handles
// the file hover.
//
// The animation can be turned off in Settings, and is off for anyone
// whose system asks for reduced motion. Either way the bar is then a
// plain strip that fills in.

interface PieceBarProps {
  hash: string;
  pieces: PiecesInfo;
  files: TorrentFile[];
  progress: number; // 0..1
}

// Canvas needs concrete colours, not CSS variables, so they are read from
// the theme once at mount.
function useThemeColors(): BarColors {
  return useMemo(() => {
    const s = getComputedStyle(document.documentElement);
    const get = (name: string, fallback: string) => s.getPropertyValue(name).trim() || fallback;
    return {
      downloading: get("--color-status-downloading", "#3b9eff"),
      success: get("--color-success", "#34d399"),
      warning: get("--color-warning", "#fbbf24"),
      well: get("--color-bg-subtle", "#2a2a35"),
      divider: get("--color-border-subtle", "#3a3a4a"),
    };
  }, []);
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export default function PieceBar({ hash, pieces, files, progress }: PieceBarProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const engineRef = useRef<PieceBarEngine | null>(null);
  const colors = useThemeColors();
  const animate = usePieceAnimation() && !prefersReducedMotion();
  const waiting = pieces.num_pieces === 0;

  // Follow the wrapper's width so the bar adapts to layout changes.
  const [width, setWidth] = useState(800);
  const wrapperRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const w = Math.floor(e.contentRect.width);
        if (w > 0) setWidth(w);
      }
    });
    ro.observe(wrapper);
    return () => ro.disconnect();
  }, []);

  const segments = useMemo<FileSegment[]>(() => computeFileSegments(files, width), [files, width]);

  // The canvas exists only once the torrent has metadata. These effects
  // run in order, so the engine is sized before it gets its first pieces.
  useEffect(() => {
    if (waiting || !canvasRef.current) return;
    const engine = new PieceBarEngine(canvasRef.current, colors);
    engineRef.current = engine;
    return () => {
      engine.dispose();
      engineRef.current = null;
    };
  }, [waiting, colors]);

  useEffect(() => {
    engineRef.current?.resize(width, animate);
  }, [waiting, colors, width, animate]);

  useEffect(() => {
    engineRef.current?.setSegments(segments);
  }, [waiting, colors, segments]);

  useEffect(() => {
    engineRef.current?.applySnapshot(pieces.runs, pieces.num_pieces);
  }, [waiting, colors, width, animate, pieces.runs, pieces.num_pieces]);

  // Pieces arrive live while the torrent is downloading and the bar is
  // animating. Otherwise the once-a-second snapshot is enough.
  usePieceStream(hash, animate && !waiting && progress < 1, {
    onUpdate: (u) => engineRef.current?.onStream(u),
    onLive: (live) => engineRef.current?.setStreamLive(live),
  });

  const barHeight = animate ? 70 : 22;

  // Tooltip state. Positioned via absolute CSS over the canvas parent.
  const [tooltip, setTooltip] = useState<{
    x: number;
    segment: FileSegment;
  } | null>(null);

  function handleMouseMove(e: React.MouseEvent<HTMLCanvasElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = Math.floor(e.clientX - rect.left);
    const segment = findSegmentAt(segments, x);
    if (segment) {
      setTooltip({ x, segment });
    } else {
      setTooltip(null);
    }
  }

  function handleMouseLeave() {
    setTooltip(null);
  }

  const completePct = Math.round(progress * 100);

  return (
    <div>
      <div
        style={{
          fontSize: 11,
          color: "var(--color-text-muted)",
          marginBottom: 6,
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <span>
          {waiting
            ? "Waiting for metadata…"
            : `${pieces.num_pieces.toLocaleString()} pieces · ${formatBytes(pieces.piece_size)} each · ${completePct}%`}
        </span>
      </div>

      <div
        ref={wrapperRef}
        style={{ position: "relative", width: "100%", minWidth: 0 }}
      >
        {!waiting && (
          <canvas
            ref={canvasRef}
            onMouseMove={handleMouseMove}
            onMouseLeave={handleMouseLeave}
            style={{
              display: "block",
              width: "100%",
              height: barHeight,
              cursor: segments.length > 0 ? "crosshair" : "default",
            }}
          />
        )}
        {waiting && (
          <div
            style={{
              height: barHeight,
              background: "var(--color-bg-subtle)",
              borderRadius: 3,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontSize: 10,
              color: "var(--color-text-muted)",
              fontStyle: "italic",
            }}
          >
            waiting for metadata…
          </div>
        )}
        {tooltip && (
          <div
            style={{
              position: "absolute",
              left: Math.min(Math.max(tooltip.x - 80, 0), width - 160),
              top: barHeight + 6,
              padding: "6px 10px",
              background: "var(--color-bg-elevated)",
              border: "1px solid var(--color-border-default)",
              borderRadius: 4,
              boxShadow: "var(--shadow-card)",
              pointerEvents: "none",
              zIndex: 2,
              minWidth: 160,
              maxWidth: 320,
            }}
          >
            <div
              style={{
                fontSize: 11,
                fontFamily: "var(--font-family-mono)",
                color: "var(--color-text-primary)",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
              title={tooltip.segment.name}
            >
              {tooltip.segment.name}
            </div>
            <div style={{ fontSize: 10, color: "var(--color-text-muted)", marginTop: 2 }}>
              {formatBytes(tooltip.segment.size)}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

