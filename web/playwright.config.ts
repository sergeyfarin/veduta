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
  // A fresh container's font cache renders a small fraction of glyph pixels a few shades off a
  // warm one's, even with the browser and OS image pinned identical - confirmed by regenerating
  // the baselines and immediately re-verifying inside the same mcr.microsoft.com/playwright
  // image: back-to-back runs in one container were byte-identical (0 diff), but a second, fresh
  // container instance showed a ~1% pixel difference with no code change at all. A real
  // regression is nothing like that margin - a one-line CSS change deliberately tested here moved
  // 24-36% of pixels. 2% comfortably clears the noise floor without hiding an actual change.
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.02 } },
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
