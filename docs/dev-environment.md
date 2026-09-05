# Dev environment notes (headless VM)

This machine has no display and no system Chrome/Chromium. Two things follow from that.

## Verifying UI changes without a display

**`claude-in-chrome` (the MCP browser tool) is not usable here.** It drives a real Chrome
extension in a windowed browser; on this VM the extension has nothing to attach to and every call
times out waiting on the host client. Don't retry it — it isn't going to become reachable.

**What works: Playwright's own bundled Chromium, headless, with `--no-sandbox`.** It launches
fine as a container/CI-style headless browser independent of any display server. Confirmed here
by spinning up `pnpm dev` and screenshotting the running app directly with the `playwright-core`
package already cached under `../family-space/web/node_modules/.pnpm/` on this machine (a sibling
project uses it for its own end-to-end suite) — no install needed to *check* something ad hoc.

That sibling project's `web/playwright.config.ts` / `playwright.setup.config.ts` are worth reading
before B5 (visual regression / Playwright milestone): they already solve, on this exact VM, the
per-project-server-and-data-dir isolation, the `PLAYWRIGHT_PROJECT` env switch for running one
engine locally vs. the full matrix in CI, and building the frontend before the suite runs rather
than testing whatever was last on disk. Don't re-derive those from scratch; adapt them.

**Veduta itself does not depend on Playwright yet** — that's still milestone B5's job, deliberately
scoped there in the plan (visual regression needs a frozen fixture dashboard and a pinned browser
version, which doesn't exist before B3/B4 land). Using the sibling project's already-cached engine
for a one-off manual check, versus adding Playwright as this repo's own dependency, are different
things; only the latter is a milestone decision.

## What this caught

The very first thing screenshotted this way - the B1 card grid at a 380px mobile width - had a
real bug: the grid showed two uneven column tracks (`282px` / `34px`) instead of one full-width
column. `Grid.svelte` collapses to `grid-template-columns: 1fr` under 620px, but `Card.svelte` set
`grid-column: span 2` via an **inline** style, and a browser resolves that by inventing an implicit
extra column to satisfy the span rather than clamping it - CSS grid does not fail closed here.

Fixed by moving the span out of inline style entirely: `Card.svelte` now only sets
`--span-cols`/`--span-rows` custom properties inline, and the actual `grid-column`/`grid-row` are
set in the component's own scoped CSS, which clamps per breakpoint (`min(var(--span-cols), 2)` at
1000px, a flat `span 1` at 620px) using ordinary cascade - no `!important` needed, because the
property is no longer set inline at all. Verified with computed-style checks at 380/700/1100/1440px
(one track, two equal tracks, four equal tracks, four equal tracks; no card overflowing the
viewport at any width), not just a screenshot that looked plausible.

This is the argument for actually rendering a UI milestone rather than trusting typecheck + build +
token tests: none of those three would have caught it, because the bug is in how two components'
CSS interact at a specific viewport width, which is exactly the class of thing a contract test
cannot see.
