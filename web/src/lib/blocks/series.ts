// SPDX-License-Identifier: AGPL-3.0-or-later

// Geometry for the series block, kept apart from the component so the rules that decide what a
// chart says - where a gap is, what the axis spans, which values are clipped - are tested as plain
// functions rather than read back out of rendered SVG.
import type { BlockSeries, SeriesLine, SeriesPoint } from '../types/widget';
import type { CardHistory } from '../types/cardstate';

export const VIEW_WIDTH = 300;
export const VIEW_HEIGHT = 64;
// Room at the top and bottom so a line at the extreme of its range is not half-clipped by the
// viewBox edge.
const PAD_Y = 3;

export interface LineGeometry {
  /** One `points` attribute per unbroken run of readings. A null reading ends a run. */
  runs: string[];
  /** Readings with no neighbour on either side, which a polyline of one point would not draw. */
  dots: [number, number][];
}

export interface SeriesGeometry {
  lines: LineGeometry[];
  /** The value range the drawing spans: the block's own min/max where given, else the data's. */
  low: number;
  high: number;
  /** Milliseconds from the first sample to the last, across every series. */
  spanMs: number;
  /** False when no series has a single numeric reading; the component shows its empty state. */
  hasData: boolean;
}

/**
 * The lines a block draws, whichever way it was written. A supplied block carries its own points.
 * A bound block names its card's retained signals, and the points come from the card state the
 * core attached them to - a signal with nothing retained yet is an empty line, not an error, so a
 * new card shows its legend and "no readings" until the history fills in.
 */
export function resolveLines(block: BlockSeries, history?: CardHistory): SeriesLine[] {
  if (block.history) {
    return block.history.lines.map((line) => ({
      label: line.label,
      ...(line.level ? { level: line.level } : {}),
      points: history?.[line.signal] ?? []
    }));
  }
  return block.series ?? [];
}

export function geometry(block: BlockSeries, history?: CardHistory): SeriesGeometry {
  const series = resolveLines(block, history);
  let tMin = Infinity;
  let tMax = -Infinity;
  let vMin = Infinity;
  let vMax = -Infinity;
  for (const line of series) {
    for (const p of line.points) {
      const t = Date.parse(p.t);
      if (t < tMin) tMin = t;
      if (t > tMax) tMax = t;
      if (p.v !== null) {
        if (p.v < vMin) vMin = p.v;
        if (p.v > vMax) vMax = p.v;
      }
    }
  }
  const hasData = vMin !== Infinity;
  let low = block.min ?? (hasData ? vMin : 0);
  let high = block.max ?? (hasData ? vMax : 1);
  if (high <= low) {
    // A flat series, or data entirely on one side of a single fixed bound. Give the line room to
    // sit in the middle rather than dividing by zero.
    const pad = Math.abs(low) * 0.1 || 1;
    if (block.min === undefined) low -= pad;
    if (block.max === undefined || high <= low) high = low + 2 * pad;
  }

  const x = (t: number) =>
    tMax > tMin ? ((t - tMin) / (tMax - tMin)) * VIEW_WIDTH : VIEW_WIDTH / 2;
  // Values outside a fixed range are drawn at its edge, not off the chart; the text summary still
  // reports the real value.
  const y = (v: number) => {
    const clamped = Math.min(high, Math.max(low, v));
    return PAD_Y + (1 - (clamped - low) / (high - low)) * (VIEW_HEIGHT - 2 * PAD_Y);
  };
  const r = (n: number) => Math.round(n * 100) / 100;

  const lines = series.map((line) => {
    const runs: string[] = [];
    const dots: [number, number][] = [];
    let run: [number, number][] = [];
    const flush = () => {
      const [only] = run;
      if (run.length === 1 && only) dots.push(only);
      else if (run.length > 1) runs.push(run.map(([px, py]) => `${px},${py}`).join(' '));
      run = [];
    };
    for (const p of line.points) {
      if (p.v === null) {
        flush();
        continue;
      }
      run.push([r(x(Date.parse(p.t))), r(y(p.v))]);
    }
    flush();
    return { runs, dots };
  });

  return { lines, low, high, spanMs: hasData ? tMax - tMin : 0, hasData };
}

/** The last numeric reading of a line, or undefined when it has none. */
export function latest(points: SeriesPoint[]): number | undefined {
  for (let i = points.length - 1; i >= 0; i--) {
    const v = points[i]?.v;
    if (v !== null && v !== undefined) return v;
  }
  return undefined;
}

/** The lowest and highest numeric readings of a line, or undefined when it has none. */
export function extent(points: SeriesPoint[]): [number, number] | undefined {
  let lo = Infinity;
  let hi = -Infinity;
  for (const p of points) {
    if (p.v === null) continue;
    if (p.v < lo) lo = p.v;
    if (p.v > hi) hi = p.v;
  }
  return lo === Infinity ? undefined : [lo, hi];
}
