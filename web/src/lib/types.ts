/** Payload of GET /api/v1/version. Mirrors internal/version.Info. */
export interface BuildInfo {
  version: string;
  commit: string;
  sourceUrl: string;
}

/**
 * Core-owned execution state. An integration cannot write any of it: it says when a card ran,
 * whether what you are looking at is current, and how it failed. Mirrors the `execution` half of
 * schemas/card-state.v1.schema.json; the typed Widget Document arrives with milestone B2.
 */
export type CardState = 'pending' | 'ok' | 'stale' | 'error' | 'disabled';

export type StatusLevel = 'ok' | 'warn' | 'error' | 'info' | 'muted' | 'unknown';

/** Height and width in grid units, mirroring span: {columns, rows} in the configuration. */
export interface Span {
  columns?: 1 | 2 | 3 | 4;
  rows?: 1 | 2 | 3 | 4;
}
