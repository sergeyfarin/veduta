// SPDX-License-Identifier: AGPL-3.0-or-later

import { test, expect, type Locator } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * The integrations page (Phase M4) against the REAL API: a second `veduta serve`, started here
 * with web/tests/integrations/veduta.yaml, which declares one integration in every state the page
 * distinguishes. The visual suite's --fixtures server has no configuration store, so it cannot
 * serve /api/v1/integrations at all - and a stubbed response would test the page's reading of a
 * shape it was written against, which is the one thing worth not assuming.
 *
 * The binary is the one playwright.config.ts's webServer has just built, so both suites test the
 * same build.
 */
const port = 4191;
const base = `http://127.0.0.1:${port}`;
const binary = fileURLToPath(new URL('../../veduta', import.meta.url));
const config = fileURLToPath(new URL('./integrations/veduta.yaml', import.meta.url));

let server: ChildProcess | undefined;
let dataDir = '';

test.beforeAll(async () => {
  dataDir = mkdtempSync(join(tmpdir(), 'veduta-integrations-'));
  server = spawn(binary, ['serve', '--config', config, '--listen', `127.0.0.1:${port}`, '--data-dir', dataDir], {
    stdio: ['ignore', 'inherit', 'inherit']
  });
  for (let i = 0; i < 150; i++) {
    try {
      if ((await fetch(`${base}/api/v1/health`)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error('veduta serve did not become healthy on ' + base);
});

test.afterAll(() => {
  server?.kill();
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

const card = (page: import('@playwright/test').Page, id: string): Locator =>
  page.locator(`article[data-integration="${id}"]`);

test('shows every integration’s authority from the real API, and offers no way to grant it', async ({ page }) => {
  await page.goto(`${base}/#integrations`);
  await expect(page.getByRole('heading', { name: 'Integrations', level: 2 })).toBeVisible();
  await expect(page.locator('article[data-integration]')).toHaveCount(5);

  // Approved and unchanged: what it may reach, and nothing to do.
  const glances = card(page, 'glances');
  await expect(glances.getByText('Approved', { exact: true })).toBeVisible();
  await expect(glances.getByText('GET /api/4/cpu', { exact: true })).toBeVisible();
  await expect(glances.getByText('To review and approve')).toHaveCount(0);

  // Approved with a route left out on purpose. The digest matches, so a page reading only the
  // status would call this done; the route must be listed as not granted instead.
  const immich = card(page, 'immich');
  await expect(immich.getByText('Approved', { exact: true })).toBeVisible();
  await expect(immich.locator('[data-kind="not-granted"]')).toContainText('GET /api/server/statistics');
  await expect(immich.getByText('veduta integration approve --config <your veduta.yaml> immich', { exact: false })).toBeVisible();
  await expect(immich.getByText(/Sends a request body on POST \/api\/search\/metadata/)).toBeVisible();

  // Changed: refused by the core until re-approved, so its old approval is not "granted".
  const ha = card(page, 'homeassistant');
  await expect(ha.getByText('Changed', { exact: true })).toBeVisible();
  await expect(ha.getByRole('heading', { name: 'Granted' })).toHaveCount(0);
  await expect(ha.locator('[data-kind="not-granted"]')).toContainText('GET /api/states/*');

  // Never approved: everything it asks for is pending.
  const arcane = card(page, 'arcane');
  await expect(arcane.getByText('Not approved', { exact: true })).toBeVisible();
  await expect(arcane.locator('[data-kind="not-granted"]')).toContainText('GET /api/environments/*/containers');

  await expect(card(page, 'http-json').getByText('Built in', { exact: true })).toBeVisible();

  // Read-only by construction, not by a disabled button someone could re-enable.
  await expect(page.locator('section.integrations').locator('button, form, input')).toHaveCount(0);
});

test('the header link moves between the dashboard and the integrations page', async ({ page }) => {
  await page.goto(`${base}/`);
  await page.getByRole('link', { name: 'Integrations' }).click();
  await expect(page).toHaveURL(/#integrations$/);
  await expect(page.getByRole('heading', { name: 'Integrations', level: 2 })).toBeVisible();
  await page.getByRole('link', { name: 'Dashboard' }).click();
  await expect(page).toHaveURL(/#dashboard$/);
  await expect(page.getByRole('heading', { name: 'Integrations', level: 2 })).toHaveCount(0);
});
