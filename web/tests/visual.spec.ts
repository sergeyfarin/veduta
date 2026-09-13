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
  // --fixtures serves one fixed showcase, so the preset is varied by rewriting the real
  // GET /dashboard body. That is deliberately not the same as setting data-appearance by hand: it
  // drives the actual applyAppearance path the browser takes in production - including its
  // resolution of the viewer's default 'auto' against the instance's configured preset - so these
  // baselines fail if that wiring breaks, not only if the CSS changes.
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

// Veil, the translucent preset, in both colour schemes. Its contrast is proven arithmetically by
// TestVeilContrast and TestVeilImageContrast, and the calm of its backdrop by
// TestBundledBackdropTone; these baselines cover what arithmetic cannot - that the blur, the
// bundled painting and the card edges actually render.
//
// They do NOT cover whether the painting loaded: this suite compares with pixelmatch's default
// per-pixel threshold of 0.2, which is generous in exactly the low-contrast places a backdrop
// lives. Replacing Veil's backdrop outright moved 91.6% of the dark baseline's pixels and only
// 0.02% of them far enough to count, well inside the 2% budget. So the asset is asserted
// functionally, once, in the test below rather than hoped for here.
test('dashboard - veil dark', async ({ page }) => {
  await loadDashboard(page, 'dark', 'veil');
  await stabilizeCanvas(page, 1407);
  await expect(page).toHaveScreenshot('dashboard-veil-dark.png', { fullPage: true });
});

test('dashboard - veil light', async ({ page }) => {
  await loadDashboard(page, 'light', 'veil');
  await stabilizeCanvas(page, 1407);
  await expect(page).toHaveScreenshot('dashboard-veil-light.png', { fullPage: true });
});

// The screenshots above cannot see a 404 for the backdrop - see their comment - so this asserts it
// directly: the CSS names a bundled file, and the browser gets it. Not a baseline, because what is
// being checked is a request and a response, and pinning pixels to check a fetch is how a test ends
// up failing for an unrelated reason.
for (const [scheme, file] of [['light', 'canaletto'], ['dark', 'vernet']] as const) {
  test(`veil ${scheme} loads its bundled backdrop`, async ({ page }) => {
    await loadDashboard(page, scheme, 'veil');

    const url = await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue('--v-backdrop-image')
    );
    expect(url).toContain(file);

    const href = url.match(/url\((?:"|')?([^"')]+)/)?.[1];
    expect(href, `--v-backdrop-image is not a url(): ${url}`).toBeTruthy();
    const response = await page.request.get(new URL(href!, page.url()).toString());
    expect(response.status(), `${href} did not load`).toBe(200);
    expect(response.headers()['content-type']).toContain('image/');
  });
}

test('dashboard - mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await loadDashboard(page, 'light');
  await stabilizeCanvas(page, 3154);
  await expect(page).toHaveScreenshot('dashboard-mobile.png', { fullPage: true });
});
