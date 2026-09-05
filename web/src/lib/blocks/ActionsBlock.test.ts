// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import ActionsBlock from './ActionsBlock.svelte';

describe('ActionsBlock', () => {
  it('renders a button per action, labelled', () => {
    render(ActionsBlock, {
      props: {
        block: {
          type: 'actions',
          actions: [
            { id: 'restart', label: 'Restart' },
            { id: 'stop', label: 'Stop', danger: true }
          ]
        }
      }
    });
    expect(screen.getByRole('button', { name: /Restart/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Stop/ })).toBeInTheDocument();
  });

  it('every button is disabled - 0.1 renders actions inert, never wired to execute', () => {
    render(ActionsBlock, {
      props: { block: { type: 'actions', actions: [{ id: 'x', label: 'Do it' }] } }
    });
    expect(screen.getByRole('button', { name: 'Do it' })).toBeDisabled();
  });

  it('a danger action is visually distinguished', () => {
    const { container } = render(ActionsBlock, {
      props: { block: { type: 'actions', actions: [{ id: 'x', label: 'Delete', danger: true }] } }
    });
    expect(container.querySelector('button.danger')).not.toBeNull();
  });
});
