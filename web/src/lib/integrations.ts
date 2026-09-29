// SPDX-License-Identifier: AGPL-3.0-or-later

// The integrations page's data: GET /api/v1/integrations for the list, and each integration's
// GET /api/v1/integrations/{id}/approval for what it asks for against what it was granted
// (internal/api/integrations.go). The page shows authority and never grants it - approval stays
// at the CLI - so nothing here writes.

export interface Route {
  slot: string;
  method: string;
  path: string;
  use?: 'data' | 'asset';
  reason?: string;
  queryKeys?: string[];
  contentType?: string;
  maxBodyKB?: number;
}

export interface IntegrationSummary {
  id: string;
  builtin: boolean;
  status?: 'builtin' | 'approved' | 'unapproved' | 'changed';
  runtime?: string;
  version?: string;
  capabilities?: string[];
  error?: string;
}

export interface LockEntry {
  manifestSha256: string;
  version?: string;
  runtime?: string;
  approvedAt: string;
  approvedBy?: string;
  capabilities: string[];
  routes: Route[];
  effectiveLimits: Record<string, number>;
}

export interface ApprovalPreview {
  manifestSha256: string;
  currentLock: LockEntry | null;
  diff: {
    addedRoutes: Route[] | null;
    removedRoutes: Route[] | null;
    addedCapabilities: string[] | null;
    raisedLimits: { field: string; current: number; requested: number }[] | null;
    bodyBearingRoutes: Route[] | null;
  };
}

/** What the page says about one integration, derived from the two responses and nothing else. */
export interface IntegrationView {
  id: string;
  title: string;
  statusLabel: string;
  level: 'ok' | 'warn' | 'error' | 'muted';
  /** One sentence saying what the status means for the cards that use it. */
  meaning: string;
  /** Routes the integration can use now. Empty unless its lock entry is in force. */
  granted: Route[];
  capabilities: string[];
  limits: [string, number][];
  /** Requested by the manifest and not in the lock: never omitted, even when the rest matches. */
  notGranted: Route[];
  /** In the lock and no longer requested. */
  noLongerRequested: Route[];
  newCapabilities: string[];
  raisedLimits: { field: string; current: number; requested: number }[];
  /** An integration never approved has nothing to raise limits above; these are what it asks for. */
  requestedLimits: [string, number][];
  bodyBearing: Route[];
  /** Present when an operator has something to do at the command line. */
  commands?: string[];
  error?: string;
}

const STATUS: Record<string, { label: string; level: IntegrationView['level']; meaning: string }> = {
  approved: {
    label: 'Approved',
    level: 'ok',
    meaning: 'Its cards run with the routes, capabilities and limits below, and nothing else.'
  },
  unapproved: {
    label: 'Not approved',
    level: 'warn',
    meaning: 'Its cards are disabled until it is approved. It has never been granted anything.'
  },
  changed: {
    label: 'Changed',
    level: 'warn',
    meaning:
      'Its manifest no longer matches what was approved, so its cards are disabled until it is approved again. The earlier approval grants nothing in the meantime.'
  },
  builtin: {
    label: 'Built in',
    level: 'muted',
    meaning:
      'Compiled into Veduta, with no separate trust boundary to approve. Its reach is what each card names in the configuration.'
  }
};

const list = <T>(v: T[] | null | undefined): T[] => v ?? [];

export function describe(summary: IntegrationSummary, preview?: ApprovalPreview): IntegrationView {
  const title = summary.version ? `${summary.id} ${summary.version}` : summary.id;
  const base = {
    id: summary.id,
    title,
    granted: [],
    capabilities: [],
    limits: [],
    notGranted: [],
    noLongerRequested: [],
    newCapabilities: [],
    raisedLimits: [],
    requestedLimits: [],
    bodyBearing: []
  };
  if (summary.error) {
    return {
      ...base,
      statusLabel: 'Cannot load',
      level: 'error',
      meaning: 'Its manifest could not be read, so its cards cannot run.',
      error: summary.error
    };
  }
  const status = STATUS[summary.status ?? ''] ?? {
    label: summary.status ?? 'Unknown',
    level: 'muted' as const,
    meaning: ''
  };
  const view: IntegrationView = { ...base, statusLabel: status.label, level: status.level, meaning: status.meaning };
  if (summary.builtin || !preview) return view;

  const lock = preview.currentLock;
  // Only an entry whose digest still matches is in force. A changed integration's old entry is
  // history, and listing it under "granted" would claim authority the core is refusing.
  if (lock && summary.status === 'approved') {
    view.granted = lock.routes;
    view.capabilities = lock.capabilities;
    view.limits = Object.entries(lock.effectiveLimits).sort(([a], [b]) => a.localeCompare(b));
  }
  view.notGranted = list(preview.diff.addedRoutes);
  view.noLongerRequested = list(preview.diff.removedRoutes);
  view.newCapabilities = list(preview.diff.addedCapabilities);
  // With no lock entry the diff compares every limit against zero, so "raised" would list all
  // fourteen as 0 -> n. That is true and useless; show what is requested instead.
  if (lock) {
    view.raisedLimits = list(preview.diff.raisedLimits);
  } else {
    view.requestedLimits = list(preview.diff.raisedLimits)
      .map((l): [string, number] => [l.field, l.requested])
      .sort(([a], [b]) => a.localeCompare(b));
  }
  view.bodyBearing = list(preview.diff.bodyBearingRoutes);

  const pending =
    summary.status !== 'approved' ||
    view.notGranted.length +
      view.noLongerRequested.length +
      view.newCapabilities.length +
      view.raisedLimits.length +
      view.requestedLimits.length >
      0;
  if (pending) {
    view.commands = [
      `veduta integration diff --config <your veduta.yaml> ${summary.id}`,
      `veduta integration approve --config <your veduta.yaml> ${summary.id}`
    ];
  }
  return view;
}

/** A route as the approver reads it: method, path, and every constraint that narrows it. */
export function routeText(r: Route): string {
  const parts = [`${r.method} ${r.path}`];
  if (r.use === 'asset') parts.push('(images)');
  if (r.queryKeys) parts.push(r.queryKeys.length ? `query: ${r.queryKeys.join(', ')}` : 'no query');
  if (r.contentType) parts.push(`body: ${r.contentType}`);
  if (r.maxBodyKB) parts.push(`≤ ${r.maxBodyKB} KB`);
  return parts.join(' · ');
}

async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`GET ${url}: HTTP ${r.status}`);
  return (await r.json()) as T;
}

/** Loads every integration and, for each one with a manifest, its approval preview. */
export async function loadIntegrations(): Promise<IntegrationView[]> {
  const summaries = await getJSON<IntegrationSummary[]>('/api/v1/integrations');
  return Promise.all(
    summaries.map(async (s) => {
      if (s.builtin || s.error) return describe(s);
      try {
        return describe(s, await getJSON<ApprovalPreview>(`/api/v1/integrations/${encodeURIComponent(s.id)}/approval`));
      } catch (err) {
        return describe({ ...s, error: err instanceof Error ? err.message : String(err) });
      }
    })
  );
}
