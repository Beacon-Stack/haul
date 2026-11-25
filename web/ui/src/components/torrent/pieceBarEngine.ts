import type { PieceStateRun } from "@/api/torrents";
import type { PieceUpdate } from "@/api/pieceStream";
import {
  CHECKING, COMPLETE, MISSING, PARTIAL,
  cellCount, cellOfPiece, cellPieceRange, expandRuns,
  type FileSegment,
} from "@/lib/pieceBarGeometry";
import {
  BULK_LANDING, FINISH_MS, RIPPLE_MS, SOFT_MS,
  RateWindow, ShowcaseBudget, approach, fallDuration, fallOffset,
} from "@/lib/pieceBarMotion";

// PieceBarEngine draws the piece bar on a canvas and animates pieces as
// they arrive. It is told about pieces two ways:
//
//  - the live stream, which names each piece the moment it finishes and
//    reports how full the in-progress ones are;
//  - a snapshot of every piece's state about once a second, which is the
//    truth the bar is reconciled against and the only source when the
//    stream is down.
//
// Every drop is a real piece landing at its real position. With the
// stream connected it starts when the piece finished; without it, a
// second's pieces are spread across the next second. On a fast download
// only some pieces get the full drop (see ShowcaseBudget); the rest land
// with a quiet blink.
//
// With animation off the bar is a plain strip that fills in, and nothing
// moves.

export interface BarColors {
  downloading: string;
  success: string;
  warning: string;
  well: string;
  divider: string;
}

const STATIC_BAR = 22; // bar height with animation off
const LIVE_BAR = 40;   // bar height with animation on
const HEADROOM = 30;   // space above the bar for pieces to fall through
const UNKNOWN_LEVEL = 0.15; // fill shown for an in-progress piece before its level is known

interface Arrival { piece: number; start: number; duration: number; showcase: boolean }
interface CellFlash { cell: number; start: number }
interface Pulse { x: number; dir: -1 | 1; start: number }
interface Spark { x: number; vx: number; vy: number; start: number; life: number }

type Rgb = [number, number, number];

export class PieceBarEngine {
  private readonly ctx: CanvasRenderingContext2D | null;
  private width = 0;
  private animate = false;
  private streamLive = false;
  private colors: BarColors;
  private rgb: { downloading: Rgb; flash: Rgb } = { downloading: [59, 158, 255], flash: [214, 235, 255] };
  private segments: FileSegment[] = [];

  // Per piece.
  private numPieces = 0;
  private snap = new Uint8Array(0);     // state in the latest snapshot
  private landed = new Uint8Array(0);   // drawn as part of the bar
  private flying = new Uint8Array(0);   // finished, drop still in the air
  private frac = new Float32Array(0);   // fraction downloaded, 0 when unknown
  private hasSnapshot = false;
  private landedCount = 0;
  private sawIncomplete = false;

  // Per cell.
  private cells = 0;
  private cellWidth = 1;
  private total = new Uint16Array(0);
  private settled = new Uint16Array(0);
  private checking = new Uint8Array(0);
  private level = new Float32Array(0);  // target fill of the cell's in-progress pieces, 0 when none
  private shown = new Float32Array(0);  // fill currently drawn, easing toward level

  // Things in motion.
  private arrivals: Arrival[] = [];
  private ripples: CellFlash[] = [];
  private soft: CellFlash[] = [];
  private pulses: Pulse[] = [];
  private sparks: Spark[] = [];
  private finishedAt = 0;
  private readonly budget = new ShowcaseBudget();
  private readonly rate = new RateWindow();

  private raf: number | null = null;
  private lastFrame = 0;
  private disposed = false;

  constructor(private readonly canvas: HTMLCanvasElement, colors: BarColors) {
    this.ctx = canvas.getContext("2d");
    this.colors = colors;
    this.setColors(colors);
  }

  // height is the canvas height in CSS pixels for the current mode.
  get height(): number {
    return this.animate ? HEADROOM + LIVE_BAR : STATIC_BAR;
  }

  private get barTop(): number {
    return this.animate ? HEADROOM : 0;
  }

  private get barHeight(): number {
    return this.animate ? LIVE_BAR : STATIC_BAR;
  }

  dispose(): void {
    this.disposed = true;
    if (this.raf !== null) cancelAnimationFrame(this.raf);
    this.raf = null;
  }

  setColors(colors: BarColors): void {
    this.colors = colors;
    const downloading = this.toRgb(colors.downloading, [59, 158, 255]);
    const well = this.toRgb(colors.well, [42, 42, 53]);
    // The flash has to stand out from the bar's well: lighter than the
    // download colour on a dark theme, darker on a light one.
    const darkWell = 0.2126 * well[0] + 0.7152 * well[1] + 0.0722 * well[2] < 128;
    const toward: Rgb = darkWell ? [255, 255, 255] : [0, 0, 0];
    const mix = darkWell ? 0.75 : 0.4;
    this.rgb = {
      downloading,
      flash: downloading.map((v, i) => Math.round(v + (toward[i] - v) * mix)) as Rgb,
    };
    this.kick();
  }

  setSegments(segments: FileSegment[]): void {
    this.segments = segments;
    this.kick();
  }

  setStreamLive(live: boolean): void {
    this.streamLive = live;
  }

  // resize sets the bar's width and whether it animates. Pieces in the
  // air land at once: a resize isn't worth animating through.
  resize(width: number, animate: boolean): void {
    this.width = Math.max(1, Math.floor(width));
    this.animate = animate;
    this.landAllFlying();
    this.clearMotion();
    if (!animate) this.finishedAt = 0;

    const dpr = window.devicePixelRatio || 1;
    this.canvas.width = Math.round(this.width * dpr);
    this.canvas.height = Math.round(this.height * dpr);
    this.canvas.style.height = `${this.height}px`;
    this.ctx?.setTransform(dpr, 0, 0, dpr, 0, 0);

    this.rebuildCells();
    this.checkFinished(performance.now());
    this.kick();
  }

  // applySnapshot reconciles the bar with the full piece state.
  applySnapshot(runs: PieceStateRun[], numPieces: number, now = performance.now()): void {
    if (numPieces !== this.numPieces) this.resetPieces(numPieces);
    const first = !this.hasSnapshot;
    this.hasSnapshot = true;
    this.snap = expandRuns(runs, numPieces);

    const newly: number[] = [];
    for (let i = 0; i < numPieces; i++) {
      if (this.snap[i] === COMPLETE) {
        if (!this.landed[i] && !this.flying[i]) newly.push(i);
      } else if (this.landed[i]) {
        // A piece that failed a recheck is no longer part of the bar.
        this.landed[i] = 0;
        this.landedCount--;
      }
    }

    if (first || !this.animate || newly.length > BULK_LANDING) {
      for (const i of newly) this.settle(i);
    } else {
      // With the stream connected these are pieces it hasn't told us about
      // yet, so they drop now. Without it, a second's worth arrives at
      // once and is spread over the second that follows.
      for (const i of newly) this.launch(i, this.streamLive ? now : now + Math.random() * 940);
    }

    if (this.landedCount < numPieces) this.sawIncomplete = true;
    this.rebuildCells();
    this.checkFinished(now);
    this.kick();
  }

  // onStream handles a batch from the live stream.
  onStream(update: PieceUpdate, now = performance.now()): void {
    if (!this.hasSnapshot) return;
    for (const i of update.done ?? []) {
      if (i < 0 || i >= this.numPieces || this.landed[i] || this.flying[i]) continue;
      if (this.animate) this.launch(i, now);
      else this.settle(i);
    }
    for (const p of update.progress ?? []) {
      if (p.i < 0 || p.i >= this.numPieces) continue;
      this.frac[p.i] = p.f;
      if (this.snap[p.i] === MISSING) this.snap[p.i] = PARTIAL;
      this.refreshCell(cellOfPiece(p.i, this.numPieces, this.cells));
    }
    this.kick();
  }

  // ---- pieces --------------------------------------------------------------

  private resetPieces(numPieces: number): void {
    this.numPieces = numPieces;
    this.snap = new Uint8Array(numPieces);
    this.landed = new Uint8Array(numPieces);
    this.flying = new Uint8Array(numPieces);
    this.frac = new Float32Array(numPieces);
    this.hasSnapshot = false;
    this.landedCount = 0;
    this.sawIncomplete = false;
    this.finishedAt = 0;
    this.clearMotion();
  }

  private clearMotion(): void {
    this.arrivals = [];
    this.ripples = [];
    this.soft = [];
    this.pulses = [];
    this.sparks = [];
  }

  private settle(piece: number): void {
    if (this.landed[piece]) return;
    this.landed[piece] = 1;
    this.flying[piece] = 0;
    this.landedCount++;
  }

  private landAllFlying(): void {
    for (const a of this.arrivals) this.settle(a.piece);
  }

  private launch(piece: number, start: number): void {
    this.flying[piece] = 1;
    this.rate.add(start);
    const showcase = this.budget.take(start);
    this.arrivals.push({
      piece,
      start,
      duration: showcase ? fallDuration(this.rate.count(start)) : 0,
      showcase,
    });
  }

  private land(a: Arrival, now: number): void {
    this.settle(a.piece);
    const cell = cellOfPiece(a.piece, this.numPieces, this.cells);
    this.refreshCell(cell);
    if (!a.showcase) {
      if (this.soft.length < 400) this.soft.push({ cell, start: now });
      return;
    }
    const x = (cell + 0.5) * this.cellWidth;
    this.ripples.push({ cell, start: now });
    // Closing a gap: send a pulse along the stretch that just joined up.
    if (this.full(cell) && this.full(cell - 1) && this.full(cell + 1)) {
      this.pulses.push({ x, dir: -1, start: now }, { x, dir: 1, start: now });
    }
    const count = 3 + Math.floor(Math.random() * 3);
    for (let k = 0; k < count; k++) {
      this.sparks.push({
        x,
        vx: (Math.random() - 0.5) * 90,
        vy: -(50 + Math.random() * 90),
        start: now,
        life: 380 + Math.random() * 260,
      });
    }
  }

  private checkFinished(now: number): void {
    if (this.finishedAt || this.numPieces === 0 || this.landedCount < this.numPieces) return;
    // The sweep plays only for a download seen finishing, not for a
    // torrent that was already complete when the page opened.
    this.finishedAt = this.sawIncomplete && this.animate ? now : -1;
  }

  // ---- cells ---------------------------------------------------------------

  private full(cell: number): boolean {
    return cell >= 0 && cell < this.cells && this.settled[cell] >= this.total[cell];
  }

  private rebuildCells(): void {
    const cells = cellCount(this.numPieces, this.width);
    const sameLayout = cells === this.cells;
    this.cells = cells;
    this.cellWidth = cells > 0 ? this.width / cells : 1;
    this.total = new Uint16Array(cells);
    this.settled = new Uint16Array(cells);
    this.checking = new Uint8Array(cells);
    this.level = new Float32Array(cells);
    if (!sameLayout) this.shown = new Float32Array(cells);
    for (let c = 0; c < cells; c++) this.refreshCell(c);
  }

  private refreshCell(cell: number): void {
    if (cell < 0 || cell >= this.cells) return;
    const [start, end] = cellPieceRange(cell, this.numPieces, this.cells);
    let settled = 0;
    let checking = 0;
    let level = 0;
    for (let i = start; i < end; i++) {
      if (this.landed[i]) {
        settled++;
        continue;
      }
      if (this.snap[i] === CHECKING) checking = 1;
      // A finished piece still in the air stays drawn as in progress, full
      // to the top, until it lands.
      const f = this.flying[i] ? 1 : this.snap[i] === PARTIAL ? Math.max(this.frac[i], UNKNOWN_LEVEL) : 0;
      if (f > level) level = f;
    }
    this.total[cell] = end - start;
    this.settled[cell] = settled;
    this.checking[cell] = checking;
    this.level[cell] = level;
  }

  // ---- drawing -------------------------------------------------------------

  private kick(): void {
    if (this.disposed || this.raf !== null) return;
    this.raf = requestAnimationFrame((now) => {
      this.raf = null;
      this.frame(now);
    });
  }

  private toRgb(color: string, fallback: Rgb): Rgb {
    if (!this.ctx) return fallback;
    // Assigning to fillStyle normalises any CSS colour to #rrggbb.
    this.ctx.fillStyle = "#000000";
    this.ctx.fillStyle = color;
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(String(this.ctx.fillStyle));
    return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : fallback;
  }

  private rgba(c: Rgb, alpha: number): string {
    return `rgba(${c[0]},${c[1]},${c[2]},${Math.max(0, Math.min(1, alpha)).toFixed(3)})`;
  }

  private frame(now: number): void {
    const ctx = this.ctx;
    if (!ctx || this.disposed) return;
    const dt = this.lastFrame ? Math.min(0.1, (now - this.lastFrame) / 1000) : 0;
    this.lastFrame = now;

    // Land whatever is due.
    if (this.arrivals.length > 0) {
      const waiting: Arrival[] = [];
      for (const a of this.arrivals) {
        if (now >= a.start + a.duration) this.land(a, now);
        else waiting.push(a);
      }
      this.arrivals = waiting;
      this.checkFinished(now);
    }

    const W = this.width;
    const top = this.barTop;
    const H = this.barHeight;
    const cw = this.cellWidth;
    const drawW = Math.ceil(cw);
    const { downloading, flash } = this.rgb;
    const flashCss = this.rgba(flash, 1);
    const finished = this.finishedAt !== 0;

    ctx.globalAlpha = 1;
    ctx.clearRect(0, 0, W, this.height);

    ctx.save();
    ctx.beginPath();
    ctx.roundRect(0, top, W, H, this.animate ? 4 : 3);
    ctx.clip();

    ctx.fillStyle = this.colors.well;
    ctx.fillRect(0, top, W, H);

    // Finished pieces.
    ctx.fillStyle = finished && this.finishedAt < 0 ? this.colors.success : this.colors.downloading;
    for (let c = 0; c < this.cells; c++) {
      if (this.settled[c] === 0) continue;
      ctx.globalAlpha = this.settled[c] / this.total[c];
      ctx.fillRect(c * cw, top, drawW, H);
    }

    // Pieces being checked.
    ctx.globalAlpha = 1;
    ctx.fillStyle = this.colors.warning;
    for (let c = 0; c < this.cells; c++) {
      if (this.checking[c]) ctx.fillRect(c * cw, top, drawW, H);
    }

    // Pieces in progress: a level that rises as the piece downloads, with
    // a band of light moving across. With animation off, a flat tint.
    let anyPartial = false;
    ctx.fillStyle = this.colors.downloading;
    for (let c = 0; c < this.cells; c++) {
      const target = this.level[c];
      if (target <= 0 || this.checking[c] || this.settled[c] >= this.total[c]) {
        this.shown[c] = 0;
        continue;
      }
      anyPartial = true;
      const x = c * cw;
      if (!this.animate) {
        ctx.globalAlpha = 0.45;
        ctx.fillRect(x, top, drawW, H);
        continue;
      }
      this.shown[c] = approach(this.shown[c], target, dt);
      const h = H * this.shown[c];
      const band = (((x - now * 0.09) % 140) + 140) % 140;
      ctx.globalAlpha = 0.34 + (band < 26 ? 0.3 * Math.sin((band / 26) * Math.PI) : 0);
      ctx.fillRect(x, top + H - h, drawW, h);
      ctx.globalAlpha = 0.9;
      ctx.fillRect(x, top + H - h, drawW, 1.5);
    }

    // Quiet arrivals: a short blink.
    ctx.fillStyle = flashCss;
    this.soft = this.soft.filter((s) => {
      const age = now - s.start;
      if (age >= SOFT_MS) return false;
      ctx.globalAlpha = 0.55 * (1 - age / SOFT_MS);
      ctx.fillRect(s.cell * cw, top, Math.max(1.5, cw), H);
      return true;
    });

    // Landing ripple: a glow that spreads sideways and fades.
    this.ripples = this.ripples.filter((r) => {
      const age = now - r.start;
      if (age >= RIPPLE_MS) return false;
      const t = age / RIPPLE_MS;
      const x = (r.cell + 0.5) * cw;
      const spread = 5 + 22 * (1 - Math.pow(1 - t, 3));
      const glow = ctx.createLinearGradient(x - spread, 0, x + spread, 0);
      glow.addColorStop(0, this.rgba(flash, 0));
      glow.addColorStop(0.5, this.rgba(flash, 0.75 * (1 - t)));
      glow.addColorStop(1, this.rgba(flash, 0));
      ctx.globalAlpha = 1;
      ctx.fillStyle = glow;
      ctx.fillRect(x - spread, top, spread * 2, H);
      ctx.fillStyle = flashCss;
      ctx.globalAlpha = Math.pow(1 - t, 2);
      ctx.fillRect(x - Math.max(1, cw / 2), top, Math.max(2, cw), H);
      return true;
    });

    // Join pulse: a bright bead running along the newly joined stretch.
    this.pulses = this.pulses.filter((p) => {
      const dist = (now - p.start) * 0.34;
      if (dist > 130) return false;
      const x = p.x + p.dir * dist;
      if (!this.full(Math.floor(x / cw))) return false;
      const tail = x - p.dir * 26;
      const bead = ctx.createLinearGradient(tail, 0, x, 0);
      bead.addColorStop(0, this.rgba(flash, 0));
      bead.addColorStop(1, this.rgba(flash, 0.85 * (1 - dist / 130)));
      ctx.globalAlpha = 1;
      ctx.fillStyle = bead;
      ctx.fillRect(Math.min(x, tail), top, 26, H);
      return true;
    });

    // Finish: one sweep of light, the bar settling into the finished
    // colour behind it.
    let sweeping = false;
    if (this.finishedAt > 0) {
      const t = (now - this.finishedAt) / FINISH_MS;
      const edge = Math.min(1, Math.max(0, t)) * (W + 120) - 60;
      ctx.globalAlpha = 1;
      ctx.fillStyle = this.colors.success;
      ctx.fillRect(0, top, Math.min(W, Math.max(0, edge)), H);
      if (t < 1) {
        sweeping = true;
        const light = ctx.createLinearGradient(edge - 90, 0, edge + 30, 0);
        light.addColorStop(0, "rgba(255,255,255,0)");
        light.addColorStop(0.75, "rgba(255,255,255,0.9)");
        light.addColorStop(1, "rgba(255,255,255,0)");
        ctx.fillStyle = light;
        ctx.fillRect(edge - 90, top, 120, H);
      }
    }

    // File boundaries.
    if (this.segments.length > 1) {
      ctx.fillStyle = this.colors.divider;
      ctx.globalAlpha = 0.7;
      for (let i = 1; i < this.segments.length; i++) ctx.fillRect(this.segments[i].xStart, top, 1, H);
    }
    ctx.globalAlpha = 1;
    ctx.restore();

    // Above the bar: a landed piece overshoots for a moment.
    ctx.fillStyle = flashCss;
    for (const r of this.ripples) {
      const age = now - r.start;
      if (age > 220) continue;
      const t = age / 220;
      const rise = 5 * Math.sin(t * Math.PI);
      ctx.globalAlpha = 1 - t;
      ctx.fillRect((r.cell + 0.5) * cw - Math.max(1, cw / 2), top - rise, Math.max(2, cw), rise + 1);
    }

    // Falling pieces, each with a short trail.
    for (const a of this.arrivals) {
      if (!a.showcase || now < a.start) continue;
      const t = (now - a.start) / a.duration;
      const y = -8 + (top + 2) * fallOffset(t);
      const x = (cellOfPiece(a.piece, this.numPieces, this.cells) + 0.5) * cw;
      const w = Math.max(3, cw);
      const trail = ctx.createLinearGradient(0, y - 16 * t, 0, y);
      trail.addColorStop(0, this.rgba(downloading, 0));
      trail.addColorStop(1, this.rgba(downloading, 0.7));
      ctx.globalAlpha = 1;
      ctx.fillStyle = trail;
      ctx.fillRect(x - w / 2, y - 16 * t, w, 16 * t);
      ctx.fillStyle = flashCss;
      ctx.fillRect(x - w / 2, y, w, 6);
    }

    // Sparks thrown up by a landing.
    ctx.fillStyle = flashCss;
    this.sparks = this.sparks.filter((s) => {
      const age = now - s.start;
      if (age >= s.life) return false;
      const ts = age / 1000;
      const y = top + s.vy * ts + 260 * ts * ts;
      if (y <= top) {
        ctx.globalAlpha = 1 - age / s.life;
        ctx.fillRect(s.x + s.vx * ts - 0.9, y - 0.9, 1.8, 1.8);
      }
      return true;
    });
    ctx.globalAlpha = 1;

    // Keep drawing only while something is moving.
    const moving =
      this.arrivals.length > 0 || this.ripples.length > 0 || this.soft.length > 0 ||
      this.pulses.length > 0 || this.sparks.length > 0 || sweeping || (this.animate && anyPartial);
    if (moving) this.kick();
    else this.lastFrame = 0;
  }
}
