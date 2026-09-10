// SPDX-License-Identifier: AGPL-3.0-or-later

import { test, expect, type Page } from '@playwright/test';

/**
 * Visual regression baseline (milestone B5). Runs against --fixtures' checked-in showcase, never
 * a real deployment - see playwright.config.ts and docs/00-review-and-prior-art.md C14.
 *
 * The clock is frozen to a fixed instant chosen to sit just after the showcase's stale/error
 * timestamps, so formatRelativeTime's output ("7 minutes ago", "in 40 seconds") is the same
 * string every run, on any day, rather than drifting with the real wall clock.
 */
const FROZEN_NOW = '2026-09-04T21:42:00Z';

async function loadDashboard(
  page: Page,
  colorScheme: 'light' | 'dark',
  appearance: 'clean' | 'veil' = 'clean'
) {
  await page.emulateMedia({ colorScheme });
  await page.clock.setFixedTime(new Date(FROZEN_NOW));
  // The preset is instance configuration, and --fixtures serves one fixed showcase. Rewriting the
  // real GET /dashboard body is deliberately not the same as setting data-appearance by hand: it
  // drives the actual applyAppearance path the browser takes in production, so this baseline
  // fails if that wiring breaks, not only if the CSS changes.
  if (appearance !== 'clean') {
    await page.route('**/api/v1/dashboard', async (route) => {
      const response = await route.fetch();
      await route.fulfill({ json: { ...(await response.json()), appearance } });
    });
  }
  await page.goto('/');
  // The dashboard's own last card (disks) is the one furthest down the page; waiting for it
  // present means every section above it has rendered too.
  await page.getByRole('heading', { name: 'Disks' }).waitFor();
  await page.waitForLoadState('networkidle');
}

async function stabilizeCanvas(page: Page, minimumHeight: number) {
  // GitHub's container and a fresh local instance of the identical pinned Playwright image can
  // round the auth banner's fractional line box one pixel apart. Playwright rejects unequal image
  // dimensions before applying maxDiffPixelRatio, so pin the baseline canvas while leaving all
  // component pixels under comparison. Content growing beyond this height still grows the image.
  await page.addStyleTag({ content: `body { min-height: ${minimumHeight}px }` });
}

test('dashboard - light', async ({ page }) => {
  await loadDashboard(page, 'light');
  await stabilizeCanvas(page, 1407);
  await expect(page).toHaveScreenshot('dashboard-light.png', { fullPage: true });
});

test('dashboard - dark', async ({ page }) => {
  await loadDashboard(page, 'dark');
  await stabilizeCanvas(page, 1407);
  await expect(page).toHaveScreenshot('dashboard-dark.png', { fullPage: true });
});

// Veil, the translucent preset, in dark - the pairing the README shows beside Clean-light. Its
// contrast is proven arithmetically by TestVeilContrast; this baseline covers what arithmetic
// cannot: that the blur, the backdrop gradient and the card edges actually render.
test('dashboard - veil dark', async ({ page }) => {
  await loadDashboard(page, 'dark', 'veil');
  await stabilizeCanvas(page, 1407);
  await expect(page).toHaveScreenshot('dashboard-veil-dark.png', { fullPage: true });
});

test('dashboard - mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await loadDashboard(page, 'light');
  await stabilizeCanvas(page, 3154);
  await expect(page).toHaveScreenshot('dashboard-mobile.png', { fullPage: true });
});
