// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import KeyValueBlock from './KeyValueBlock.svelte';

describe('KeyValueBlock', () => {
  it('renders every row regardless of count (unlike metrics, always the row layout)', () => {
    render(KeyValueBlock, {
      props: {
        block: {
          type: 'key-value',
          items: [
            { label: 'Movies', value: 438, format: 'count' },
            { label: 'Shows', value: 61, format: 'count' },
            { label: 'Streams', value: 2, format: 'count' }
          ]
        }
      }
    });
    expect(screen.getByText('Movies')).toBeInTheDocument();
    expect(screen.getByText('438')).toBeInTheDocument();
    expect(screen.getByText('Streams')).toBeInTheDocument();
  });

  it('a level colours the value, not the label', () => {
    const { container } = render(KeyValueBlock, {
      props: { block: { type: 'key-value', items: [{ label: 'Root', value: 92, format: 'percent', level: 'error' }] } }
    });
    expect(container.querySelector('.row.level-error')).not.toBeNull();
  });
});
