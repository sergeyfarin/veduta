// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import StatusListBlock from './StatusListBlock.svelte';

describe('StatusListBlock', () => {
  it('renders a label per row', () => {
    render(StatusListBlock, {
      props: {
        block: {
          type: 'status',
          items: [
            { label: 'immich-server', level: 'ok' },
            { label: 'frigate', level: 'warn', text: 'restarting' }
          ]
        }
      }
    });
    expect(screen.getByText('immich-server')).toBeInTheDocument();
    expect(screen.getByText('frigate')).toBeInTheDocument();
    expect(screen.getByText('restarting')).toBeInTheDocument();
  });

  it('a status with no text renders a bare, accessibly-labelled dot (B1 Status.svelte rule)', () => {
    render(StatusListBlock, {
      props: { block: { type: 'status', items: [{ label: 'adguard-home', level: 'ok' }] } }
    });
    // No visible text beyond the label itself; the dot's own label is screen-reader-only.
    expect(screen.queryByText('Online')).not.toBeInTheDocument();
  });
});
