// Pure functions that back the PieceBar canvas widget: where pieces and
// files sit on the bar. DOM-free, so they can be unit-tested in isolation.

import type { PieceStateRun, TorrentFile } from "@/api/torrents";

export interface FileSegment {
  xStart: number; // inclusive pixel x
  xEnd: number;   // exclusive pixel x
  name: string;
  size: number;   // bytes
}

// Piece states as the bar stores them, one byte per piece.
export const MISSING = 0;
export const PARTIAL = 1;
export const COMPLETE = 2;
export const CHECKING = 3;

// expandRuns turns the run-length-encoded piece states into one state per
// piece. Pieces the runs don't cover stay MISSING.
export function expandRuns(runs: PieceStateRun[], numPieces: number): Uint8Array {
  const out = new Uint8Array(Math.max(0, numPieces));
  let idx = 0;
  for (const run of runs) {
    const state =
      run.state === "complete" ? COMPLETE
      : run.state === "partial" ? PARTIAL
      : run.state === "checking" ? CHECKING
      : MISSING;
    const end = Math.min(idx + run.length, out.length);
    if (state !== MISSING) out.fill(state, idx, end);
    idx += run.length;
  }
  return out;
}

// The bar is divided into cells. With fewer pieces than pixels each piece
// gets its own cell, wider than a pixel; with more, a cell is one pixel
// and holds several pieces. Position is preserved either way: a piece
// from the middle of the torrent sits in the middle of the bar.
export function cellCount(numPieces: number, width: number): number {
  return Math.max(0, Math.min(numPieces, Math.floor(width)));
}

export function cellOfPiece(index: number, numPieces: number, cells: number): number {
  if (numPieces <= 0 || cells <= 0) return 0;
  // Multiply before dividing: whole numbers stay exact, so this always
  // agrees with cellPieceRange.
  return Math.min(cells - 1, Math.floor((index * cells) / numPieces));
}

// cellPieceRange returns the pieces [start, end) that cellOfPiece maps to
// the given cell.
export function cellPieceRange(cell: number, numPieces: number, cells: number): [number, number] {
  if (numPieces <= 0 || cells <= 0) return [0, 0];
  const start = Math.ceil((cell * numPieces) / cells);
  const end = cell >= cells - 1 ? numPieces : Math.ceil(((cell + 1) * numPieces) / cells);
  return [start, end];
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
