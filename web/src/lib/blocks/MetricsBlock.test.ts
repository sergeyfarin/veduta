// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import MetricsBlock from './MetricsBlock.svelte';

describe('MetricsBlock', () => {
  it('renders each item label and formatted value', () => {
    render(MetricsBlock, {
      props: {
        block: {
          type: 'metrics',
          items: [
            { label: 'Photos', value: 48214, format: 'count' },
            { label: 'Videos', value: 1903, format: 'count' },
            { label: 'Library', value: 8461937274, format: 'bytes' }
          ]
        }
      }
    });
    expect(screen.getByText('Photos')).toBeInTheDocument();
    expect(screen.getByText('48,214')).toBeInTheDocument();
    expect(screen.getByText('Library')).toBeInTheDocument();
    expect(screen.getByText('7.88 GB')).toBeInTheDocument();
  });

  it('a null value renders the shared em-dash placeholder, not "null" or a crash', () => {
    render(MetricsBlock, {
      props: { block: { type: 'metrics', items: [{ label: 'Uptime', value: null }] } }
    });
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('switches to the row layout at two items or fewer (S4: avoids a lopsided column grid)', () => {
    const { container } = render(MetricsBlock, {
      props: { block: { type: 'metrics', items: [{ label: 'A', value: 1 }, { label: 'B', value: 2 }] } }
    });
    expect(container.querySelector('.items.pair-items')).not.toBeNull();
  });

  it('uses the column layout at three items or more', () => {
    const { container } = render(MetricsBlock, {
      props: {
        block: {
          type: 'metrics',
          items: [{ label: 'A', value: 1 }, { label: 'B', value: 2 }, { label: 'C', value: 3 }]
        }
      }
    });
    expect(container.querySelector('.items.pair-items')).toBeNull();
  });

  it('an optional title renders as a heading', () => {
    render(MetricsBlock, {
      props: { block: { type: 'metrics', title: 'Storage', items: [{ label: 'Used', value: 1 }] } }
    });
    expect(screen.getByText('Storage')).toBeInTheDocument();
  });
});
