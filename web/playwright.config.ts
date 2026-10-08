// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineConfig, devices } from '@playwright/test';

// The visual regression baseline (milestone B5). One browser, one project: this suite exists to
// catch an unintended pixel change in this app's own CSS, not to test browser compatibility -
// see docs/00-review-and-prior-art.md C14 (deterministic fixture data, a frozen clock, checked-in
// local images, a pinned browser, one OS/font environment; real-service tests never gate CI).
const port = 4190;
const address = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'line',
  use: { baseURL: address, trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  // Two numbers, because pixelmatch judges a change twice: `threshold` decides whether one pixel
  // differs at all (a perceptual colour distance, 0-1), and maxDiffPixelRatio how many such pixels
  // may differ before the screenshot fails.
  //
  // maxDiffPixelRatio was chosen in B5 against font-cache noise: a fresh container of the pinned
  // mcr.microsoft.com/playwright image once rendered ~1% of glyph pixels a few shades off a warm
  // one, while a one-line CSS change deliberately tested here moved 24-36% of pixels. 2% clears
  // the first and not the second.
  //
  // `threshold` was left at pixelmatch's 0.2 until 2026-10-08, and 0.2 hides a wash across a large
  // area - exactly where a backdrop or a card surface lives. Adding the Climate card (a 2x2 card a
  // few shades off the grid behind it) counted 0.07% of the light baseline's pixels at 0.2, and all
  // four desktop baselines passed against images without it; at 0.01 it counts 6.8%. Removing the
  // card again fails all five baselines at 0.01, and only mobile (on its height) at 0.2. Replacing
  // Veil's light backdrop counted 0.002% at 0.2 and 72% at 0.01.
  //
  // The noise floor at 0.01: the baselines regenerated in four fresh containers, every pair (and
  // each against the committed baselines) compared with Playwright's own comparator - 0 pixels at
  // every threshold down to 0.01. Only at 0 did anything count, 101 pixels (0.006%) in one
  // veil-light pair. Change either number only after re-measuring both; a noise floor measured for
  // one pair does not transfer to another.
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.02, threshold: 0.01 } },
  // Built here, not assumed: running the suite without building first would test whatever was
  // last built, silently, which is exactly the flakiness C14 warns about. --fixtures serves the
  // checked-in showcase (internal/fixtures) - no network, no real integration, nothing that can
  // drift between two runs except the clock, which each test freezes itself.
  //
  // The version is pinned rather than taken from `pnpm build`, which asks git: the footer prints
  // it, so a baseline otherwise depends on whether the checkout has tags and how dirty it is. It
  // did - the committed baselines said 0.0.0-dev, CI rendered a short commit hash, a tagged local
  // clone rendered v0.1.3-5-g...-dirty - and only the pixel budget kept that from failing.
  webServer: {
    command:
      (process.env.PLAYWRIGHT_BUILT
        ? ''
        : 'cd .. && pnpm --filter veduta-web build && go build -trimpath ' +
          '-ldflags "-X veduta.dev/veduta/internal/version.Version=0.0.0-dev ' +
          '-X veduta.dev/veduta/internal/version.Commit=visual" -o veduta ./cmd/veduta && cd web && ') +
      `../veduta serve --fixtures --listen 127.0.0.1:${port}`,
    url: `${address}/api/v1/health`,
    reuseExistingServer: false,
    timeout: 120_000
  }
});
