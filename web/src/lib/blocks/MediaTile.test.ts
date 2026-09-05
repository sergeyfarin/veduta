// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import MediaTile from './MediaTile.svelte';

describe('MediaTile', () => {
  it('shows a skeleton placeholder before the image has loaded', () => {
    const { container } = render(MediaTile, {
      props: { item: { image: { ref: 'v1.a.b' } }, defaultAspect: '1 / 1' }
    });
    expect(container.querySelector('.placeholder')).not.toBeNull();
    const img = container.querySelector('img') as HTMLImageElement;
    expect(img.classList.contains('loaded')).toBe(false);
  });

  it('the placeholder disappears and the image fades in once it loads', async () => {
    const { container } = render(MediaTile, {
      props: { item: { image: { ref: 'v1.a.b' } }, defaultAspect: '1 / 1' }
    });
    const img = container.querySelector('img') as HTMLImageElement;
    img.dispatchEvent(new Event('load'));
    await Promise.resolve();
    expect(container.querySelector('.placeholder')).toBeNull();
    expect(img.classList.contains('loaded')).toBe(true);
  });

  it('a failed image swaps to the broken-image placeholder rather than the native broken icon', async () => {
    const { container } = render(MediaTile, {
      props: { item: { image: { ref: 'v1.a.b' }, title: 'Dune' }, defaultAspect: '2 / 3' }
    });
    const img = container.querySelector('img') as HTMLImageElement;
    img.dispatchEvent(new Event('error'));
    await Promise.resolve();
    expect(container.querySelector('img')).toBeNull();
    const broken = container.querySelector('.broken');
    expect(broken).not.toBeNull();
    expect(broken).toHaveAttribute('aria-label', 'Dune');
  });

  it('the broken placeholder still reserves the same aspect-ratio box - no layout shift on failure', async () => {
    const { container } = render(MediaTile, {
      props: { item: { image: { ref: 'v1.a.b', aspect: '2:3' } }, defaultAspect: '1 / 1' }
    });
    const img = container.querySelector('img') as HTMLImageElement;
    img.dispatchEvent(new Event('error'));
    await Promise.resolve();
    const tile = container.querySelector('.tile') as HTMLElement;
    expect(tile.style.aspectRatio).toBe('2 / 3');
  });

  it('a caption renders only when a title or subtitle is present', () => {
    const withCaption = render(MediaTile, {
      props: { item: { image: { ref: 'v1.a.b' }, title: 'Dune' }, defaultAspect: '1 / 1' }
    });
    expect(withCaption.container.querySelector('.cap')).not.toBeNull();

    const withoutCaption = render(MediaTile, {
      props: { item: { image: { ref: 'v1.a.b' } }, defaultAspect: '1 / 1' }
    });
    expect(withoutCaption.container.querySelector('.cap')).toBeNull();
  });
});
