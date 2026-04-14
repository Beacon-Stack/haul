import { useEffect, useMemo, useRef, useState } from "react";
import type { PiecesInfo, TorrentFile } from "@/api/torrents";
import {
  runsToColumns,
  computeFileSegments,
  findSegmentAt,
  diffNewCompletions,
  pieceIndexToX,
  type BarColumn,
  type FileSegment,
} from "@/lib/pieceBarGeometry";

// PieceBar — canvas-rendered torrent piece visualization with per-piece
// arrival animation, file hover, and auto-stopping rAF loop.
//
// Design doc: plans/haul-torrent-detail-enhancements.md §4
//
// Key properties:
//  - Single pixel column per piece (adaptive via runsToColumns when more
//    pieces than pixels). Spatial position is preserved — a piece from the
//    middle of the file lights up in the middle of the bar.
//  - Three layers per draw: base completion bar → partial-piece shimmer
//    (sine-wave alpha) → one-shot arrival flashes (fade over 400ms).
//  - File hover: DOM tooltip positioned from pointer, not a canvas-drawn
//    overlay, so the text gets native CSS styling and antialiasing.
//  - rAF loop stops itself whenever nothing is animating (idle CPU = 0%).
//  - prefers-reduced-motion kills the shimmer and flashes entirely.
//  - Intensity control: if >50 pieces completed in a single poll, skip
//    flash generation — at that rate the bar's colour update alone conveys
//    rapid progress and individual flashes would look busy.

interface PieceBarProps {
  pieces: PiecesInfo;
  files: TorrentFile[];
  progress: number; // 0..1 — used to auto-collapse section when done
}

const BAR_HEIGHT = 22;
const FLASH_DURATION_MS = 400;
const FLASH_INTENSITY_CUTOFF = 50;

interface ArrivalFlash {
  x: number;
  start: number;
}

// ThemeColors are read once at mount via getComputedStyle — canvas needs
// concrete hex, not CSS vars. See plan §4.7.
interface ThemeColors {
  downloading: string;
  warning: string;
  bgSubtle: string;
  divider: string;
  flash: string;
}

function useThemeColors(): ThemeColors {
  return useMemo(() => {
    const s = getComputedStyle(document.documentElement);
    const get = (name: string, fallback: string) => {
      const v = s.getPropertyValue(name).trim();
      return v || fallback;
    };
    const downloading = get("--color-status-downloading", "#3b9eff");
    return {
      downloading,
      warning: get("--color-warning", "#fbbf24"),
      bgSubtle: get("--color-bg-subtle", "#2a2a35"),
      divider: get("--color-border-subtle", "#3a3a4a"),
      // Flash is a brighter cousin of the downloading colour — we bias
      // toward near-white so it pops against the base bar.
      flash: "#d0e8ff",
    };
  }, []);
}

export default function PieceBar({ pieces, files, progress }: PieceBarProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const colors = useThemeColors();

  // Observe canvas parent width so the bar adapts to layout changes.
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

  // Track previous runs so we can diff newly-complete pieces into arrival
  // flashes. The ref is intentionally kept outside React state — we don't
  // want to trigger re-renders just to update the diff source.
  const prevRunsRef = useRef<PiecesInfo["runs"] | undefined>(undefined);
  const flashesRef = useRef<ArrivalFlash[]>([]);

  // Columns and file segments are derived from the incoming pieces/files
  // snapshot and the observed canvas width. Recomputed every poll + every
  // resize, which is cheap (O(numPieces + width)).
  const columns = useMemo<BarColumn[]>(() => {
    return runsToColumns(pieces.runs, pieces.num_pieces, width);
  }, [pieces.runs, pieces.num_pieces, width]);

  const segments = useMemo<FileSegment[]>(() => {
    return computeFileSegments(files, width);
  }, [files, width]);

  // Diff previous-vs-current runs whenever a new snapshot arrives. This
  // generates the arrival-flash queue the rAF loop will draw.
  useEffect(() => {
    const newly = diffNewCompletions(prevRunsRef.current, pieces.runs, pieces.num_pieces);
    prevRunsRef.current = pieces.runs;

    if (newly.length === 0 || newly.length > FLASH_INTENSITY_CUTOFF) return;

    const now = performance.now();
    const newFlashes = newly.map((idx) => ({
      x: pieceIndexToX(idx, pieces.num_pieces, width),
      start: now,
    }));
    flashesRef.current = [...flashesRef.current, ...newFlashes];
  }, [pieces.runs, pieces.num_pieces, width]);

  // Main rAF render loop. Runs whenever there's something to animate —
  // shimmer (hasPartial in any column) or unresolved flashes. Stops itself
  // when idle so completed torrents don't waste CPU.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(BAR_HEIGHT * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${BAR_HEIGHT}px`;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    let rafId: number | null = null;
    let running = true;

    const hasPartial = columns.some((c) => c.hasPartial);

    const draw = (now: number) => {
      if (!running) return;

      // Background wash.
      ctx.globalAlpha = 1;
      ctx.fillStyle = colors.bgSubtle;
      ctx.fillRect(0, 0, width, BAR_HEIGHT);

      // Base completion layer. Missing columns are already painted by the
      // wash; we draw only where we have something to show.
      for (let x = 0; x < columns.length; x++) {
        const col = columns[x];
        if (col.hasChecking) {
          ctx.globalAlpha = 1;
          ctx.fillStyle = colors.warning;
          ctx.fillRect(x, 0, 1, BAR_HEIGHT);
          continue;
        }
        if (col.fracComplete <= 0) continue;
        ctx.globalAlpha = col.fracComplete;
        ctx.fillStyle = colors.downloading;
        ctx.fillRect(x, 0, 1, BAR_HEIGHT);
      }
      ctx.globalAlpha = 1;

      // Partial-piece shimmer — low-intensity sine wave pulsing by clock
      // time. Skipped entirely under prefers-reduced-motion.
      if (!reducedMotion && hasPartial) {
        const shimmer = 0.45 + 0.25 * Math.sin((now / 600) * Math.PI);
        ctx.globalAlpha = shimmer;
        ctx.fillStyle = colors.downloading;
        for (let x = 0; x < columns.length; x++) {
          if (columns[x].hasPartial) ctx.fillRect(x, 0, 1, BAR_HEIGHT);
        }
        ctx.globalAlpha = 1;
      }

      // Arrival flashes — fade out over FLASH_DURATION_MS, then drop from
      // the queue. Skipped entirely under prefers-reduced-motion.
      if (!reducedMotion) {
        const stillActive: ArrivalFlash[] = [];
        for (const f of flashesRef.current) {
          const age = now - f.start;
          if (age >= FLASH_DURATION_MS) continue;
          stillActive.push(f);
          const t = age / FLASH_DURATION_MS;
          ctx.globalAlpha = 1 - t;
          ctx.fillStyle = colors.flash;
          // 2px wide so the flash is visible even at sub-pixel positions.
          ctx.fillRect(Math.max(0, f.x - 0.5), 0, 2, BAR_HEIGHT);
        }
        flashesRef.current = stillActive;
        ctx.globalAlpha = 1;
      } else {
        // Reduced motion: drop the queue so we don't accumulate.
        flashesRef.current = [];
      }

      // File boundary dividers (multi-file only).
      if (segments.length > 1) {
        ctx.fillStyle = colors.divider;
        ctx.globalAlpha = 0.7;
        for (let i = 1; i < segments.length; i++) {
          ctx.fillRect(segments[i].xStart, 0, 1, BAR_HEIGHT);
        }
        ctx.globalAlpha = 1;
      }

      const needsAnotherFrame =
        !reducedMotion && (hasPartial || flashesRef.current.length > 0);
      if (needsAnotherFrame) {
        rafId = requestAnimationFrame(draw);
      } else {
        rafId = null;
      }
    };

    rafId = requestAnimationFrame(draw);

    return () => {
      running = false;
      if (rafId !== null) cancelAnimationFrame(rafId);
    };
  }, [columns, segments, width, colors]);

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
  const waiting = pieces.num_pieces === 0;

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
              height: BAR_HEIGHT,
              borderRadius: 3,
              cursor: segments.length > 0 ? "crosshair" : "default",
            }}
          />
        )}
        {waiting && (
          <div
            style={{
              height: BAR_HEIGHT,
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
              top: BAR_HEIGHT + 6,
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

function formatBytes(b: number): string {
  if (b <= 0) return "0 B";
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`;
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
