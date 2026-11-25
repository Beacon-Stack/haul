// The piece bar's motion: how a piece falls, how long effects last, and
// how many pieces get the full treatment on a fast download. Pure, so the
// numbers can be tested without a canvas.

// How long a landing's glow, a quiet blink and the finish sweep last (ms).
export const RIPPLE_MS = 620;
export const SOFT_MS = 260;
export const FINISH_MS = 1100;

// Pieces a second that get the full drop. Past this the rest land with a
// quiet blink: every piece is still shown when and where it finished, but
// hundreds of drops a second would be a blur.
const SHOWCASE_PER_SECOND = 40;

// A snapshot that finishes more pieces than this at once is a recheck or
// a resume, not a download; they land without ceremony.
export const BULK_LANDING = 400;

// fallDuration is how long a piece takes to drop, in ms. Faster downloads
// drop faster, so a quick torrent looks quick.
export function fallDuration(piecesPerSecond: number): number {
  return Math.min(560, Math.max(240, 560 - piecesPerSecond * 3));
}

// fallOffset is how far a piece has fallen, 0 (start) to 1 (landed), for
// time t in 0..1. It accelerates, like something dropped.
export function fallOffset(t: number): number {
  const c = Math.min(1, Math.max(0, t));
  return c * c;
}

// approach moves a displayed value toward its target, covering most of the
// gap in about a sixth of a second whatever the frame rate.
export function approach(shown: number, target: number, dtSeconds: number): number {
  return shown + (target - shown) * Math.min(1, dtSeconds * 6);
}

// RateWindow counts events in the trailing second.
export class RateWindow {
  private times: number[] = [];

  add(now: number): void {
    this.times.push(now);
  }

  count(now: number): number {
    let drop = 0;
    while (drop < this.times.length && now - this.times[drop] >= 1000) drop++;
    if (drop > 0) this.times.splice(0, drop);
    return this.times.length;
  }
}

// ShowcaseBudget decides which arriving pieces get the full drop.
export class ShowcaseBudget {
  private readonly window = new RateWindow();

  constructor(private readonly perSecond: number = SHOWCASE_PER_SECOND) {}

  take(now: number): boolean {
    if (this.window.count(now) >= this.perSecond) return false;
    this.window.add(now);
    return true;
  }
}
