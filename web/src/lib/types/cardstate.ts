/**
 * GENERATED FILE - do not edit by hand.
 *
 * Produced by web/scripts/gen-types.mjs from schemas/card-state.v1.schema.json.
 * Run `pnpm gen-types` to regenerate. CI fails if this file would change.
 */

import type { WidgetDocument } from './widget';

export interface Source {
  integration?: string;
  integrationVersion?: string;
  runtime?: 'builtin' | 'declarative' | 'wasm';
  operation?: string;
  slots?: {
    [k: string]: string;
  };
}

export interface RunError {
  code: 'upstream' | 'auth' | 'timeout' | 'config' | 'denied' | 'invalid' | 'limit' | 'internal';
  message: string;
  retryable?: boolean;
  at?: string;
}

export interface ExecutionPending {
  state: 'pending';
  ttlSeconds?: number;
  durationMs?: number;
  consecutiveFailures?: number;
  nextRunAt?: string;
  source?: Source;
  error?: RunError | null;
  disabledReason?: 'unapproved' | 'permissions-changed' | 'integration-missing' | 'config-error' | 'operator';
}

export interface CardStatePending {
  cardId: string;
  document?: null;
  execution: ExecutionPending;
}

export interface ExecutionOk {
  state: 'ok';
  generatedAt: string;
  ttlSeconds: number;
  expiresAt: string;
  durationMs?: number;
  consecutiveFailures?: number;
  nextRunAt?: string;
  source?: Source;
  error?: RunError | null;
}

export interface CardStateOk {
  cardId: string;
  document: WidgetDocument;
  execution: ExecutionOk;
}

export interface ExecutionStale {
  state: 'stale';
  generatedAt: string;
  ttlSeconds?: number;
  expiresAt?: string;
  staleSince: string;
  durationMs?: number;
  consecutiveFailures?: number;
  nextRunAt?: string;
  source?: Source;
  error?: RunError | null;
  circuitOpenUntil?: string;
}

export interface CardStateStale {
  cardId: string;
  document: WidgetDocument;
  execution: ExecutionStale;
}

export interface ExecutionError {
  state: 'error';
  generatedAt?: string;
  ttlSeconds?: number;
  expiresAt?: string;
  staleSince?: string;
  durationMs?: number;
  consecutiveFailures?: number;
  nextRunAt?: string;
  source?: Source;
  error: RunError | null;
  circuitOpenUntil?: string;
}

export interface CardStateError {
  cardId: string;
  document?: null;
  execution: ExecutionError;
}

export interface ExecutionDisabled {
  state: 'disabled';
  generatedAt?: string;
  ttlSeconds?: number;
  expiresAt?: string;
  staleSince?: string;
  durationMs?: number;
  consecutiveFailures?: number;
  source?: Source;
  error?: RunError | null;
  disabledReason: 'unapproved' | 'permissions-changed' | 'integration-missing' | 'config-error' | 'operator';
}

export interface CardStateDisabled {
  cardId: string;
  document?: WidgetDocument | null;
  execution: ExecutionDisabled;
}

/**
 * What the API returns for a card. The envelope separates integration-owned content (document, including its signals) from core-owned execution facts. An integration cannot influence any field under execution.
 */
export type CardState = CardStatePending | CardStateOk | CardStateStale | CardStateError | CardStateDisabled;

export function isPending(cs: CardState): cs is CardStatePending {
  return cs.execution.state === 'pending';
}

export function isOk(cs: CardState): cs is CardStateOk {
  return cs.execution.state === 'ok';
}

export function isStale(cs: CardState): cs is CardStateStale {
  return cs.execution.state === 'stale';
}

export function isError(cs: CardState): cs is CardStateError {
  return cs.execution.state === 'error';
}

export function isDisabled(cs: CardState): cs is CardStateDisabled {
  return cs.execution.state === 'disabled';
}
