// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import MediaBlock from './MediaBlock.svelte';

function block(type: 'image' | 'image-grid' | 'poster-grid', items: unknown[] = [], extra: object = {}) {
  return { type, items, ...extra } as never;
}

describe('MediaBlock', () => {
  it('renders one tile per item', () => {
    const { container } = render(MediaBlock, {
      props: {
        block: block('image-grid', [
          { image: { ref: 'v1.a.b' } },
          { image: { ref: 'v1.c.d' } }
        ])
      }
    });
    expect(container.querySelectorAll('.tile')).toHaveLength(2);
  });

  it('an image-grid tile defaults to a 1:1 aspect ratio when the item has none', () => {
    const { container } = render(MediaBlock, {
      props: { block: block('image-grid', [{ image: { ref: 'v1.a.b' } }]) }
    });
    const tile = container.querySelector('.tile') as HTMLElement;
    expect(tile.style.aspectRatio).toBe('1 / 1');
  });

  it('a poster-grid tile defaults to 2:3', () => {
    const { container } = render(MediaBlock, {
      props: { block: block('poster-grid', [{ image: { ref: 'v1.a.b' } }]) }
    });
    const tile = container.querySelector('.tile') as HTMLElement;
    expect(tile.style.aspectRatio).toBe('2 / 3');
  });

  it("an item's own aspect overrides the per-kind default", () => {
    const { container } = render(MediaBlock, {
      props: { block: block('image-grid', [{ image: { ref: 'v1.a.b', aspect: '16:9' } }]) }
    });
    const tile = container.querySelector('.tile') as HTMLElement;
    expect(tile.style.aspectRatio).toBe('16 / 9');
  });

  it('the asset URL is built from the ref, never a raw url an integration could supply', () => {
    const { container } = render(MediaBlock, {
      props: { block: block('image-grid', [{ image: { ref: 'v1.abc.def' } }]) }
    });
    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe('/api/v1/assets/v1.abc.def');
  });

  it('an empty grid shows the custom empty message', () => {
    const { getByText } = render(MediaBlock, {
      props: { block: block('image-grid', [], { empty: 'No photos yet' }) }
    });
    expect(getByText('No photos yet')).toBeInTheDocument();
  });

  it('an item with a link wraps its tile in a real anchor', () => {
    const { container } = render(MediaBlock, {
      props: {
        block: block('poster-grid', [{ image: { ref: 'v1.a.b' }, link: 'https://example.com/x' }])
      }
    });
    const a = container.querySelector('a.tile-link');
    expect(a?.getAttribute('href')).toBe('https://example.com/x');
  });

  it('images use loading=lazy and decoding=async', () => {
    const { container } = render(MediaBlock, {
      props: { block: block('image-grid', [{ image: { ref: 'v1.a.b' } }]) }
    });
    const img = container.querySelector('img');
    expect(img).toHaveAttribute('loading', 'lazy');
    expect(img).toHaveAttribute('decoding', 'async');
  });

  it('an explicit columns count overrides the per-kind default', () => {
    const { container } = render(MediaBlock, {
      props: { block: block('image-grid', [{ image: { ref: 'v1.a.b' } }], { columns: 4 }) }
    });
    const media = container.querySelector('.media') as HTMLElement;
    expect(media.style.getPropertyValue('--cols')).toBe('4');
  });

  it('with no explicit columns, image-grid defaults to 3 and poster-grid to 5', () => {
    const grid = render(MediaBlock, { props: { block: block('image-grid', []) } });
    expect((grid.container.querySelector('.media') as HTMLElement).style.getPropertyValue('--cols')).toBe(
      '3'
    );
    const poster = render(MediaBlock, { props: { block: block('poster-grid', []) } });
    expect(
      (poster.container.querySelector('.media') as HTMLElement).style.getPropertyValue('--cols')
    ).toBe('5');
  });
});
