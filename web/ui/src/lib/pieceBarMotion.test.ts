import { describe, expect, it } from "vitest";
import { RateWindow, ShowcaseBudget, approach, fallDuration, fallOffset } from "./pieceBarMotion";

describe("fall", () => {
  it("drops faster on a faster download, within limits", () => {
    expect(fallDuration(0)).toBe(560);
    expect(fallDuration(50)).toBe(410);
    expect(fallDuration(10_000)).toBe(240);
  });

  it("starts at rest, ends landed, and accelerates", () => {
    expect(fallOffset(0)).toBe(0);
    expect(fallOffset(1)).toBe(1);
    expect(fallOffset(0.5)).toBeLessThan(0.5);
    expect(fallOffset(7)).toBe(1);
  });
});

describe("approach", () => {
  it("closes on the target without overshooting", () => {
    const next = approach(0, 1, 1 / 60);
    expect(next).toBeGreaterThan(0);
    expect(next).toBeLessThan(1);
    expect(approach(0, 1, 10)).toBe(1);
  });
});

describe("RateWindow", () => {
  it("counts only the trailing second", () => {
    const w = new RateWindow();
    w.add(0);
    w.add(500);
    expect(w.count(900)).toBe(2);
    expect(w.count(1000)).toBe(1);
    expect(w.count(1500)).toBe(0);
  });
});

describe("ShowcaseBudget", () => {
  it("allows a fixed number a second, then frees up as time passes", () => {
    const b = new ShowcaseBudget(3);
    expect([b.take(0), b.take(10), b.take(20), b.take(30)]).toEqual([true, true, true, false]);
    expect(b.take(1005)).toBe(true);
  });
});
