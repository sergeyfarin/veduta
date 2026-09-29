// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import IntegrationsPage from './IntegrationsPage.svelte';
import { describe as view } from './integrations';

describe('IntegrationsPage', () => {
  it('renders each integration with its status, grants and pending requests, and never an approve control', async () => {
    const views = [
      view(
        { id: 'immich', builtin: false, status: 'approved', version: '0.3.0' },
        {
          manifestSha256: 'a'.repeat(64),
          currentLock: {
            manifestSha256: 'a'.repeat(64),
            approvedAt: '2026-09-16T00:00:00Z',
            capabilities: ['http'],
            routes: [{ slot: 'server', method: 'GET', path: '/api/assets/statistics', reason: 'Counts' }],
            effectiveLimits: { timeoutMs: 5000 }
          },
          diff: {
            addedRoutes: [{ slot: 'server', method: 'GET', path: '/api/server/statistics' }],
            removedRoutes: null,
            addedCapabilities: null,
            raisedLimits: null,
            bodyBearingRoutes: null
          }
        }
      ),
      view({ id: 'http-json', builtin: true, status: 'builtin' })
    ];
    const { container } = render(IntegrationsPage, { props: { load: () => Promise.resolve(views) } });
    expect(await screen.findByText('immich 0.3.0')).toBeInTheDocument();
    expect(screen.getByText('GET /api/assets/statistics')).toBeInTheDocument();
    expect(screen.getByText('Requested, not approved')).toBeInTheDocument();
    expect(screen.getByText('GET /api/server/statistics')).toBeInTheDocument();
    expect(screen.getByText(/veduta integration approve --config <your veduta.yaml> immich/)).toBeInTheDocument();
    expect(screen.getByText('Built in')).toBeInTheDocument();
    expect(container.querySelector('button, form, input')).toBeNull();
  });

  it('says so when loading fails, rather than showing an empty list', async () => {
    render(IntegrationsPage, { props: { load: () => Promise.reject(new Error('GET /api/v1/integrations: HTTP 401')) } });
    expect(await screen.findByRole('alert')).toHaveTextContent('HTTP 401');
  });
});
