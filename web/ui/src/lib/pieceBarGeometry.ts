// Pure functions that back the PieceBar canvas widget. Everything here is
// DOM-free so the canvas component stays focused on drawing, not math.
//
// These functions are unit-testable in isolation; Haul doesn't currently
// have a frontend test runner wired up, so verification happens via smoke
// test against a real torrent. If a regression is ever suspected, install
// vitest in haul/web/ui and drop in a test file alongside this one.
//
// See plans/haul-torrent-detail-enhancements.md §4 for the full design.

import type { PieceStateRun, TorrentFile } from "@/api/torrents";

export interface BarColumn {
  fracComplete: number; // 0..1 — weighted completion of pieces mapped to this column
  hasPartial: boolean;  // any piece in this column is currently being downloaded
  hasChecking: boolean; // any piece in this column is being verified/hashed
}

export interface FileSegment {
  xStart: number; // inclusive pixel x
  xEnd: number;   // exclusive pixel x
  name: string;
  size: number;   // bytes
}

// runsToColumns walks the run-length-encoded piece state and builds a
// fixed-width array of per-pixel columns. Each column aggregates the state
// of the pieces whose index maps to that x-coordinate via
//   x = floor((pieceIndex / numPieces) * width)
//
// Complexity: O(numPieces + width). For a 10k-piece torrent at 800px this
// is ~20μs — negligible even at 1 Hz poll + 60 Hz redraw.
export function runsToColumns(
  runs: PieceStateRun[],
  numPieces: number,
  width: number,
): BarColumn[] {
  const cols: BarColumn[] = Array.from({ length: width }, () => ({
    fracComplete: 0,
    hasPartial: false,
    hasChecking: false,
  }));
  if (numPieces <= 0 || width <= 0) return cols;

  // Use typed arrays for the count accumulators — avoids GC pressure at
  // high poll rates with large torrents.
  const totals = new Int32Array(width);
  const completes = new Int32Array(width);

  let globalIdx = 0;
  for (const run of runs) {
    for (let i = 0; i < run.length; i++) {
      const x = Math.min(Math.floor((globalIdx / numPieces) * width), width - 1);
      totals[x]++;
      if (run.state === "complete") completes[x]++;
      else if (run.state === "partial") cols[x].hasPartial = true;
      else if (run.state === "checking") cols[x].hasChecking = true;
      globalIdx++;
    }
  }
  for (let x = 0; x < width; x++) {
    cols[x].fracComplete = totals[x] === 0 ? 0 : completes[x] / totals[x];
  }
  return cols;
}

// computeFileSegments maps each file in the torrent to an inclusive/exclusive
// pixel x-range proportional to its size. Used by the hover tooltip to
// translate a mouse x-position back to a filename.
//
// We deliberately use byte-proportional segments instead of piece-aligned
// ones: piece boundaries can split a file, and aligning to them introduces
// off-by-one confusion around file edges. Byte-proportional is exact
// relative to the file-size math the user can reason about.
export function computeFileSegments(
  files: TorrentFile[],
  width: number,
): FileSegment[] {
  if (files.length === 0 || width <= 0) return [];

  const totalSize = files.reduce((sum, f) => sum + f.size, 0);
  if (totalSize <= 0) return [];

  const segments: FileSegment[] = [];
  let runningBytes = 0;
  for (const f of files) {
    const xStart = Math.round((runningBytes / totalSize) * width);
    runningBytes += f.size;
    const xEnd = Math.round((runningBytes / totalSize) * width);
    segments.push({
      xStart,
      xEnd: Math.max(xEnd, xStart + 1), // guarantee at least 1px width per file
      name: f.path,
      size: f.size,
    });
  }
  // Clamp the last segment's xEnd to `width` — Math.round can produce
  // width+1 for cumulative sums at the tail.
  if (segments.length > 0) {
    segments[segments.length - 1].xEnd = width;
  }
  return segments;
}

// findSegmentAt returns the file segment that contains the given x-pixel.
// Linear scan because the segment list is typically <20 entries — a binary
// search would be micro-optimisation theatre.
export function findSegmentAt(segments: FileSegment[], x: number): FileSegment | undefined {
  for (const s of segments) {
    if (x >= s.xStart && x < s.xEnd) return s;
  }
  return undefined;
}

// diffNewCompletions walks previous and current RLE runs and returns the
// indices of pieces that transitioned from not-complete to complete.
// Used to generate the per-piece arrival flash animations — see §4.5 in
// the plan for why this is the "assembling from parts" signal.
//
// The intensity-control cutoff (§D5) is applied by the caller, not here —
// this function always returns the full diff.
export function diffNewCompletions(
  prev: PieceStateRun[] | undefined,
  curr: PieceStateRun[],
  numPieces: number,
): number[] {
  if (!prev || numPieces <= 0) return [];

  const prevComplete = runsToCompleteSet(prev, numPieces);
  const newlyComplete: number[] = [];
  let idx = 0;
  for (const run of curr) {
    if (run.state === "complete") {
      for (let i = 0; i < run.length; i++) {
        if (!prevComplete.has(idx + i)) newlyComplete.push(idx + i);
      }
    }
    idx += run.length;
  }
  return newlyComplete;
}

// runsToCompleteSet expands a run-length run list into a set of piece indices
// that are in the 'complete' state. Internal helper for diffNewCompletions.
function runsToCompleteSet(runs: PieceStateRun[], numPieces: number): Set<number> {
  const set = new Set<number>();
  let idx = 0;
  for (const run of runs) {
    if (run.state === "complete") {
      for (let i = 0; i < run.length; i++) {
        if (idx + i < numPieces) set.add(idx + i);
      }
    }
    idx += run.length;
  }
  return set;
}

// pieceIndexToX maps a piece index to its pixel x-coordinate using the same
// formula as runsToColumns. Used by the arrival-flash renderer to place
// individual pieces on the canvas.
export function pieceIndexToX(index: number, numPieces: number, width: number): number {
  if (numPieces <= 0) return 0;
  return Math.min(Math.floor((index / numPieces) * width), width - 1);
}
