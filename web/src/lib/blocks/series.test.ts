// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { geometry, latest, extent, resolveLines, VIEW_WIDTH, VIEW_HEIGHT } from './series';
import type { BlockSeries } from '../types/widget';

const at = (h: number) => `2026-09-29T${String(h).padStart(2, '0')}:00:00Z`;
// Indexing is checked (noUncheckedIndexedAccess); these fail the test on a missing element
// rather than letting undefined flow into an assertion.
function must<T>(v: T | undefined): T {
  if (v === undefined) throw new Error('missing element');
  return v;
}
const coords = (run: string | undefined) =>
  must(run)
    .split(' ')
    .map((p) => p.split(',').map(Number) as [number, number]);

const block = (series: BlockSeries['series'], extra: Partial<BlockSeries> = {}): BlockSeries => ({
  type: 'series',
  series,
  ...extra
});

describe('series geometry', () => {
  it('spans the full width in time and the full height in value', () => {
    const g = geometry(block([{ label: 'a', points: [{ t: at(0), v: 0 }, { t: at(2), v: 10 }] }]));
    const pts = coords(must(g.lines[0]).runs[0]);
    const first = must(pts[0]);
    const last = must(pts[pts.length - 1]);
    expect(first[0]).toBe(0);
    expect(last[0]).toBe(VIEW_WIDTH);
    expect(first[1]).toBeGreaterThan(last[1]); // SVG y grows downward: the higher value is higher up
    expect(first[1]).toBeLessThanOrEqual(VIEW_HEIGHT);
    expect(g.spanMs).toBe(2 * 3600 * 1000);
  });

  // The point of carrying null at all: a missing reading must read as missing, never as a straight
  // line from the reading before it to the reading after.
  it('breaks the line at a null reading instead of interpolating across it', () => {
    const g = geometry(
      block([
        {
          label: 'a',
          points: [
            { t: at(0), v: 1 },
            { t: at(1), v: 2 },
            { t: at(2), v: null },
            { t: at(3), v: 3 },
            { t: at(4), v: 4 }
          ]
        }
      ])
    );
    expect(must(g.lines[0]).runs).toHaveLength(2);
    expect(must(g.lines[0]).dots).toHaveLength(0);
  });

  it('draws a reading isolated between two gaps as a dot, which a one-point polyline would not', () => {
    const g = geometry(
      block([{ label: 'a', points: [{ t: at(0), v: null }, { t: at(1), v: 5 }, { t: at(2), v: null }, { t: at(3), v: 6 }] }])
    );
    expect(must(g.lines[0]).runs).toHaveLength(0);
    expect(must(g.lines[0]).dots).toHaveLength(2);
  });

  it('shares one time axis between series, so their points line up', () => {
    const g = geometry(
      block([
        { label: 'long', points: [{ t: at(0), v: 1 }, { t: at(4), v: 1 }] },
        { label: 'short', points: [{ t: at(2), v: 1 }, { t: at(4), v: 1 }] }
      ])
    );
    const startOfShort = must(coords(must(g.lines[1]).runs[0])[0])[0];
    expect(startOfShort).toBe(VIEW_WIDTH / 2);
  });

  it('clamps a value outside a fixed range to its edge rather than drawing off the chart', () => {
    const g = geometry(block([{ label: 'a', points: [{ t: at(0), v: -50 }, { t: at(1), v: 500 }] }], { min: 0, max: 100 }));
    const ys = coords(must(g.lines[0]).runs[0]).map(([, y]) => y);
    for (const y of ys) {
      expect(y).toBeGreaterThanOrEqual(0);
      expect(y).toBeLessThanOrEqual(VIEW_HEIGHT);
    }
    expect([g.low, g.high]).toEqual([0, 100]);
  });

  it('gives a flat line a range instead of dividing by zero', () => {
    const g = geometry(block([{ label: 'a', points: [{ t: at(0), v: 7 }, { t: at(1), v: 7 }] }]));
    expect(g.high).toBeGreaterThan(g.low);
    for (const n of must(must(g.lines[0]).runs[0]).split(/[ ,]/).map(Number)) expect(Number.isFinite(n)).toBe(true);
  });

  it('reports no data when every reading is null or there are none', () => {
    expect(geometry(block([{ label: 'a', points: [] }])).hasData).toBe(false);
    expect(geometry(block([{ label: 'a', points: [{ t: at(0), v: null }] }])).hasData).toBe(false);
  });

  it('latest and extent skip nulls', () => {
    const pts = [
      { t: at(0), v: 4 },
      { t: at(1), v: 9 },
      { t: at(2), v: null }
    ];
    expect(latest(pts)).toBe(9);
    expect(extent(pts)).toEqual([4, 9]);
    expect(latest([{ t: at(0), v: null }])).toBeUndefined();
  });
});

describe('a block bound to retained history', () => {
  const bound: BlockSeries = {
    type: 'series',
    history: { window: '24h', lines: [{ signal: 'cpu.percent', label: 'CPU', level: 'warn' }, { signal: 'mem.percent', label: 'Memory' }] }
  };

  it('takes its points from the card history, by signal name', () => {
    const lines = resolveLines(bound, { 'cpu.percent': [{ t: at(0), v: 0.2 }, { t: at(1), v: 0.4 }] });
    expect(lines.map((l) => [l.label, l.level, l.points.length])).toEqual([
      ['CPU', 'warn', 2],
      ['Memory', undefined, 0]
    ]);
  });

  // A card that has just started has a document naming its signals and no stored history yet.
  // That is a normal state, not an error, and it must not borrow another signal's points.
  it('draws nothing, rather than failing, before any history exists', () => {
    expect(geometry(bound).hasData).toBe(false);
    expect(geometry(bound, {}).hasData).toBe(false);
  });

  it('ignores history it was not asked for', () => {
    const lines = resolveLines(bound, { 'disk.percent': [{ t: at(0), v: 1 }] });
    expect(lines.every((l) => l.points.length === 0)).toBe(true);
  });
});

