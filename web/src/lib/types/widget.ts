/**
 * GENERATED FILE - do not edit by hand.
 *
 * Produced by web/scripts/gen-types.mjs from schemas/widget-document.v1.schema.json.
 * Run `pnpm gen-types` to regenerate. CI fails if this file would change.
 */

export type ShortText = string;
/**
 * Structural only: scheme, a non-empty authority, no whitespace. Accepts bracketed IPv6 (http://[fd00::1]:8096). Host and port SEMANTICS are validated by the loader with net/url - a regex that tries to validate ports and IPv6 is both wrong and unreadable.
 */
export type SafeURL = string;
export type Level = 'ok' | 'warn' | 'error' | 'info' | 'muted' | 'unknown';
export type Block =
  | BlockMetrics
  | BlockKeyValue
  | BlockProgress
  | BlockStatus
  | BlockList
  | BlockMedia
  | BlockText
  | BlockTable
  | BlockActions;
export type Scalar = string | number | boolean | null;
export type Format =
  | 'text'
  | 'number'
  | 'bytes'
  | 'bytes-rate'
  | 'percent'
  | 'duration'
  | 'relative-time'
  | 'temperature'
  | 'currency'
  | 'count';
export type Text = string;

/**
 * The integration-owned half of a card: presentation (blocks) plus machine-readable semantics (signals). Freshness, staleness, errors and provenance are NOT here - they are core-owned and live in card-state.v1. See schemas/card-state.v1.schema.json.
 */
export interface WidgetDocument {
  schemaVersion: 1;
  title?: ShortText;
  subtitle?: ShortText;
  link?: SafeURL;
  status?: Status;
  /**
   * @maxItems 12
   */
  blocks: Block[];
  /**
   * Stable, machine-readable values. The ONLY substrate for rules, history and alerts - blocks are never queried. Every name must be declared in the integration manifest so rules can be validated at config-load time.
   */
  signals?: {
    [k: string]: Signal;
  };
  /**
   * Integration-reported soft problems (a sub-request failed, data is partial). Distinct from a failed invocation, which the core records in execution.error.
   *
   * @maxItems 4
   */
  notices?: {
    level: 'info' | 'warn';
    message: Text;
  }[];
  /**
   * Advisory only. The core clamps these to the card's configured schedule and its own maxima.
   */
  hints?: {
    ttlSeconds?: number;
  };
}
export interface Status {
  level: Level;
  text?: ShortText;
  since?: string;
}
export interface BlockMetrics {
  type: 'metrics';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  columns?: number;
  /**
   * @minItems 1
   * @maxItems 12
   */
  items: MetricItem[];
}
export interface MetricItem {
  label: ShortText;
  value: Scalar;
  format?: Format;
  unit?: string;
  level?: Level;
  icon?: string;
  link?: SafeURL;
}
export interface BlockKeyValue {
  type: 'key-value';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  /**
   * @minItems 1
   * @maxItems 24
   */
  items: MetricItem[];
}
export interface BlockProgress {
  type: 'progress';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  /**
   * @minItems 1
   * @maxItems 16
   */
  items: ProgressItem[];
}
export interface ProgressItem {
  label: ShortText;
  progress: number;
  value?: Scalar;
  format?: Format;
  level?: Level;
}
export interface BlockStatus {
  type: 'status';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  /**
   * @minItems 1
   * @maxItems 24
   */
  items: StatusItem[];
}
export interface StatusItem {
  label: ShortText;
  level: Level;
  text?: ShortText;
  since?: string;
  link?: SafeURL;
}
/**
 * An empty items array is legal and renders the block's empty state (for example: no active streams).
 */
export interface BlockList {
  type: 'list';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  empty?: ShortText;
  /**
   * @maxItems 100
   */
  items: ListItem[];
}
export interface ListItem {
  id?: string;
  title: ShortText;
  subtitle?: ShortText;
  value?: Scalar;
  format?: Format;
  level?: Level;
  icon?: string;
  image?: Image;
  link?: SafeURL;
  timestamp?: string;
}
/**
 * ref is an opaque HMAC-signed token minted by the capability broker against an approved asset route. Integrations cannot construct one; there is deliberately no url field.
 */
export interface Image {
  ref: string;
  alt?: ShortText;
  aspect?: string;
  blurhash?: string;
}
export interface BlockMedia {
  type: 'image' | 'image-grid' | 'poster-grid';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  columns?: number;
  empty?: ShortText;
  /**
   * @maxItems 48
   */
  items: MediaItem[];
}
export interface MediaItem {
  id?: string;
  image: Image;
  title?: ShortText;
  subtitle?: ShortText;
  badge?: ShortText;
  level?: Level;
  link?: SafeURL;
  timestamp?: string;
}
export interface BlockText {
  type: 'text' | 'markdown';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  level?: Level;
  /**
   * markdown is a restricted subset: emphasis, links, inline code, lists. Raw HTML is rejected at validation time.
   */
  content: string;
}
export interface BlockTable {
  type: 'table';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  /**
   * @minItems 1
   * @maxItems 8
   */
  columns: {
    key: string;
    label?: ShortText;
    format?: Format;
    align?: 'start' | 'center' | 'end';
  }[];
  /**
   * @maxItems 100
   */
  rows: {
    [k: string]: Scalar;
  }[];
}
/**
 * References action ids declared in configuration. A document can never express a command.
 */
export interface BlockActions {
  type: 'actions';
  title?: ShortText;
  emphasis?: 'normal' | 'strong' | 'subtle';
  /**
   * @minItems 1
   * @maxItems 6
   */
  actions: {
    id: string;
    label: ShortText;
    icon?: string;
    confirm?: boolean;
    danger?: boolean;
  }[];
}
export interface Signal {
  value: number | string | boolean | null;
  unit?:
    'count' | 'bytes' | 'bytes_per_second' | 'percent' | 'seconds' | 'celsius' | 'ratio' | 'boolean' | 'state' | 'none';
  level?: Level;
}
