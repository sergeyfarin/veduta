// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { assetUrl, cssAspectRatio } from './assets';

describe('assetUrl', () => {
  it('builds the documented endpoint path', () => {
    expect(assetUrl('v1.abc.def')).toBe('/api/v1/assets/v1.abc.def');
  });
  it('encodes a token so it cannot break out of the path segment', () => {
    expect(assetUrl('v1.a/b')).toBe('/api/v1/assets/v1.a%2Fb');
  });
});

describe('cssAspectRatio', () => {
  it('converts the schema\'s colon syntax to CSS syntax', () => {
    expect(cssAspectRatio('2:3')).toBe('2 / 3');
    expect(cssAspectRatio('16:9')).toBe('16 / 9');
    expect(cssAspectRatio('1:1')).toBe('1 / 1');
  });
  it('returns undefined for an absent value, letting the caller fall back', () => {
    expect(cssAspectRatio(undefined)).toBeUndefined();
  });
  it('returns undefined for a malformed value rather than emitting broken CSS', () => {
    expect(cssAspectRatio('not-a-ratio')).toBeUndefined();
    expect(cssAspectRatio('2:')).toBeUndefined();
    expect(cssAspectRatio(':3')).toBeUndefined();
  });
});
