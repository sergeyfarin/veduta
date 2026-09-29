// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import SeriesBlock from './SeriesBlock.svelte';

const at = (h: number) => `2026-09-29T${String(h).padStart(2, '0')}:00:00Z`;

describe('SeriesBlock', () => {
  it('describes the chart in words: each line now, its range, and the span', () => {
    render(SeriesBlock, {
      props: {
        block: {
          type: 'series',
          title: 'Climate',
          format: 'temperature',
          series: [
            { label: 'Inside', points: [{ t: at(0), v: 20 }, { t: at(24 - 1), v: 21.5 }] },
            { label: 'Outside', points: [{ t: at(0), v: null }] }
          ]
        }
      }
    });
    const chart = screen.getByRole('img');
    expect(chart.getAttribute('aria-label')).toBe(
      'Climate over 23h. Inside: 21.5°C now, between 20°C and 21.5°C; Outside: no readings'
    );
  });

  it('tells lines apart by dash as well as colour', () => {
    const { container } = render(SeriesBlock, {
      props: {
        block: {
          type: 'series',
          series: [
            { label: 'a', points: [{ t: at(0), v: 1 }, { t: at(1), v: 2 }] },
            { label: 'b', points: [{ t: at(0), v: 2 }, { t: at(1), v: 1 }] }
          ]
        }
      }
    });
    const groups = container.querySelectorAll('svg[role="img"] g');
    expect(groups).toHaveLength(2);
    expect(groups[0]?.getAttribute('stroke-dasharray')).toBeNull();
    expect(groups[1]?.getAttribute('stroke-dasharray')).toBeTruthy();
  });

  it('shows an empty state, not an empty chart, when nothing has been read', () => {
    render(SeriesBlock, {
      props: { block: { type: 'series', series: [{ label: 'CPU', points: [] }] } }
    });
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByText('No readings yet')).toBeInTheDocument();
  });

  it('renders labels as text, never as markup', () => {
    const { container } = render(SeriesBlock, {
      props: {
        block: {
          type: 'series',
          series: [{ label: '<img src=x onerror=alert(1)>', points: [{ t: at(0), v: 1 }] }]
        }
      }
    });
    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeInTheDocument();
  });

  it('renders a bound block from the history it is handed', () => {
    render(SeriesBlock, {
      props: {
        block: { type: 'series', format: 'percent', history: { lines: [{ signal: 'cpu.percent', label: 'CPU' }] } },
        history: { 'cpu.percent': [{ t: at(0), v: 0.25 }, { t: at(1), v: 0.5 }] }
      }
    });
    expect(screen.getByRole('img').getAttribute('aria-label')).toBe(
      'Over 1h. CPU: 50.0% now, between 25.0% and 50.0%'
    );
  });
});
