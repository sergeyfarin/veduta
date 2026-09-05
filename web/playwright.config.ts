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
  // Built here, not assumed: running the suite without building first would test whatever was
  // last built, silently, which is exactly the flakiness C14 warns about. --fixtures serves the
  // checked-in showcase (internal/fixtures) - no network, no real integration, nothing that can
  // drift between two runs except the clock, which each test freezes itself.
  webServer: {
    command:
      (process.env.PLAYWRIGHT_BUILT ? '' : 'cd .. && pnpm build && cd web && ') +
      `../veduta serve --fixtures --listen 127.0.0.1:${port}`,
    url: `${address}/api/v1/health`,
    reuseExistingServer: false,
    timeout: 120_000
  }
});
