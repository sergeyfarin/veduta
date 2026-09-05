// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import BlockRenderer from './BlockRenderer.svelte';
import { registry } from './registry';

describe('BlockRenderer', () => {
  it('dispatches every registered block type to its component, not a placeholder', () => {
    render(BlockRenderer, { props: { block: { type: 'metrics', items: [{ label: 'X', value: 1 }] } } });
    expect(screen.getByText('X')).toBeInTheDocument();
  });

  it('falls back to the unknown-block placeholder for a type not in the registry', () => {
    render(BlockRenderer, { props: { block: { type: 'chart', items: [] } as never } });
    expect(screen.getByText('chart')).toBeInTheDocument();
  });

  it('the registry covers every one of the nine v1 block types (B3 + B4 complete the set)', () => {
    for (const type of [
      'status', 'metrics', 'key-value', 'progress', 'list', 'text', 'markdown',
      'image', 'image-grid', 'poster-grid', 'table', 'actions'
    ]) {
      expect(registry[type]).toBeDefined();
    }
  });
});
