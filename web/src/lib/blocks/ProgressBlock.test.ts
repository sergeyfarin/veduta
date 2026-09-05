// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import ProgressBlock from './ProgressBlock.svelte';

describe('ProgressBlock', () => {
  it('sets the bar width from a 0-1 fraction', () => {
    const { container } = render(ProgressBlock, {
      props: { block: { type: 'progress', items: [{ label: 'CPU', progress: 0.42 }] } }
    });
    const fill = container.querySelector('.bar > span') as HTMLElement;
    expect(fill.style.width).toBe('42%');
  });

  it('clamps a value above 1 rather than overflowing the bar', () => {
    const { container } = render(ProgressBlock, {
      props: { block: { type: 'progress', items: [{ label: 'X', progress: 1.5 }] } }
    });
    const fill = container.querySelector('.bar > span') as HTMLElement;
    expect(fill.style.width).toBe('100%');
  });

  it('clamps a negative value to zero', () => {
    const { container } = render(ProgressBlock, {
      props: { block: { type: 'progress', items: [{ label: 'X', progress: -0.3 }] } }
    });
    const fill = container.querySelector('.bar > span') as HTMLElement;
    expect(fill.style.width).toBe('0%');
  });

  it('exposes an accessible progressbar role with the clamped percentage', () => {
    render(ProgressBlock, {
      props: { block: { type: 'progress', items: [{ label: 'Root', progress: 0.92, level: 'error' }] } }
    });
    const bar = screen.getByRole('progressbar', { name: 'Root' });
    expect(bar).toHaveAttribute('aria-valuenow', '92');
  });

  it('a warn/error level colours the bar fill', () => {
    const { container } = render(ProgressBlock, {
      props: { block: { type: 'progress', items: [{ label: 'X', progress: 0.9, level: 'error' }] } }
    });
    expect(container.querySelector('.bar.error')).not.toBeNull();
  });
});
