// SPDX-License-Identifier: AGPL-3.0-or-later

// Formats a Scalar value per its semantic Format hint. Units, locale and rounding are the
// renderer's job by design (docs/01-architecture.md section 4): an integration says "bytes",
// never "MiB", so every homelab dashboard looks consistent regardless of which integration
// produced a number - and so this is the ONE place that decision is made, not forty places.
import type { Format, Scalar } from './types/widget';

const EMPTY = '—'; // em dash: the one placeholder for "no value", regardless of format

const numberFormat = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 });
const countFormat = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });

// Binary (1024-based) byte thresholds, labelled with the decimal-style unit strings (KB, MB, GB)
// that mainstream software actually shows users - not the pedantically correct KiB/MiB/GiB,
// which is unfamiliar outside specialist contexts.
const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const;

function formatBytes(n: number): string {
  if (n === 0) return '0 B';
  const sign = n < 0 ? '-' : '';
  n = Math.abs(n);
  let unit = 0;
  while (n >= 1024 && unit < BYTE_UNITS.length - 1) {
    n /= 1024;
    unit++;
  }
  const precision = unit === 0 ? 0 : n < 10 ? 2 : n < 100 ? 1 : 0;
  return `${sign}${n.toFixed(precision)} ${BYTE_UNITS[unit]}`;
}

function formatDuration(totalSeconds: number): string {
  const sign = totalSeconds < 0 ? '-' : '';
  let s = Math.round(Math.abs(totalSeconds));
  if (s === 0) return '0s';
  const days = Math.floor(s / 86400);
  s -= days * 86400;
  const hours = Math.floor(s / 3600);
  s -= hours * 3600;
  const minutes = Math.floor(s / 60);
  s -= minutes * 60;
  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0) parts.push(`${hours}h`);
  // Once we're into days, minutes are noise on a status card - keep the two biggest units.
  if (minutes > 0 && days === 0) parts.push(`${minutes}m`);
  if (s > 0 && days === 0 && hours === 0) parts.push(`${s}s`);
  return sign + (parts.length > 0 ? parts.slice(0, 2).join(' ') : '0s');
}

const RELATIVE_UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 31536000],
  ['month', 2592000],
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60]
];
const relativeFormat = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });

export function formatRelativeTime(iso: string): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return EMPTY;
  const diffSeconds = (then - Date.now()) / 1000;
  for (const [unit, secondsInUnit] of RELATIVE_UNITS) {
    if (Math.abs(diffSeconds) >= secondsInUnit) {
      return relativeFormat.format(Math.round(diffSeconds / secondsInUnit), unit);
    }
  }
  return relativeFormat.format(Math.round(diffSeconds), 'second');
}

/**
 * Formats a Scalar per its Format hint. `unit`, when the caller provides one (MetricItem.unit),
 * is appended verbatim for the formats that don't already choose their own unit string - it
 * exists precisely for values format alone can't fully describe (a temperature caller wanting
 * °F instead of the °C default, a currency code).
 */
export function formatValue(value: Scalar | undefined, format?: Format, unit?: string): string {
  if (value === null || value === undefined) return EMPTY;

  switch (format) {
    case 'bytes':
      return typeof value === 'number' ? formatBytes(value) : String(value);
    case 'bytes-rate':
      return typeof value === 'number' ? `${formatBytes(value)}/s` : String(value);
    case 'percent':
      // Convention: a "percent"-formatted metric VALUE is a 0-1 fraction, matching the
      // pre-existing "progress" block's own 0-1 scale (schemas/widget-document.v1: progressItem's
      // `progress` field) and this repo's own example config (examples/veduta.yaml's AdGuard
      // card computes `num_blocked_filtering / num_dns_queries` for a format:"percent" value).
      return typeof value === 'number' ? `${(value * 100).toFixed(1)}%` : String(value);
    case 'duration':
      return typeof value === 'number' ? formatDuration(value) : String(value);
    case 'relative-time':
      return typeof value === 'string' ? formatRelativeTime(value) : String(value);
    case 'temperature':
      return typeof value === 'number' ? `${numberFormat.format(value)}${unit ?? '°C'}` : String(value);
    case 'currency':
      if (typeof value !== 'number') return String(value);
      try {
        return new Intl.NumberFormat(undefined, { style: 'currency', currency: unit ?? 'USD' }).format(value);
      } catch {
        return `${numberFormat.format(value)}${unit ? ' ' + unit : ''}`;
      }
    case 'count':
      return typeof value === 'number' ? countFormat.format(value) + (unit ? ' ' + unit : '') : String(value);
    case 'number':
    case undefined:
      return typeof value === 'number'
        ? numberFormat.format(value) + (unit ? ' ' + unit : '')
        : String(value);
    case 'text':
    default:
      return typeof value === 'boolean' ? (value ? 'true' : 'false') : String(value);
  }
}
