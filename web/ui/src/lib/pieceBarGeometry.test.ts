import { describe, expect, it } from "vitest";
import {
  CHECKING, COMPLETE, MISSING, PARTIAL,
  cellCount, cellOfPiece, cellPieceRange, expandRuns,
} from "./pieceBarGeometry";

describe("expandRuns", () => {
  it("gives one state per piece", () => {
    const got = expandRuns(
      [
        { length: 2, state: "complete" },
        { length: 1, state: "partial" },
        { length: 1, state: "checking" },
        { length: 2, state: "missing" },
      ],
      6,
    );
    expect(Array.from(got)).toEqual([COMPLETE, COMPLETE, PARTIAL, CHECKING, MISSING, MISSING]);
  });

  it("ignores runs past the end and leaves uncovered pieces missing", () => {
    expect(Array.from(expandRuns([{ length: 9, state: "complete" }], 3))).toEqual([COMPLETE, COMPLETE, COMPLETE]);
    expect(Array.from(expandRuns([{ length: 1, state: "complete" }], 3))).toEqual([COMPLETE, MISSING, MISSING]);
  });
});

describe("cells", () => {
  it("gives each piece its own cell when there is room", () => {
    expect(cellCount(100, 800)).toBe(100);
    expect(cellOfPiece(37, 100, 100)).toBe(37);
  });

  it("uses one cell per pixel when pieces outnumber pixels", () => {
    expect(cellCount(5000, 800)).toBe(800);
    expect(cellOfPiece(0, 5000, 800)).toBe(0);
    expect(cellOfPiece(4999, 5000, 800)).toBe(799);
  });

  // The range and the mapping have to agree, or a piece is counted in one
  // cell and drawn in another.
  it.each([[10, 10], [1000, 300], [777, 250], [5000, 800], [3, 800]])(
    "cellPieceRange matches cellOfPiece for %i pieces in %i px",
    (numPieces, width) => {
      const cells = cellCount(numPieces, width);
      let covered = 0;
      for (let c = 0; c < cells; c++) {
        const [start, end] = cellPieceRange(c, numPieces, cells);
        for (let i = start; i < end; i++) expect(cellOfPiece(i, numPieces, cells)).toBe(c);
        covered += end - start;
      }
      expect(covered).toBe(numPieces);
    },
  );
});
