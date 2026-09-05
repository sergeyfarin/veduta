// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import TableBlock from './TableBlock.svelte';

describe('TableBlock', () => {
  it('renders a header cell per column and a row per data row', () => {
    const { container } = render(TableBlock, {
      props: {
        block: {
          type: 'table',
          columns: [{ key: 'name', label: 'Name' }, { key: 'size', label: 'Size', format: 'bytes' }],
          rows: [
            { name: 'immich-server', size: 2100000000 },
            { name: 'jellyfin', size: 1400000000 }
          ]
        }
      }
    });
    expect(container.querySelectorAll('thead th')).toHaveLength(2);
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    expect(screen.getByText('immich-server')).toBeInTheDocument();
    expect(screen.getByText('1.96 GB')).toBeInTheDocument();
  });

  it('a column with no label falls back to its key', () => {
    render(TableBlock, {
      props: { block: { type: 'table', columns: [{ key: 'host' }], rows: [{ host: 'nas' }] } }
    });
    expect(screen.getByText('host')).toBeInTheDocument();
  });

  it('align maps to a text-align class, defaulting to start', () => {
    const { container } = render(TableBlock, {
      props: {
        block: {
          type: 'table',
          columns: [{ key: 'a', align: 'end' }, { key: 'b' }],
          rows: [{ a: 1, b: 2 }]
        }
      }
    });
    const cells = container.querySelectorAll('tbody td');
    expect(cells[0]).toHaveClass('align-end');
    expect(cells[1]).toHaveClass('align-start');
  });

  it('a missing cell value renders the shared placeholder, not "undefined"', () => {
    render(TableBlock, {
      props: { block: { type: 'table', columns: [{ key: 'missing' }], rows: [{}] } }
    });
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('scrolls internally rather than growing the page - a scroll container wraps the table', () => {
    const { container } = render(TableBlock, {
      props: { block: { type: 'table', columns: [{ key: 'a' }], rows: [{ a: 1 }] } }
    });
    expect(container.querySelector('.scroll table')).not.toBeNull();
  });
});
