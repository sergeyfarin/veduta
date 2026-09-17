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

**Update (B5): Veduta now depends on Playwright itself** (`@playwright/test` 1.63.0; B5 pinned
1.62.1 to match the sibling project's already-cached engine, and a later dependency sweep moved
it) —
`web/playwright.config.ts` and `web/tests/visual.spec.ts` are the real visual regression suite,
run with `pnpm --filter veduta-web test:e2e` against `veduta serve --fixtures`. The borrowed,
sibling-cached engine described above is still the right tool for a one-off ad hoc check outside
the committed suite (a quick screenshot while iterating on a component before writing or updating
a baseline) — the difference is now which one you reach for, not whether Playwright exists here.

**Regenerating the committed baselines needs the pinned Docker image, not this dev VM.** This VM's
system fonts are not the same font environment CI runs in, and the two render text with visibly
different pixels even with the identical pinned browser build - baselines made here failed in CI
on first push. Do it inside the exact image CI uses instead:

```bash
docker run --rm -v "$(pwd)":/work -w /work -e CI=true \
  mcr.microsoft.com/playwright:v1.63.0-noble bash -c '
    git config --global --add safe.directory /work
    curl -sSL https://go.dev/dl/go1.27.0.linux-amd64.tar.gz -o /tmp/go.tgz
    tar -C /usr/local -xzf /tmp/go.tgz
    export PATH=$PATH:/usr/local/go/bin
    corepack enable
    cd web && pnpm exec playwright test --update-snapshots'
```

Match the image tag to `web/package.json`'s `@playwright/test` version whenever that gets bumped -
`TestPlaywrightImageMatchesPackage` in `internal/contracts` fails if they drift, because they did:
a dependency sweep took the package to 1.63.0 and left CI's image at 1.62.1, and every visual test
then failed on a missing browser build rather than on anything about the pixels.
`docker run` writes the new PNGs as your own UID (the bind mount inherits host ownership), but any
stray files pnpm/corepack creates directly under the repo root (a `.pnpm-store/` cache, if `pnpm
install` runs there instead of inside `web/`) come out root-owned - remove those the same way,
via another `docker run ... rm -rf`, not a host-side `rm`.

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
