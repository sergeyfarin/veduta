// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { formatValue } from './format';

describe('formatValue', () => {
  describe('null and undefined are always the same placeholder, regardless of format', () => {
    const formats = ['bytes', 'percent', 'duration', 'relative-time', 'number', 'text', undefined] as const;
    for (const f of formats) {
      it(`format=${f}`, () => {
        expect(formatValue(null, f)).toBe('—');
        expect(formatValue(undefined, f)).toBe('—');
      });
    }
  });

  describe('bytes', () => {
    it('zero', () => expect(formatValue(0, 'bytes')).toBe('0 B'));
    it('small values stay in bytes', () => expect(formatValue(512, 'bytes')).toBe('512 B'));
    it('crosses into KB at 1024', () => expect(formatValue(1024, 'bytes')).toBe('1.00 KB'));
    it('a realistic library size', () => expect(formatValue(8461937274, 'bytes')).toBe('7.88 GB'));
    it('huge does not throw and never invents a unit past PB', () => {
      // A homelab disk/memory value never realistically exceeds low petabytes; PB is the
      // deliberate ceiling. An absurdly large input still renders (as a large PB count) rather
      // than throwing or silently truncating - "doesn't crash" matters more than "looks pretty"
      // for a value this far outside anything the product actually displays.
      expect(formatValue(1024 ** 6, 'bytes')).toBe('1024 PB');
      expect(formatValue(1024 ** 8, 'bytes')).toMatch(/^\d+ PB$/);
    });
    it('negative preserves the sign', () => expect(formatValue(-2048, 'bytes')).toBe('-2.00 KB'));
    it('non-numeric falls back to String()', () => expect(formatValue('n/a', 'bytes')).toBe('n/a'));
  });

  describe('bytes-rate', () => {
    it('appends /s', () => expect(formatValue(1048576, 'bytes-rate')).toBe('1.00 MB/s'));
  });

  describe('percent', () => {
    it('0-1 fraction convention, matching progressItem and the AdGuard example config', () => {
      expect(formatValue(0.314, 'percent')).toBe('31.4%');
    });
    it('zero', () => expect(formatValue(0, 'percent')).toBe('0.0%'));
    it('over 100% is not clamped - the renderer displays what it is given', () => {
      expect(formatValue(1.5, 'percent')).toBe('150.0%');
    });
    it('negative', () => expect(formatValue(-0.05, 'percent')).toBe('-5.0%'));
  });

  describe('duration', () => {
    it('zero', () => expect(formatValue(0, 'duration')).toBe('0s'));
    it('seconds only', () => expect(formatValue(45, 'duration')).toBe('45s'));
    it('shows the two biggest units, not just one', () =>
      expect(formatValue(125, 'duration')).toBe('2m 5s'));
    it('hours and minutes', () => expect(formatValue(3661, 'duration')).toBe('1h 1m'));
    it('an exact hour has nothing to add as a second unit', () =>
      expect(formatValue(3600, 'duration')).toBe('1h'));
    it('days drop minutes entirely - noise on a status card', () =>
      expect(formatValue(90000, 'duration')).toBe('1d 1h'));
    it('huge (multi-year uptime) does not throw', () => {
      expect(formatValue(1e9, 'duration')).toMatch(/^\d+d/);
    });
    it('negative preserves the sign', () => expect(formatValue(-45, 'duration')).toBe('-45s'));
  });

  describe('relative-time', () => {
    it('a few minutes ago', () => {
      const iso = new Date(Date.now() - 5 * 60 * 1000).toISOString();
      expect(formatValue(iso, 'relative-time')).toMatch(/minute/);
    });
    it('an invalid timestamp does not throw', () => {
      expect(formatValue('not-a-date', 'relative-time')).toBe('—');
    });
  });

  describe('temperature', () => {
    it('defaults to celsius', () => expect(formatValue(21, 'temperature')).toBe('21°C'));
    it('unit overrides the default', () => expect(formatValue(70, 'temperature', '°F')).toBe('70°F'));
    it('zero', () => expect(formatValue(0, 'temperature')).toBe('0°C'));
    it('negative', () => expect(formatValue(-10, 'temperature')).toBe('-10°C'));
  });

  describe('currency', () => {
    it('defaults to USD', () => expect(formatValue(1234.5, 'currency')).toContain('1,234.50'));
    it('an invalid currency code falls back instead of throwing', () => {
      expect(formatValue(5, 'currency', 'NOT-A-CODE')).toContain('5');
    });
  });

  describe('count', () => {
    it('zero', () => expect(formatValue(0, 'count')).toBe('0'));
    it('groups thousands, no decimals', () => expect(formatValue(48214, 'count')).toBe('48,214'));
    it('appends a unit when given', () => expect(formatValue(3, 'count', 'streams')).toBe('3 streams'));
  });

  describe('number (default format)', () => {
    it('zero', () => expect(formatValue(0)).toBe('0'));
    it('integers have no decimal point', () => expect(formatValue(438)).toBe('438'));
    it('non-integers round to 2 places', () => expect(formatValue(1.23456)).toBe('1.235'.slice(0, 4)));
    it('negative', () => expect(formatValue(-7)).toBe('-7'));
    it('huge', () => expect(formatValue(1e15)).toContain('1,000,000,000,000,000'));
  });

  describe('text', () => {
    it('passes strings through', () => expect(formatValue('Online', 'text')).toBe('Online'));
    it('renders booleans as words, not 0/1', () => {
      expect(formatValue(true, 'text')).toBe('true');
      expect(formatValue(false, 'text')).toBe('false');
    });
  });
});
