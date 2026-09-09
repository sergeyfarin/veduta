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

async function loadDashboard(page: Page, colorScheme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme });
  await page.clock.setFixedTime(new Date(FROZEN_NOW));
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

test('dashboard - mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await loadDashboard(page, 'light');
  await stabilizeCanvas(page, 3154);
  await expect(page).toHaveScreenshot('dashboard-mobile.png', { fullPage: true });
});
