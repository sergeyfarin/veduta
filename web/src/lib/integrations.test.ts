// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe as suite, it, expect } from 'vitest';
import { describe, routeText, type ApprovalPreview, type IntegrationSummary, type LockEntry } from './integrations';

const lock: LockEntry = {
  manifestSha256: 'a'.repeat(64),
  approvedAt: '2026-09-16T00:00:00Z',
  capabilities: ['http', 'assets'],
  routes: [{ slot: 'server', method: 'GET', path: '/api/assets/statistics' }],
  effectiveLimits: { timeoutMs: 5000, httpRequests: 2 }
};
const emptyDiff: ApprovalPreview['diff'] = {
  addedRoutes: null,
  removedRoutes: null,
  addedCapabilities: null,
  raisedLimits: null,
  bodyBearingRoutes: null
};
const summary = (status: IntegrationSummary['status']): IntegrationSummary => ({
  id: 'immich',
  builtin: false,
  status,
  version: '0.3.0',
  runtime: 'declarative'
});

suite('integration view', () => {
  it('lists what an approved integration is granted, and nothing to do', () => {
    const v = describe(summary('approved'), { manifestSha256: lock.manifestSha256, currentLock: lock, diff: emptyDiff });
    expect(v.statusLabel).toBe('Approved');
    expect(v.granted).toHaveLength(1);
    expect(v.capabilities).toEqual(['http', 'assets']);
    expect(v.limits).toEqual([
      ['httpRequests', 2],
      ['timeoutMs', 5000]
    ]);
    expect(v.commands).toBeUndefined();
  });

  // The plan's acceptance criterion: a route the manifest requests and the lock does not grant is
  // shown as not granted, never left out because the digest happens to match.
  it('shows a requested route the approval left out, with the command that would grant it', () => {
    const extra = { slot: 'server', method: 'GET', path: '/api/server/statistics', reason: 'admin only' };
    const v = describe(summary('approved'), {
      manifestSha256: lock.manifestSha256,
      currentLock: lock,
      diff: { ...emptyDiff, addedRoutes: [extra] }
    });
    expect(v.notGranted).toEqual([extra]);
    expect(v.granted).toHaveLength(1);
    expect(v.commands?.[1]).toBe('veduta integration approve --config <your veduta.yaml> immich');
  });

  // A changed integration is refused at load. Its old entry is history; showing it under
  // "granted" would claim authority the core is not giving it.
  it('does not present a changed integration’s old approval as granted', () => {
    const v = describe(summary('changed'), {
      manifestSha256: 'b'.repeat(64),
      currentLock: lock,
      diff: { ...emptyDiff, raisedLimits: [{ field: 'timeoutMs', current: 5000, requested: 8000 }] }
    });
    expect(v.statusLabel).toBe('Changed');
    expect(v.granted).toEqual([]);
    expect(v.capabilities).toEqual([]);
    expect(v.raisedLimits).toEqual([{ field: 'timeoutMs', current: 5000, requested: 8000 }]);
    expect(v.commands).toBeDefined();
  });

  it('shows everything an unapproved integration asks for as not granted', () => {
    const routes = [{ slot: 'server', method: 'GET', path: '/api/environments/*/containers' }];
    const v = describe(summary('unapproved'), {
      manifestSha256: lock.manifestSha256,
      currentLock: null,
      diff: {
        ...emptyDiff,
        addedRoutes: routes,
        addedCapabilities: ['http'],
        raisedLimits: [
          { field: 'timeoutMs', current: 0, requested: 6000 },
          { field: 'httpRequests', current: 0, requested: 1 }
        ]
      }
    });
    // Nothing was ever approved, so these are requests, not raises from zero.
    expect(v.raisedLimits).toEqual([]);
    expect(v.requestedLimits).toEqual([
      ['httpRequests', 1],
      ['timeoutMs', 6000]
    ]);
    expect(v.granted).toEqual([]);
    expect(v.notGranted).toEqual(routes);
    expect(v.newCapabilities).toEqual(['http']);
    expect(v.level).toBe('warn');
  });

  it('says a builtin needs no approval and a broken manifest cannot run', () => {
    expect(describe({ id: 'http-json', builtin: true, status: 'builtin' }).commands).toBeUndefined();
    const broken = describe({ id: 'x', builtin: false, error: 'manifest.yaml: no such file' });
    expect(broken.level).toBe('error');
    expect(broken.error).toContain('no such file');
  });

  it('writes a route with every constraint that narrows it', () => {
    expect(routeText({ slot: 's', method: 'GET', path: '/api/memories', queryKeys: ['for', 'type'] })).toBe(
      'GET /api/memories · query: for, type'
    );
    expect(routeText({ slot: 's', method: 'GET', path: '/x', queryKeys: [] })).toBe('GET /x · no query');
    expect(
      routeText({ slot: 's', method: 'POST', path: '/api/search', contentType: 'application/json', maxBodyKB: 8 })
    ).toBe('POST /api/search · body: application/json · ≤ 8 KB');
    expect(routeText({ slot: 's', method: 'GET', path: '/a/*/t', use: 'asset' })).toBe('GET /a/*/t · (images)');
  });
});
