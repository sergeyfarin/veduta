// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import ListBlock from './ListBlock.svelte';

describe('ListBlock', () => {
  it('renders each item', () => {
    render(ListBlock, {
      props: {
        block: {
          type: 'list',
          items: [
            { title: 'immich-server', value: '2.1 GB' },
            { title: 'jellyfin', value: '1.4 GB' }
          ]
        }
      }
    });
    expect(screen.getByText('immich-server')).toBeInTheDocument();
    expect(screen.getByText('jellyfin')).toBeInTheDocument();
  });

  it('an empty list shows the custom empty message, not a blank box', () => {
    render(ListBlock, { props: { block: { type: 'list', items: [], empty: 'No active streams' } } });
    expect(screen.getByText('No active streams')).toBeInTheDocument();
  });

  it('an empty list with no custom message falls back to a default', () => {
    render(ListBlock, { props: { block: { type: 'list', items: [] } } });
    expect(screen.getByText('Nothing to show')).toBeInTheDocument();
  });

  it('a subtitle renders alongside the title', () => {
    render(ListBlock, {
      props: { block: { type: 'list', items: [{ title: 'Dune', subtitle: '2021' }] } }
    });
    expect(screen.getByText('2021')).toBeInTheDocument();
  });
});
