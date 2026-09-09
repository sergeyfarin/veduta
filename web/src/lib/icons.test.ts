// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, it } from 'vitest';
import { iconFallback, iconSource } from './icons';

describe('icons', () => {
  it('routes named sets and URLs through the core proxy', () => {
    expect(iconSource('sh:immich')).toBe('/api/v1/icons/sh%3Aimmich');
    expect(iconSource('https://example.com/icon.svg')).toBe(
      '/api/v1/icons/https%3A%2F%2Fexample.com%2Ficon.svg'
    );
  });

  it('leaves literal glyphs local and supplies a failure fallback', () => {
    expect(iconSource('IM')).toBeUndefined();
    expect(iconFallback('IM')).toBe('IM');
    expect(iconFallback('mdi:home-assistant')).toBe('HO');
  });

  it('does not send malformed named specs to the endpoint', () => {
    expect(iconSource('sh:../secret')).toBeUndefined();
    expect(iconSource('si:UPPER')).toBeUndefined();
  });
});
