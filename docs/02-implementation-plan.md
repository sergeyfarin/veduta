# 02 — Implementation plan

Sized for a single developer (or one coding agent) working in 0.5–2 day slices. Every issue is
independently implementable, testable and mergeable. Estimates are ideal developer-days.

---

## Part 0 — Contract freeze (0.5 d, before anything else)

The review round in [00-review-and-prior-art.md §7](00-review-and-prior-art.md) changed four
contracts that everything downstream encodes. Land them as schemas + fixtures **first**, because
each one invalidates every golden test and every integration if it changes later:

1. Route-based grants (`Grant.Routes`, manifest `routes`, `veduta.lock.yaml`).
2. `signals` as the rules/history substrate.
3. The `CardState` envelope, including the legal state-combination invariants.
4. The declarative template grammar (four explicit node kinds).
5. The route-canonicalisation contract (one routine, `*` only, three independent checks).
6. The pre-parse and aggregate manifest budgets, and the effective-limit formula.
7. The RFC 8785 canonical digest, with golden fixtures both languages reproduce.

Deliverable: the five schemas in `schemas/`, the worked Immich manifest, the lock example, and a
65-case adversarial corpus — all of which exist in this repository and pass
`go test ./internal/contracts/...` today. That script runs three layers, and CI runs all three:

| Layer | What it proves |
| --- | --- |
| structural | every schema is valid; every fixture and adversarial case lands on its expected side |
| portability | every `pattern` is compiled by **Go's own `regexp`** via `internal/contracts` — not a Python heuristic. The first draft's route pattern used `(?!.*\.\.)`, which `regexp.Compile` rejects outright, so the Go validator would have refused the schema at boot |
| canonical | `internal/canonical` must agree byte for byte on golden fixtures **and on the real YAML manifests through Go's strict decoder** (duplicate keys rejected, floats rejected, YAML scalar typing preserved). Normalisation is path-aware, with `must_match`/`must_differ` invariants proving arbitrary user data named `capabilities` does not collide |
| helpers | media-type normalisation and SemVer validity are delegated to `internal/contracts` (`mime.ParseMediaType`, real SemVer 2.0.0), not approximated in Python; the semantic layer fails closed without them |
| semantic | what neither schema nor pattern can express, using the parsers the Go loader will use: real RFC3339/URL/semver values; lock routes ⊆ manifest routes **on full route identity**; recomputed canonical manifest digests; **per-operation** signal declarations; required slots bound and slot kinds matching connection kinds; duplicate ids; and every rule's card, signal and channel reference resolved against that card's selected operation |

**Anything that must be enforced is a `pattern`, never a `format`** — `date-time` and `uri` are
annotations in both the Python and the Go validator's default mode, so relying on them would have
been enforcement theatre.

**AC:** all four layers green; **no example is skipped** (an integration referenced by the examples
with no manifest is an error, so `plugins/glances` and `plugins/jellyfin` exist as contract
fixtures); and the semantic checks are guarded by **19 checked-in negative fixtures** in
`testdata/semantic-cases/`, run through the same code as the real examples — so deleting a check
fails the suite instead of silently reducing coverage. Verified: removing the per-operation signal
check turns the build red.

---

## Part 1 — Spikes (~3 days, mostly parallel to Part 2)

Spikes are timeboxed, live in `spikes/`, and are **deleted or rewritten** before the code they
inform is merged. Each ends with a decision recorded in `docs/01-architecture.md §14`.

### S1a — wazero feasibility smoke test · 0.5 d · **early, does not block the demo**

Two hours of code, answering only: does wazero compile and run a trivial module on linux/amd64,
linux/arm64 and armv7? What are cold-compile and warm-invoke times and RSS on a Pi-class board?
How large is a hello-world module built with Rust, Go 1.24 `go:wasmexport`, TinyGo, and the Extism
js-pdk? If wazero cannot run acceptably on ARM, the entire plugin story changes and it is worth
knowing in week one — but nothing before Phase G depends on the answer.

### S1b — Sandbox + capability broker proof of concept · 1.5 d · **immediately before G1**

The full execution model, now including route enforcement:

1. A module receives `{"operation":"stats","params":{}}` as JSON.
2. It calls `veduta_http({"slot":"server","method":"GET","path":"/api/server/statistics"})`.
3. A stub broker checks capability → slot → **route** → budget, injects `x-api-key`, calls a test server.
4. The module returns a Widget Document with signals; the host validates it against the schema.
5. **Negative tests all pass:** `extism_http_request` fails; slot `"other"` fails; `DELETE` on an
   allowed *path* fails; `GET /api/albums` (allowed host, unapproved path) fails; `assets.ref` on a
   `use: data` route fails; `../../etc/passwd` fails; an infinite loop is killed by the deadline; a
   100 MB allocation traps; a 10 MB output is rejected.

**Decision produced:** Extism vs hand-rolled ABI (D6); per-call instantiation vs pooling.
**Kill criteria:** >50 ms warm invocation or >20 MB RSS per instance on ARM ⇒ reconsider a sidecar.

The original plan put this first *and* on the demo critical path while describing it as merely
"informing" the broker — an internal contradiction the review caught. With route grants now defined
by security policy rather than by sandbox ergonomics, the broker interface no longer waits on it.
S1a keeps the early warning; S1b moves next to the code it serves.

### S2 — Upstream reality check: Immich + Jellyfin · 0.5 d · **blocks real-server E3/G4
acceptance, not E1 implementation** · **spec pass DONE, live pass outstanding**

Findings and consequences: [docs/spikes/s2-upstream-reality-check.md](spikes/s2-upstream-reality-check.md).
The specification pass found two defects that would have surfaced in E3 and G4: Immich's
`/server/statistics` is admin-only, and Jellyfin 12 removed `/Users/{userId}/Items` entirely. Both
manifests are corrected. The live pass runs
[`hack/capture-upstream-fixtures.sh`](../hack/capture-upstream-fixtures.sh) against real servers to
answer what a specification cannot — chiefly the actual `Content-Type` of an Immich thumbnail,
which originally appeared to decide whether the asset proxy could compare headers at all.
**Corrected during E1 implementation:** architecture §7 already says content type is sniffed and
not trusted. E1 therefore uses bytes plus `image.DecodeConfig` exclusively and safely rejects a
lying or absent upstream header. The live pass remains essential compatibility/latency validation,
but is not a sound reason to block the security implementation.

Original scope — against real servers, capture as `testdata/`: Immich `/api/server/statistics`, the metadata search
payload, and the exact thumbnail endpoint + auth mechanism; Jellyfin's `X-Emby-Authorization`
format, `/Users/{id}/Items?SortBy=DateCreated`, and `/Items/{id}/Images/Primary`. Record pagination,
whether image endpoints accept the same auth as JSON endpoints, response sizes, redirects — **and
the exact route set each integration needs**, which is now manifest content, not an implementation
detail. Prevents discovering in E3 that Immich thumbnails need a session cookie.

### S3 — Expression and validation bake-off · 0.5 d · **blocks C1, D3, J2** · **DONE**

Two separable questions, resolved separately (C1 already answered the second one while building
ahead of this spike, see C1's own entry): `santhosh-tekuri/jsonschema/v6` errors do combine with
`yaml.v3` Node positions into a usable `file:line:col` (confirmed directly in C1, not assumed).

**Which expression language, resolved without the originally-planned dual-library benchmark.**
The plan's own reasoning for that benchmark was that `expr`'s context-aware mode "instruments loops
with cancellation checks" and a spike was needed to prove that empirically — checked directly
against `expr`'s source and docs instead: `expr.WithContext` propagates cancellation only to
context-aware **custom functions**, never to `expr`'s own built-in `map`/`filter`/`sortBy`/etc., so
neither candidate offers free interruption for those operators and a benchmark comparing "which
library interrupts a hostile loop automatically" would have measured a property neither library
actually has. That reframes the decision: CEL's real advantage isn't "battle-tested," it's that its
interpreter accounts for its own comprehensions — a structural property, not something a benchmark
against three sample mappings would surface differently than reading the two interpreters' designs.
Confirmed separately (also without needing the benchmark to run): `expr.DisableBuiltin`/
`DisableAllBuiltins` remove a builtin from name resolution before compilation, so an
environment-supplied function of the same name resolves in its place - the mechanism D3 needs to
own the sandbox itself rather than trust `expr`'s.

**Decision:** `expr-lang/expr`, for both D3 and J2, used as a parser/evaluator only - D3 owns the
entire execution budget (deadline, iteration budget, output-size/depth budget, AST complexity
limit) and every scalable collection builtin is disabled and replaced by a D3-owned charged
implementation of the same name. Ergonomics is the deciding factor, not safety: D3's manifest DSL is
templating-shaped (`map`/`filter`/`sortBy`/`take`/string/date helpers), which is what `expr` already
looks like natively - CEL optimises for boolean policy predicates and would push a manifest-DSL
redesign around CEL's macro model, not a mere library swap, for a use (J2's `signal(...) > 90`
predicates) where either language is equally trivial. Full rationale, the corrected claim about
`WithContext`, and the explicit supported-function-list design: docs/01-architecture.md D47 and §5's
"expr is a parser and evaluator; D3 is the sandbox." **Decision produced:** D7, D47.

### S4 — Visual prototype · 1 d · **blocks B1** · **DONE**

Delivered: [docs/spikes/s4-visual-prototype.html](spikes/s4-visual-prototype.html), with the
decisions it settles written up in [s4-visual-prototype.md](spikes/s4-visual-prototype.md).
24 tokens, 8 block renderings, all four card states, both themes, no per-card CSS and no external
requests. Notable outcomes for B1: status is never colour alone; metrics use tabular numerals;
aspect ratios come from the block so nothing reflows when an image lands; container queries rather
than media queries inside cards.

---

## Part 2 — Milestones and backlog

Legend: **⇉** can run in parallel with its siblings · **→** strictly serial.

### Phase A — Skeleton (2 d)

**A1 · Repository bootstrap** · 0.5 d · deps: none · **DONE**
Objective: a repo that builds, lints and tests in CI on day one.
Creates: `go.mod` (Go 1.27), task runner (mise + pnpm scripts rather than a Makefile, since the
dev loop spans both halves), `.golangci.yml`, `.github/workflows/ci.yml`,
`cmd/veduta/main.go`, `internal/version/`, `LICENSE` (AGPL-3.0 + the §7 plugin exception), `sdk/LICENSE` and `schemas/LICENSE` (Apache-2.0), `LICENSING.md`, SPDX headers, `CONTRIBUTING.md` with DCO, `.editorconfig`.
Tests: `make check` runs `go vet`, `golangci-lint`, `go test ./...`, `gofmt -l` (must be empty).
Licensing is enforced by tests rather than an external tool: `TestSourceFilesCarrySPDXHeaders`
checks every Go file's header against the per-directory licence, and `TestDependenciesAreRecorded`
fails when a module in `go.mod` is missing from `THIRD-PARTY-LICENSES.md` or when a recorded
licence is GPL-2.0-only, SSPL, BUSL or unlicensed. Both are mutation-tested.
AC: CI green on a clean clone; `go build ./cmd/veduta` produces a binary that prints its version;
CI matrix builds `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `darwin/arm64`.

**A2 · Svelte SPA + embedding** · 1 d · deps: A1 · **DONE**
Objective: `veduta` serves the frontend from the binary.
Creates: `web/` (Svelte 5 + Vite + TS, no SvelteKit), `web/embed.go` + `internal/api/static.go`
with `embed.FS`, `web/vite.config.ts` with a dev proxy to `:8099`, and root `pnpm dev` / `pnpm build`
covering both halves. `web/build/.gitkeep` is committed because `//go:embed` fails to compile with
nothing to match; a binary built without the frontend answers 503 with the command to fix it rather
than serving a blank page.
Contracts: static handler serves `index.html` for unknown non-`/api` paths (SPA fallback), sets
`Cache-Control: immutable` for hashed assets and `no-cache` for `index.html`.
Tests: `GET /` returns HTML with `Cache-Control: no-cache` and the strict CSP; `GET /assets/*.js`
returns JavaScript with `immutable`; deep links fall back to the SPA; **`/api/*` is never shadowed
by the fallback** (a typo in an endpoint must 404, not return HTML with a 200); path traversal
cannot escape the embedded filesystem; a binary with no embedded build says so.
AC met: a 6.7 MB `veduta` binary serves the SPA, its hashed assets and the API with no Node at
runtime, and cross-compiles for linux/amd64, linux/arm64, linux/armv7 and darwin/arm64 in CI.

**A3 · Server foundation** · 0.5 d · deps: A1 · ⇉ with A2 — **partially landed**: `internal/api`
serves `/api/v1/health` and `/api/v1/version`, with timeouts, panic recovery, structured logging,
graceful shutdown and the loopback gate below. Request-id middleware and config flags remain.
**Gate:** the listener defaults to `127.0.0.1:8099` and **refuses to bind a non-loopback address**
until H1 has landed and `auth` is configured with a mode other than `none`. The dashboard holds
service credentials from Phase D onward, and Phase H used to sit after the container image and the
real-Immich demo — that ordering shipped a credential-bearing service on all interfaces with no
authentication, so the bind refusal is the interim gate and H1 moves earlier (see below).
Objective: production-shaped HTTP server.
Creates: `internal/api/server.go` (stdlib router, timeouts, max header/body bytes), structured
logging (`log/slog`, JSON in production, text in dev), graceful shutdown, panic-recovery middleware,
request-id middleware, `GET /api/v1/health`, `GET /api/v1/version` (build info + AGPL §13 source URL for the built commit), config flags/env
(`--config`, `--data-dir`, `--listen`).
Tests: shutdown drains in-flight requests; panic returns 500 and logs once; health returns 200.
AC: `SIGTERM` exits cleanly within 5 s; no request logs contain secrets.

**A4 · Container image and release build** · 0.5 d · deps: A2 · ⇉
Creates: multi-stage `Dockerfile` (node build → go build → distroless/static, non-root uid),
`compose.yaml` with the **docker-socket-proxy** default from C5, `.dockerignore`, goreleaser
config (checksums, multi-arch manifest).
AC: image < 40 MB; runs as non-root; `docker compose up` serves the dashboard; image builds for
amd64 and arm64.

### Phase B — Renderer and design system (4 d) — the product's face

**B1 · Design tokens and layout** · 1 d · deps: A2, S4 · **DONE**
Creates: `web/src/styles/tokens.css` (24 tokens, light and dark, with the three-state theme
cascade), `web/src/lib/{Grid,Card,Section,Status,Skeleton}.svelte`, `web/src/lib/theme.ts`.
`Card` owns all four execution states, because they are core-owned facts about the run rather than
content: `stale` dims and dates its retained document instead of blanking, `error` gives the reason
and the retry, `pending` shows skeletons, `disabled` says what a human has to do.

AC met. Contrast is enforced by `TestTokenContrast`, which parses `tokens.css` and computes
WCAG 2.1 ratios for both palettes rather than trusting a browser to be present - it caught
`--v-faint` at 4.44:1 on its first run. `TestComponentsUseTokensNotHardcodedColours` enforces the S4
constraint that no component may use a colour literal.

The visual comparison against the S4 prototype was done for real: `pnpm dev` plus a headless
Playwright Chromium (borrowed ad hoc from a sibling project's cache on this VM, which has no
display and no system browser - see `docs/dev-environment.md`), screenshotted in light, dark and
at 380/700/1100/1440px. It caught a genuine bug rather than confirming a clean pass: at 380px the
grid produced two uneven column tracks instead of one, because `Card.svelte` set `grid-column: span
2` via **inline** style while `Grid.svelte`'s own mobile breakpoint collapsed to a single explicit
column - a browser resolves that mismatch by inventing an implicit extra column, not by clamping
the span. Fixed by moving the span out of inline style into the component's own scoped CSS (driven
by `--span-cols`/`--span-rows` custom properties), which clamps per breakpoint using ordinary
cascade. Verified after the fix with computed-style checks at all four widths, not just a
screenshot that looked plausible: one track, two equal tracks, four equal tracks, four equal
tracks, no card overflowing the viewport at any width. This is the argument for actually rendering
a milestone that touches CSS interaction between components, rather than trusting
typecheck+build+token-tests alone - none of those three would have seen it.

**B2 · Widget Document, signals, and the CardState envelope** · 1.5 d · deps: Part 0 · ⇉ with B1 · **DONE**
Creates: `internal/widgets/{document,blocks,validate}.go` (typed structs, a closed `Block`
interface with nine concrete types and discriminated marshal/unmarshal), `schemas/embed.go` (the
schemas ship inside the binary, like `web/embed.go`), `internal/state/cardstate.go` (the envelope:
`Execution` has no constructor but the five named ones - `Pending`/`OK`/`Stale`/
`StaleWithOpenCircuit`/`Error`/`ErrorWithOpenCircuit`/`Disabled` - each producing exactly one legal
`(state, field)` combination from the schema's conditional validation, so an invalid combination
cannot be assembled by calling code), `web/scripts/gen-types.mjs` and its output,
`web/src/lib/types/{widget,cardstate}.ts` **generated**, wired into `pnpm gen-types` and a CI step
that regenerates and runs `git diff --exit-code` against them.

Contracts: `widgets.Validate(raw []byte) (Document, error)` — the single gate every runtime passes
through; `state.CardState` — the single shape every API and SSE response returns.

Tests: the widget-schema slice of Part 0's adversarial corpus runs as a Go table test (16 cases,
filtered by `schema=="widget"` from the shared `testdata/schema-cases.json` rather than a second
copy); the golden fixture round-trips; hard limits beyond what JSON Schema itself can express -
total document size and raw HTML inside markdown - are each mutation-tested (disabling the check
makes its test fail); `Validate` is fuzzed for panics. On the envelope side: all seven constructors
validated against **both** the real compiled JSON Schema (including its `allOf`/if-then branches)
and a Go-level `CardState.Validate()` self-check; nine illegal combinations built as raw struct
literals (bypassing every constructor) are each rejected by `Validate()`; a source-scanning test
proves nothing in `internal/state`'s non-test source ever calls `Unmarshal`/`Decode` into an
`Execution` - the ONLY route from untrusted bytes to program state is `widgets.Validate`, which
returns a `widgets.Document`, never a `state.CardState` - and that scanner is itself
mutation-tested (a planted call makes it fail).

Real bugs the schema round-trip caught, worth recording because they are exactly the class of bug
this milestone's cross-checking exists to catch:
- `Document` had no `MarshalJSON`. It decoded fine (`UnmarshalJSON` reads and checks
  `schemaVersion`), but re-encoding silently dropped the field, so every constructed `CardState`
  failed schema validation - not because the envelope was wrong, but because its embedded document
  was missing a required property on the way back out. Fixed and pinned with a dedicated
  marshal-unmarshal-marshal round-trip test.
- The first version of the 64 KiB document-size test built 200 small blocks and never actually
  exercised the size gate: it tripped the schema's own unrelated 12-block `maxItems` limit first.
  Rewritten as a single table block, schema-valid at every individual field (12 blocks, 100 rows,
  8 columns, 512-char cells - each exactly at its own schema maximum) that is still ~410 KB in
  total, isolating the byte-count check from the field-level limits already covered by the shared
  corpus.
- `json-schema-to-typescript` compiles the envelope's `allOf`/if-then branches into a TypeScript
  **intersection** of all five state shapes, not a union keyed on the discriminant - which is not
  merely inconvenient, it silently drops the entire point of the type (no narrowing of `document`
  to non-null when `state === 'ok'`). `cardstate.ts`'s discriminated union is hand-assembled by
  `gen-types.mjs` instead, reading the same `allOf` branches programmatically rather than
  hardcoding independent knowledge of them - still generated, still caught by the CI diff, just
  not routed through the library for this one schema. Confirmed necessary (not just cosmetic) with
  an isolated TypeScript repro showing the exact same nested-discriminant narrowing failure
  outside any generated code, and confirmed the fix works by exercising real narrowing through five
  generated type-guard functions (`isOk`, `isStale`, ...) in a throwaway consumer file, both before
  (fails to compile) and after (typechecks clean) the fix.
- `json-schema-to-typescript`'s default handling of bounded arrays (`blocks` maxItems 12, table
  `rows` maxItems 100, ...) compiles to a union of every fixed-length tuple up to the max -
  technically faithful, unusable as a type. Set `ignoreMinAndMaxItems: true`; the length limits are
  enforced by `widgets.Validate`, not by the TS type.

Known, accepted limitation: the hand-assembled envelope union models field *removal*
(`"x": false` in a branch) precisely but not field *narrowing-while-present* (`"error": {"type":
"null"}` on the `ok` branch), so a couple of fields are slightly more permissive in TypeScript than
the schema allows. Judged acceptable because the frontend only ever consumes a `CardState` the
backend already built via the airtight Go constructors - it never constructs one itself - so the
gap has no real exploit surface; revisit if that stops being true.

AC met: Go and TS types are provably generated from one schema, and CI fails if regeneration
diffs (verified both directions: a mutated schema produces a detected diff, restoring it produces
none); no code path lets an integration write any field under `execution` (proven structurally by
the source scanner, not merely by convention).

**B3 · Block renderers I** · 1 d · deps: B1, B2 · **DONE**
Blocks: `status` (as `StatusListBlock.svelte` - a list of named statuses, kept visually distinct
in the codebase from B1's `Status.svelte`, which renders a card's own single overall health),
`metrics`, `key-value`, `progress`, `list`, `text` (both plain and the restricted markdown subset,
one component - the schema's own `blockText` covers both `type` values).

Creates: `web/src/lib/blocks/{MetricsBlock,KeyValueBlock,ProgressBlock,StatusListBlock,ListBlock,
TextBlock,InlineMarkdown,UnknownBlock,BlockRenderer}.svelte`, `blocks/registry.ts`, `format.ts`,
`markdown.ts` (the restricted-subset parser - see below), plus the Vitest harness itself
(`vitest.config.ts`, `vitest-setup.ts`, pinned `vitest`/`@testing-library/svelte`/`jsdom`/
`@testing-library/jest-dom`) and `internal/contracts/no_html_directive_test.go` for the CI grep.
Wired a full set of real block instances into the B1 showcase page (`App.svelte`) in place of the
placeholder text, exercising all six types together - the one thing that actually found the two
bugs below.

Tests: 93 Vitest cases across `format.ts` (45, covering the stated 0/negative/huge/null edge cases
for every `Format` value), `markdown.ts` (19, with the link-scheme-safety cases mutation-tested -
disabling the href check turns 4 of them red) and one file per component (29). `format.ts`'s own
"huge" case caught two of its own test's wrong expectations before catching anything in the
implementation - worth noting because it means the edge-case requirement did its job even before
touching real code. `TestNoRawHTMLDirectiveInFrontend` (Go, in `internal/contracts`) is the CI grep
the milestone asked for, run through `go test ./...` rather than a separate shell step, and it is
itself mutation-tested (planting a real `{@html}` directive turns it red).

Two real bugs found only by actually rendering the wired-in showcase page and looking at it -
neither would have been caught by typecheck, unit tests, or the schema contracts, because both are
about how two things interact visually or across tool boundaries, which is exactly the class of
bug those layers cannot see:

- **The stale-state dimming failed WCAG AA**, discovered by screenshotting the Proxmox (stale)
  card and finding its metric labels nearly unreadable. `--v-faint` sits at 4.71:1 against
  `--v-surface` - barely above the 4.5:1 body-text floor - and Card.svelte's `.dimmed` rule
  (`opacity: 0.55` layered under `filter: saturate(0.65)`) composited that down to 2.1:1.
  Confirmed numerically before touching anything: **no** opacity below 1.0 keeps `--v-faint`
  legal, since even `opacity: 0.9` computes to 3.88:1. The fix removes opacity entirely and keeps
  only `saturate(0.4)`: verified that near-grey text tokens are ~unaffected by desaturation
  (contrast moves by hundredths) while the accent colours a stale card actually wants to look
  muted - the progress bar fill, a level-tinted value - genuinely desaturate toward grey.
  `TestStaleStateContrast` (`internal/contracts`) parses the real `.dimmed` rule out of
  `Card.svelte`, simulates the same filter-then-opacity pixel pipeline a browser applies, and
  asserts every neutral token still clears AA in both palettes - mutation-tested by reintroducing
  the exact original rule, which fails it in both themes (light 2.35:1/2.11:1, dark 3.05:1/2.49:1).
  This is the concrete answer to the "cards read too tall/thin on placeholder content" feedback
  from B1: real content was needed to judge real density and legibility, and it surfaced a
  correctness bug the placeholder text could never have shown.
- **`svelte-check` silently skipped every jest-dom matcher**, reported as "`toBeInTheDocument` does
  not exist" across every single test file including ones that had type-checked cleanly minutes
  earlier. Root cause: `vitest-setup.ts` (which imports `@testing-library/jest-dom/vitest` for its
  ambient type augmentation) was never in `tsconfig.json`'s `include` list, so the type checker's
  program never saw the augmentation at all - Vitest itself still ran the tests fine, since it
  uses its own esbuild-based transform, which is why `pnpm test` kept passing throughout while
  `pnpm check` was quietly broken. Fixed by adding `vitest.config.ts` and `vitest-setup.ts` to
  `include`.

Also fixed along the way, both confirmed with an isolated repro rather than worked around blindly:
recursive component self-reference (`InlineMarkdown.svelte` rendering nested emphasis/links)
needs an explicit `import Self from './InlineMarkdown.svelte'` in Svelte 5, bare self-reference by
tag name does not resolve; and `noUncheckedIndexedAccess` correctly flags every `RegExpMatchArray`
index in `markdown.ts` as possibly `undefined`, resolved with non-null assertions at each site
since a successful match on a hand-written fixed-capture-group regex always has those groups.

AC met: every component has Vitest coverage including the stated edge cases; `{@html}` is absent
from `web/` and a mutation-tested Go test (not a shell grep) enforces it in CI.

**B4 · Block renderers II (media)** · 1 d · deps: B3 · **DONE**
Blocks: `image`, `image-grid`, `poster-grid` (one `MediaBlock.svelte` + `MediaTile.svelte`,
switching grid density and default aspect by kind - `markdown` was already covered by B3's
`TextBlock`, since the schema's `blockText` carries both `text` and `markdown` under one type),
`table`, `actions` (rendered disabled - see below). All nine v1 block types are now registered.

Creates: `web/src/lib/blocks/{MediaBlock,MediaTile,TableBlock,ActionsBlock}.svelte`,
`web/src/lib/assets.ts` (the one place an `Image.ref` becomes a fetchable URL, and the one place
the schema's `"W:H"` aspect syntax becomes CSS `aspect-ratio` syntax).

- **Aspect-ratio boxes**: every tile reserves its box via CSS `aspect-ratio` before the image
  exists, using the item's own `image.aspect` when present, else a per-kind default (16:9 for a
  single `image`, 1:1 for `image-grid`, 2:3 for `poster-grid`, matching the S4 prototype).
- **Lazy loading**: `loading="lazy" decoding="async"` on every `<img>`.
- **Blur-up placeholder**: a shimmer skeleton (reusing B1's `Skeleton.svelte`) shown until `load`
  fires, then a plain opacity cross-fade. `Image.blurhash` exists in the schema for a true
  decoded-thumbnail placeholder; implementing the BlurHash algorithm itself (decode → pixel grid →
  canvas) is real, separable work, named here as a deliberate deferral rather than silently
  dropped.
- **Broken-image state**: an `error` handler swaps the `<img>` for a placeholder that still
  fills the exact same aspect-ratio box - the failure never collapses the layout, only the pixels
  inside it change.
- **Table overflow**: a `.scroll` container with `overflow: auto` on both axes (the schema allows
  up to 100 rows and 8 columns, either of which can exceed a card's fixed row-quantised height or
  width) plus a sticky header.
- **Actions rendered disabled**: real, labelled `<button disabled>` elements, not omitted and not
  silently enabled - executing an action needs authentication, an authorisation check and an audit
  trail on the core side (none of which exists before Phase D/H), so an inert button is honest and
  a fake-working one would not be.

Tests: 22 new Vitest cases (`MediaBlock` 10, `MediaTile` 5, `TableBlock` 5, `ActionsBlock` 3),
including the milestone's own stated cases: an item's own aspect overriding the per-kind default,
the asset URL built from `ref` alone (never a raw URL an integration could supply), the broken
placeholder reserving the identical aspect box as a real image would, and every action rendered
disabled. `BlockRenderer.test.ts`'s assertion from B3 ("media/table/actions are NOT yet
registered") now correctly fails and is updated to assert the full set of nine.

AC verified for real, not approximated: `pnpm dev` plus the same borrowed Playwright engine used
for B1/B3, this time going further than a screenshot. The asset endpoint does not exist until
milestone E1, so every image in the wired-in showcase honestly 404s right now - confirmed and
screenshotted as the correct, working broken-image path (right icon, right aspect ratio, no
crash). To verify the *successful*-load path and measure real CLS without faking the app's own
code, Playwright's `page.route()` intercepted `/api/v1/assets/**` and served a real 1×1 JPEG - a
test-side technique simulating the not-yet-built backend, not a change to any shipped code. Against
that, the browser's own Layout Instability API (`PerformanceObserver({type:'layout-shift'})`,
summed for the page's lifetime) measured **0** across all 11 images in the showcase (a 5-poster
row, a 6-photo grid, and one more), which is the literal AC metric rather than a proxy for it.

Found while looking at the honestly-404ing screenshot, not by any automated check: the caption
overlay (`MediaTile`'s hover caption, ported from the S4 prototype) used literal `#fff` and
`rgb(0 0 0 / 70%)` instead of tokens, which `TestComponentsUseTokensNotHardcodedColours` (added in
B1) correctly rejected. Fixed properly rather than exempted: two new **deliberately
theme-invariant** tokens, `--v-overlay-text`/`--v-overlay-scrim`, defined once in `:root` and never
redefined per theme - a caption sits on top of unknown, variable photo luminance, not the page
background, so it needs to stay legible against any photo in either theme, which a light/dark pair
cannot express. Still a token, still enforced by the same test; this is the one legitimate
exception to "every colour is a token," not a literal smuggled past the guard.

**B5 · Fixture dashboard + visual regression baseline** · 0.5 d · deps: B3, B4 · **DONE**
Creates: `internal/fixtures/{showcase.json,images/}` (13 deterministic CardStates, checked-in
local JPEGs, exercising all 9 block types and all 5 execution states), `internal/fixtures.Load`
(re-validates every document against the real widget schema and cross-checks every descriptor
against its card, not just decodes), the `--fixtures` dev flag on `veduta serve`
(`internal/api/fixtures.go`: `GET /dashboard`, `/cards`, `/cards/{id}`, `/assets/{token}`, gated
entirely behind `Config.Fixtures` so the surface does not exist at all in a normal binary),
`web/playwright.config.ts` + `web/tests/visual.spec.ts` (`@playwright/test` pinned to 1.62.1,
matching the borrowed engine used for verification since B1), and the checked-in baselines under
`web/tests/visual.spec.ts-snapshots/`.

Corrected from the plan while building it: the milestone text named
`testdata/dashboards/showcase.json` at the repository root, but `go:embed` cannot reach outside
its package directory (no `..`, no absolute paths) - the fixture binary needs this data compiled
in, not read from a source tree that may not exist at runtime. The data moved into
`internal/fixtures/` itself; `docs/01-architecture.md` section 9's route table is unaffected, and
`--fixtures` is the only thing that ever reads it.

`App.svelte` changed from B1-B4's hardcoded showcase literals to a real fetch-driven renderer:
`GET /dashboard` for layout, `GET /cards` for every CardState, mapped through the same `isOk` /
`isStale` / `isError` / `isDisabled` / `isPending` guards B2 generated. This is not scope creep -
the milestone's own text ("a `--fixtures` dev flag *serving* them") only makes sense if something
fetches what is served, and it is the same component Phase C's real configuration and Phase F's
real scheduler will drive once they exist; only the data source changes underneath. A card's
`document.status.{level,text}` (not a hardcoded per-card prop) now drives the head status text,
matching what the schema actually reserves that field for - "34d" and "14 running" are fixture
content, not App.svelte literals, and `formatRelativeTime` (exported from `format.ts`, previously
private) turns `staleSince`/`nextRunAt` into the stale/error notices' text.

AC verified for real: `pnpm --filter veduta-web test:e2e` run twice consecutively, both green, no
flake. For the third AC item a first attempt was a false pass worth recording - setting
`.card`'s `border-radius` to 0 did **not** fail the suite, because `--v-bg` (#fbfbf9) and
`--v-surface` (#fff) are close enough in light mode that Playwright's default perceptual
threshold (pixelmatch, not byte-identical) judged the corner pixels unchanged. A second,
unambiguous mutation - `--v-bg` to `#ff0000` - failed light and mobile immediately (0.24 of all
pixels differing) while correctly leaving dark green (only the light token was touched),
confirming the harness catches a real regression rather than merely existing. Both mutations were
reverted before committing; `git diff --stat` was checked clean on both files first.

The frozen clock is `page.clock.setFixedTime` set to an instant just after the showcase's own
stale/error timestamps (chosen so `formatRelativeTime` renders fixed strings - "7 minutes ago",
"retry in 40 seconds" - independent of the real wall clock on any day the suite runs), which is
also why running the app under plain `--fixtures` without Playwright shows the same text drifting
with real time: that is correct, expected behaviour for a dev flag, not a bug the frozen clock
needs to hide.

A second, real gap surfaced only once this ran in GitHub Actions rather than only locally: the
first pushed baselines, generated on this repository's own dev VM, failed both light and dark in
CI - `ubuntu-latest` plus `playwright install --with-deps chromium` is the identical pinned
browser build, but a different font-rendering environment, and roughly 1% of pixels came out a
few shades off from font hinting alone. This is precisely what C14 means by "one OS/font
environment" - a dev VM's system fonts were never a controlled artefact, and pinning only the
browser was not enough. The fix has two parts. First, both generating and CI-verifying the
baselines now use the exact same reproducible environment - the official
`mcr.microsoft.com/playwright:v1.62.1-noble` image, matching `@playwright/test`'s pinned version
- rather than a bare `ubuntu-latest` runner or this dev VM; `.github/workflows/ci.yml`'s `visual`
job runs inside that image via `container:`. Second, even *that* is not perfectly bit-reproducible
across separate cold containers: two runs inside the *same* running container were byte-identical,
but a fresh container instance still showed the same ~1% noise with no code change, apparently
from the font cache building fresh on first use. `playwright.config.ts` now sets
`expect.toHaveScreenshot.maxDiffPixelRatio: 0.02` - confirmed, not assumed, to still fail the
CSS-mutation check above (24-36% of pixels moved) while comfortably clearing the ~1% noise floor
measured directly, in both a warm-container and cold-container run.

### Phase C — Configuration (2.5 d)

**C1 · Config schema, loader, validation** · 1.5 d · deps: A3, S3 · **DONE**
Creates: `internal/config/{types,secretref,yamltypes,yamlmerge,schema,load,validate,snapshot,
diagnostics}.go`, `veduta --check-config`. `schemas/config.v1.schema.json` and
`examples/veduta.yaml` already existed from the design phase; this milestone dropped the
schema's own "DRAFT" title marker now that a real loader validates against it.

Built ahead of S3, not blocked on it: S3 is really two separable questions - which expression
language rules use (irrelevant to C1; `rule.when` is stored and pattern-scanned for card ids as a
plain string, never evaluated) and whether `santhosh-tekuri/jsonschema/v6` errors combine with
`yaml.v3` Node positions into a usable `file:line:col`. The second was answered directly while
building this, not assumed: `jsonschema.ValidationError.InstanceLocation` is a JSON-pointer-style
path, walked through the same merged `*yaml.Node` tree that was validated
(`nodeAtPath` in yamlmerge.go) to recover the exact node, then attributed to a source file via a
provenance map built during conf.d merging (yamlmerge.go's `merger`). `kind.AdditionalProperties`
extends the path one more segment for an unknown-key error specifically, since jsonschema reports
those at the *containing* object, not the offending key - confirmed by testing both forms and
preferring the more precise one.

Pipeline, matching docs/01-architecture.md section 2 with one deliberate departure: yaml.v3 into
`*yaml.Node` → merge conf.d (mappings recurse key-by-key, arrays and scalars replace wholesale,
confirmed with `TestLoad_ConfDMergeOrder`) → decode straight into `any` (confirmed
`jsonschema.Schema.Validate` accepts plain Go `int`/`float64`/`map[string]any` natively - no JSON
round-trip needed, unlike `internal/widgets.Validate`'s JSON-only path) → schema validation →
decode into the typed structs above (`SecretRef.UnmarshalYAML` is where `${secret:NAME}` becomes
a marker, in place of a separate "expand" pass - simpler, same outcome) → semantic validation →
`*Snapshot`. The one departure: the doc mentions yaml.v3's own `KnownFields(true)` as a second
unknown-key guard: skipped, because every object in `schemas/config.v1.schema.json` already sets
`additionalProperties: false`, so schema validation alone already catches every unknown key with
equal-or-better `file:line:col` fidelity - a second, overlapping mechanism would just be more code
enforcing the same rule.

Semantic checks deliberately do NOT cover two things the schema's own field descriptions mention:
whether a rule's signal name is declared by the integration operation it names, and whether a
card's required slots are all bound. Both need an integration manifest, which does not exist
until Phase D loads one - checking them here would make config.Load depend on the
connections/broker package, which must not be an upward dependency of configuration loading.
`internal/contracts/semantic.go`'s existing `checkConfig` (built during the design phase, over
generic maps, explicitly "before the configuration types exist") is untouched: it still validates
things this package cannot yet (manifest/lock cross-checks), so the two are not fully redundant,
and the small overlap (card/channel/reference checks that don't need a manifest) is intentionally
reimplemented here in the typed, positioned form production code actually needs, rather than
made to share code with a generic-map checker built for a different job.

The "cycle" AC turned out to already be solved: `yaml.v3`'s own `Node.Decode` rejects a
self-referential anchor outright ("anchor ... value contains itself") rather than hanging -
confirmed with an isolated repro before writing any merge code, and again with
`TestLoad_Cycle` (a goroutine + 5s timeout, so a regression would fail loudly rather than hang
the test suite). This package's own tree-walking code (merge, duplicate-key detection, the
schema-error path walker) never follows `Alias` - only `Content` - which independently makes it
immune to looping on a cyclic document regardless of what `Decode` does.

Verified for real: `TestLoad_500CardsUnder50ms` generates 500 cards and times three runs, keeping
the best (22ms measured, comfortably under the 50ms budget) - skipped under `-race` via the
standard external `raceEnabled` build-tag pair, since race instrumentation alone measured ~180ms
with no code change, which is not what the budget is about. `veduta --check-config --config
examples/veduta.yaml` and a deliberately broken config were both run by hand against the built
binary, not just through `go test`, confirming the exact AC wording (prints diagnostics, exits
non-zero).

Not done in this milestone, deliberately: `serve` does not yet load configuration at all. C3
("watcher, atomic reload, status") owns deciding how a live `*Snapshot` interacts with `serve`'s
own flags and the running server - wiring that in now would mean guessing at C3's design
(precedence between `--listen` and `server.listen`, where the `atomic.Pointer[Snapshot]` lives)
rather than deciding it when C3 actually needs to.

**C2 · Secrets** · 0.5 d · deps: C1 · **DONE**
Creates: `internal/secrets/{value,provider,resolver,scrub,document}.go`, plus
`internal/config/secretlocations.go` and `internal/config/secretsuspicious.go` (see below),
`internal/contracts/secrets_boundary_test.go`.

`Value.String`/`GoString`/`MarshalJSON` all redact to `"***"` - confirmed directly, not assumed,
that `%v` and `%+v` both already respect `fmt.Stringer` but `%#v` does not (needs `GoString`
specifically), and that this holds when `Value` is nested inside another struct, matching where
it actually lives (`ConnectionAuth.Value`, `NtfyChannel.Token`, ...). `Reveal` is the one way out,
restricted to `internal/connections`, `internal/notify` and `internal/auth` by
`TestSecretRevealBoundary` - a source-text scan (not an import-graph check: a package that only
passes a `Value` along, never calling `Reveal`, is fine and common) - mutation-tested by adding a
real violation in `internal/api` and confirming it failed before removing it. `Resolver` tries
`FileProvider` (`/run/secrets/NAME`, trimmed - Docker secrets) then `EnvProvider`, per
docs/01-architecture.md's stated order; a provider error (permission denied) stops the search
rather than falling through, since silently trying the next provider would mask a real
misconfiguration. `Registry` is the slog scrubber: every `Value` ever constructed via `New`
registers its own value for scrubbing immediately, regardless of whether `Reveal` is ever called -
defence in depth starts at resolution, not at first use. Values under 8 characters are excluded
(docs/01's own stated rule, avoiding false-positive redactions); `cmd/veduta/main.go`'s `serve`
wraps its logger in `secrets.NewHandler` unconditionally, so scrubbing is active by construction,
not by remembering to opt in.

Two real gaps this milestone was explicitly asked to close, both found and written down before
C1 was called done (see docs/03-backlog.md, now moved to its Resolved section with full detail):

- `SecretRef` had no position. Fixed not by adding fields to `SecretRef` (ambiguous whenever a
  name is referenced more than once, and would need a reflection-based walk matching decoded
  struct fields back to schema paths) but by scanning the merged tree by *content* instead of by
  path: `Snapshot.SecretRefs []SecretLocation` records every `${secret:NAME}` occurrence with a
  real file:line:col, and `secrets.ResolveAll` reuses `config.Diagnostic` directly - one
  diagnostic per occurrence of a name that fails to resolve, not one anonymous complaint.
- "A secret value in a Widget Document is rejected by validation" cannot live in
  `internal/widgets` (the frozen import-boundary rule: widgets never sees `secrets`). Resolved in
  the other direction: `internal/secrets` imports `internal/widgets` instead (nothing forbids
  that; the frozen rule constrains integrations, not core packages), and
  `Registry.ContainsSecretInDocument` walks all nine block types explicitly by type-switch,
  matching this project's existing preference for that over reflection. Mutation-tested: deleting
  one block type's case from the switch was confirmed to fail
  `TestContainsSecretInDocument_EveryBlockType` before this was considered done. Honestly still
  pending: nothing calls this against a *real* produced Document yet, because nothing produces
  one until Phase F's scheduler exists - this is the primitive ready for that caller, not the
  end-to-end wiring, and is written down as such rather than implied to be more finished than it is.

A third, unplanned gap turned up while smoke-testing the whole pipeline end to end for the first
time (`config.LoadPath` → `secrets.ResolveAll` against the real `examples/veduta.yaml`, not just
unit tests): the Jellyfin connection's real auth header
(`Authorization: MediaBrowser Token="${secret:JELLYFIN_KEY}"`, per
docs/spikes/s2-upstream-reality-check.md's F6) embeds a secret reference inside a larger string,
which `secretRefPattern` has only ever recognised as an entire scalar value. As committed, that
value silently resolves to nothing and would send a literal placeholder string as Jellyfin's
credential once Phase D's HTTP client exists - with no error anywhere before this was found.
Mitigated now, not fully fixed: `suspiciousSecretRefs` warns (not errors - a config must still
load) on any scalar containing `${secret:` that isn't a whole-value match, verified to fire on
the real example file (`TestLoad_RealExampleConfig`). The real fix - a template-aware `SecretRef`
that composes and wraps the *whole* result in one opaque `Value` - is written down in
docs/03-backlog.md as a decision to make deliberately before D1 implements auth injection, not
something to guess at here.

Tests: 27 across `internal/secrets` plus the boundary test in `internal/contracts` (28 total), covering every
stated AC (redaction across all fmt verbs and JSON, both bare and nested; a missing secret's
diagnostic; provider ordering and error handling; the log scrubber against message/attr/group/
WithAttrs; the Reveal boundary) plus the three gaps above.

**C3 · Watcher, atomic reload, status** · 0.5 d · deps: C1 · **DONE**
Creates: `internal/config/watch.go` (fsnotify + 300 ms debounce), `GET /api/v1/config/status`.
Tests: invalid edit keeps the previous snapshot live and reports the error; valid edit swaps within
1 s; rapid successive edits coalesce; watcher survives editor rename-and-replace saves (vim/VS Code).

Implemented with a process-wide `config.Store`: readers load an immutable snapshot through an
`atomic.Pointer`, while status is copied under a small lock. Startup requires a valid config and
resolved secrets; reload repeats that complete pipeline and changes the pointer, generation and
checksum only on success. The watcher observes the containing directory (not the config inode),
plus `conf.d`, so rename-and-replace saves remain visible. The configured listener is used unless
`--listen` explicitly overrides it. Until the scheduler produces real documents, configured cards
are returned as honest `pending` envelopes, which makes the real dashboard path usable without
pretending integrations have run. `pnpm dev` deliberately uses the complete fixture showcase;
`pnpm run dev:config` is the separate real-config development path.

This landed as a local commit reviewed and verified separately before push (parallel work,
per the user's own framing): `go vet`/build/gofmt were clean, but `golangci-lint` was not yet
clean - `w.Close()`'s return value unchecked in `Watch`, and `err != context.Canceled` instead of
`errors.Is` in the test - both fixed the same way the rest of this codebase already handles them
(`_ = w.Close()`; `errors.Is`). More importantly, constructing `api.Config` with both `Fixtures`
and `ConfigStore` set was found to **panic** - both register the identical `GET /dashboard` and
`/cards` patterns, and `http.ServeMux` panics on a duplicate route rather than erroring cleanly.
Nothing in `cmd/veduta` can reach that state today (`serve`'s `if/else` sets exactly one), but
`api.Config` itself did not say so, and `api.New` did not check. Fixed at the root: `New` now
refuses a `Config` with both set, with a clear error naming why, verified by constructing exactly
that `Config` before the fix (confirmed it panicked) and after (confirmed a clean error) -
`TestFixturesAndConfigStoreAreMutuallyExclusive`. Everything else - the config/status/dashboard/
cards routes, the watcher's debounce and rename-survival, the `auth: none` + secrets startup
guard - was exercised by hand against the real `examples/veduta.yaml` (with placeholder env vars
for its four secrets) in addition to the existing test suite, and matched this account exactly.

### Phase D — Connections, broker, declarative runtime (5 d) — the security core

**D1 · Connection registry and HTTP client** · 1.5 d · deps: C1, C2 · **DONE**
Creates: `internal/connections/{types,build,client,dial,auth,rate,registry,request,health}.go`.
Every Go type matches docs/01-architecture.md section 3's frozen signatures exactly (`Connection`,
`HTTPConfig`, `Auth`, `Registry`), with one addition the doc's sketch doesn't spell out:
`Auth.Value`/`.Pass` are `secrets.Value` even when the underlying config held a literal, never a
real secret (`internal/config.SecretRef.Literal`) - so injection never has to distinguish "a real
credential" from "a literal that happens to look like one," and neither is ever logged by
accident. `New(connections, resolved, logger)` is the bridge from C1/C2's output to a working
`Registry`: it refuses to build - loudly, not with an empty credential - if a connection
references a `${secret:NAME}` that was never resolved, and it is the one place a
`golang.org/x/time/rate` limiter and a small buffered-channel semaphore get attached per
connection.

Each mandatory test was built to demonstrate its property directly, not asserted from reading the
code:

- **IP pinning against rebinding**: `pinnedDialer`'s resolver is an injectable field (a stub in
  tests, `net.DefaultResolver.LookupIP` in production), used to prove the property that actually
  stops rebinding - a single `DialContext` call performs exactly one lookup and connects to
  exactly that answer, so there is no separate "check" step whose result a later "use" could
  contradict. Verified with real local listeners on two loopback addresses and a stub that
  answers differently per call, counting calls with `atomic.Int32`.
- **Redirects**: refused cross-host unconditionally, and same-host beyond `MaxRedirects` (default
  0, per docs/01 section 8) - both directions verified against real `httptest` servers, including
  proving a same-host redirect *within* an explicit limit still succeeds (the refusal is about
  the default and cross-host safety, not redirects being broken outright).
- **Oversized response**: `io.LimitReader(body, limit+1)` then a length check - a response that
  reads exactly `limit+1` bytes is the error case, never silently returned truncated as if
  complete.
- **Path traversal / absolute URL in Path**: `joinPath` rejects scheme-bearing and `//`-prefixed
  paths outright, then independently confirms the `path.Join`-cleaned result still starts under
  the base path - added specifically because `path.Join("/a", "../../etc")` cleans silently to
  `/etc` with no error from `path.Join` itself, which would otherwise be a traversal `joinPath`
  missed by trusting the standard library's own cleaning as if it were a security boundary.
  Deliberately self-contained and *not* the general-purpose canonicalisation D1b centralises -
  this is refactored to call into `internal/connections/routepath` once D1b exists, per that
  milestone's own AC ("no other package in the tree performs path comparison or unescaping").
- **`InsecureSkipVerify` warning**: asserted against a real `slog.NewTextHandler` writing to a
  buffer, checking for an actual `level=WARN` line - not merely that `logger.Warn` was called in
  the source.

Composes against the real `examples/veduta.yaml` end to end - `config.LoadPath` →
`secrets.ResolveAll` → `connections.New`, the exact sequence `cmd/veduta`'s `serve` already runs
for config and secrets - building all five real connections (three `http`, one `docker`, one
more `http`) without error (`TestNew_RealExampleConfig`).

Not wired into `cmd/veduta` in this milestone, deliberately: nothing calls `Registry.Do` yet - the
capability broker (D2) is what integrations reach a connection through, and building a `Registry`
in `serve` with no real consumer would be wiring for its own sake. `internal/connections` is a
complete, independently tested package waiting on D2 to call it, the same way C1's loader waited
on C3 to wire it into `serve`.

**D1b · Route canonicalisation and matching** · 1 d · deps: D1, Part 0 · **DONE**
Creates: `internal/connections/routepath/{canonicalise,match}.go` implementing
docs/01-architecture.md's normative algorithm exactly - every reject rule, then "decode the
remaining unreserved escapes exactly once, lowercase the percent-hex digits, re-encode to a
single normal form."

One real gap found and fixed while writing the first example-based test: the bare root `"/"`
was being rejected as an empty segment (`strings.Split("", "/")` yields `[""]`, one element, not
zero) - fixed with an explicit `raw == "/"` case returning `"/"` directly, zero segments, before
the general splitting path runs at all.

`Match` is ordinary `*`-only glob matching per segment (split pattern on every `*`, anchor the
first literal part to the segment's start and the last to its end, require any middle parts to
appear between them in order) - handles any number of `*` in one segment correctly, including
the schema's own allowance for more than one, which no first-party manifest uses today but the
pattern syntax permits.

The fuzz target (Go's native `testing.F`, not a hand-rolled corpus) runs two properties together
against arbitrary input, not just the hand-picked adversarial cases: idempotence
(`Canonicalise(Canonicalise(x)) == Canonicalise(x)`), and - the property that actually is "check
vs use can't disagree" - every canonical form matches itself under `Match` read as a literal
pattern. 4.2 million fuzz executions (20s, `-fuzz`) found nothing; both properties also hold
against the hand-picked adversarial corpus (`%2e%2e`, `%2E%2E`, `%2f`, `%5c`, backslash, control
characters, malformed escapes, `//`, `.`/`..` segments - every case the milestone names).

**Refactored `internal/connections`'s `joinPath` to call this package**, closing the AC directly
rather than leaving it as a documented future intention: D1's original `joinPath` used
`path.Join` plus a prefix check, which is exactly the pattern this milestone exists to replace -
`path.Join("/a", "../../etc")` cleans silently to `/etc` with no error of its own. The new
version canonicalises the connection's base path and the request path *independently* (each
rejects its own `..`/encoded-separator/control-character content on its own terms), then simply
concatenates the two canonical strings - because neither can contain `..` any more, there is
nothing left for a join step to clean away a traversal from, unlike the old approach. Every
existing `joinPath` test in `internal/connections` passed unchanged against the new
implementation with no test edits needed - the refactor preserved observable behaviour exactly
while removing the class of bug that motivated D1b in the first place.

`TestRoutepathBoundary` (`internal/contracts`, alongside the other repo-wide invariant tests -
the Reveal boundary from C2, SPDX headers, licensing) is the "import check" AC: a source-text
scan for `path.Join`/`path.Clean`/`filepath.Join`/`filepath.Clean`/`url.PathUnescape`/
`url.PathEscape` calls anywhere under `cmd/` and `internal/` outside the routepath package
itself, against an explicit allowlist recording *why* each current hit is not a routepath
violation (the SPA's own static-file traversal guard, and several purely local-filesystem paths
in config/secrets loading - genuinely different from a connection's upstream route authority).
Mutation-tested: a fake violation added to `internal/api` was confirmed to fail the test before
being removed. The allowlist itself is checked too - an entry for a file that no longer makes the
matched call fails loudly rather than silently going stale.

Five of the six call sites the milestone names (manifest load, lock load, asset mint, asset
serve, connection `allowedPaths`) do not exist as real code yet - they arrive in D2b and E1. The
one that exists today (`http.request`, `internal/connections`'s `joinPath`) is refactored and
tested; `TestRoutepathBoundary` is the standing guard that keeps the other five honest once they
are written, rather than something to re-derive per milestone.

**D2 · Capability broker with route grants** · 1.5 d · deps: D1, D1b, Part 0 · **DONE**
Creates: `internal/capabilities/{types,grant,errors,request,authorize,headers,budget,cache,audit,
broker,assetref}.go`, matching docs/01-architecture.md section 6's frozen `Route`/`Grant`/
`Broker` signatures exactly.

`Grant.Authorize` is the one place a request is compared against policy, and it is genuinely
three independent searches - `anyRouteAllows` walks `ManifestRoutes`, then separately walks
`ApprovedRoutes`, then `connectionAllows` checks the connection's own `AllowedPaths` - never a
precomputed intersection.
`TestAuthorize_NoGlobIntersectionShortcut` proves this directly: a manifest glob and a lock
literal route that each independently match a request succeed, but a second request the
manifest's glob would match and the lock's literal route would not is denied - which a naive
intersection could get wrong in either direction. Every route's own `queryKeys`/`contentType`/
`maxBodyKB` is checked as part of matching *that* route (docs/01's "route identity is the full
tuple"), not bolted on afterwards. `queryKeys: nil` vs `queryKeys: []` is a real, separately
tested distinction (unconstrained vs nothing at all); content-type comparison reuses the exact
`mime.ParseMediaType`/`FormatMediaType` normalisation `internal/contracts.NormaliseMediaType`
already established, reimplemented locally rather than imported - the same call this project
made for `internal/config` in C1, for the same reason (production code should not depend on the
contract-suite package).

Header/query stripping (`headers.go`) is the allowlist docs/01 names exactly (`Accept`,
`Accept-Language`, `Content-Type`, `If-None-Match`, `If-Modified-Since`, `Range`), plus
unconditional stripping of `Host`/`Content-Length`/`Transfer-Encoding`/`Connection`/`Upgrade` and
whatever the connection itself owns (its auth header or query parameter name, and every name in
its static `headers` map).
`TestBroker_HTTP_StripsConnectionOwnedAuthAndPluginHeaders` proves this on the real request path
(a real `connections.Registry` against a real `httptest.Server`), not only in isolated unit
tests: a plugin trying to override the connection's own `X-Api-Key` and inject a non-allowlisted
header gets neither - the upstream server only ever sees the connection's real value and the one
allowlisted header that was actually sent.

Budgets (`budget.go`) are a `*budgetState` pointer inside `Grant`, so every broker call sharing
one `Grant` value shares the same mutable counters (Go copies the struct, not what its pointer
field points to) - `hostCalls` is decremented by all six methods, `httpRequests` by `HTTP` alone,
`cacheEntries`/`cacheBytesKB` by `CachePut` alone. `TestBroker_HostCallsSharedAcrossMethods`
proves the sharing specifically: exhausting the budget via `Log` then blocks `Emit`.

Two deliberate scope boundaries, both because the infrastructure they'd need does not exist yet:
`Cache` and `Audit` are in-memory, process-lifetime implementations (`NewMemCache`/
`NewMemAudit`) - real persistence (`plugin_kv`, `audit_log`, docs/01 section 10) waits for
whichever milestone actually wires SQLite, since correctly testing namespacing and budgets does
not require persistence. `AssetRef` mints a token structurally matching section 7's payload but
without the connection-revision field (`cf`) or a persisted signing key - both depend on
`connection_state`, which needs storage that arrives with milestone E1 (see docs/03-backlog.md).
`ExecutionIdentity` is carried on `Grant` (used today only for cache namespacing's manifest
digest) but not fenced against a live snapshot generation - that wiring is Phase F's scheduler,
which is what actually constructs and cancels one per invocation (docs/01 section 11).

AC verified directly, not assumed: `go tool cover` shows the four-check preamble of every broker
method at 100% (`HTTP` 96% only because of one unreachable connection-type-mismatch line already
covered by its own dedicated test; `CacheGet`/`CachePut`/`Log`/`Emit`/`Authorize` all exactly
100%) - reached by first writing the obvious tests, then reading the coverage profile and adding
one test per still-red line rather than guessing which branches mattered.
`TestAuthorize_WrongMethodDeniedForEveryMethod` is parametrised over all six `Route.Method`
values × all six requested methods (30 cases), the literal "a route-denial test exists for every
method in the enum" AC.

**D2b · Integration lock and approval flow** · 1 d · deps: D2, C1 · **DONE** (core + CLI + REST;
sudo window and audit trail deferred, see below)
Objective: make "safe to install from strangers" enforceable without a marketplace.
Creates: `internal/integrations/lock.go` (canonical manifest digest, lock read/write through
`config.Apply`, permission diff), `veduta integration approve|list|diff`, `GET /api/v1/integrations`,
`GET /api/v1/integrations/{id}/approval`, `POST /api/v1/integrations/{id}/approve`,
`POST /api/v1/auth/sudo`, audit entries.
Also creates: `internal/integrations/canonical/` (RFC 8785 digest) with the golden fixtures from
`testdata/canonical/` as Go table tests.
Tests: the Go digest reproduces every golden fixture; reordering keys or set-like arrays does not
change it while any semantic edit does; a float in a manifest is a load error; an unlocked
integration loads as `disabled` with `disabledReason: unapproved` and never runs;
a manifest edited to add a route or capability is **refused** with a diff until re-approved; a
manifest edited to swap `module` + `sha256` is refused (proving the digest covers the manifest, not
just the module); **approve with a stale `expectedManifestSha256` returns 409 and changes nothing**;
**approve outside a sudo window is rejected in password mode**; forward-auth and `auth: none` accept
approval only from the CLI; a client may approve a strict subset of the requested grants; `builtin`
integrations are exempt; every approval is audited with actor, IP and diff.
AC: the UI shows a human-readable **applicability report** — for each declared route, whether it is
approved, and whether the connection's policy would permit it — produced by the same three
independent checks the broker runs. It is explicitly not a computed three-way glob intersection.

**Scope decision, made explicitly before starting rather than discovered mid-build:** this
milestone's own text above asks for a sudo-window gate and an audited approval trail, but sessions
(H1, deps: F1 - not built) and `internal/audit` (H2, deps: H1) do not exist, and D2b's own listed
deps are only `D2, C1`. Building a fake sudo window with nothing real to gate would be worse than
not building one, so the implementation covers everything with no such dependency - the digest,
lock file, permission diff, CLI and REST access to approve - and the sudo/audit gap is recorded in
docs/03-backlog.md (priority H2) with the exact hook point named (`internal/api/integrations.go`'s
approve handler) rather than silently built against non-existent sessions or silently dropped.

`internal/integrations/{types→route,limits,manifest,source,lock,status,diff,approve,schema}.go`
reuses `internal/canonical` (already built in Part 0) for the digest rather than duplicating it -
the plan's own "Also creates: internal/integrations/canonical/" line turned out unnecessary once
Part 0's package was confirmed to already implement exactly what this milestone needed.
`ComputeDiff`'s route/capability/limit comparisons are genuinely the "well-defined, exact-tuple"
side docs/01 draws a contrast with - `manifest ∩ lock computed by exact-tuple comparison` - never
the three-independent-searches evaluation `Broker.Authorize` performs at request time; the two are
deliberately different operations serving different purposes (showing a human what changed vs.
authorising one concrete request) and this milestone does not conflate them.

Found and fixed while building `ComputeDiff` against the real, checked-in fixtures rather than only
synthetic ones (`TestComputeDiff_RealExamplesMatchTheirLockRecords` runs every one of `plugins/*/
manifest.yaml` against `examples/veduta.lock.yaml`): the lock's own `effectiveLimits` formula is
`min(core maximum, manifest value-or-default, approved value-or-default)`, where *approved* comes
from the lock entry's own optional `limits:` field, genuinely independent of what the manifest
requests - this is `internal/contracts/semantic.go`'s own pre-existing, already-tested formula, not
something invented here. `examples/veduta.lock.yaml`'s glances entry recorded
`effectiveLimits.timeoutMs: 3000` (the default) while its manifest requests `4000` with no
`limits:` override on record to justify granting it - a real, previously uncaught inconsistency in
a hand-authored fixture, caught the moment a real reconciliation implementation existed to check
it. Fixed by adding the missing `limits: {timeoutMs: 4000}`, mirroring the convention immich's own
entry already used. A second, independent fixture bug found the same way: immich's lock entry
recorded `version: 0.1.0` while its manifest's actual (and correctly-digested) version is `0.2.0` -
a stale display-only field, fixed to match.

`internal/integrations.Grants` carries an optional `Limits` field the architecture doc's own
illustrative `POST /approve` body does not show (see docs/03-backlog.md) - an additive field,
needed because otherwise no client could ever grant a limit above its default through the
documented endpoint at all. The CLI and the REST handler's own default echo the manifest's full
requested limits back as that override, matching how routes and capabilities already default to
"approve everything requested"; a narrower value is how an admin declines an elevated limit, the
same subset-approval shape routes and capabilities already have. `Approve` refuses a `Grants` that
asks for more than the manifest requests on any of the three axes (capabilities, routes, limits) -
symmetric checks, all returning `ErrGrantExceedsRequest`.

`veduta integration list|diff|approve` (`cmd/veduta/integration.go`) talks to
`internal/integrations` directly rather than over HTTP, so approval works without a running
server - the CLI is documented as available under every auth mode, including `forward` and `none`,
for exactly this reason. `approve` without `--yes` prints the diff and requires an interactive
`y`/`yes` confirmation before writing; a diff that is already empty is a no-op regardless (no
prompt, matching "approving nothing does nothing"). `GET/POST /api/v1/integrations...`
(`internal/api/integrations.go`) is the REST equivalent, wired only when `Config.ConfigStore` is
set; its `POST .../approve` recomputes the digest from a fresh `LoadManifest` call within the same
request, so `ErrDigestChanged`'s 409 response is never built from a stale in-memory manifest.

`internal/contracts/routepath_boundary_test.go`'s `routepathAllowlist` gained four entries for this
milestone's new `filepath.Join`/`filepath.Dir` call sites (locating `veduta.lock.yaml` next to the
primary config file, resolving a `path:` integration source, finding a manifest file on disk) -
local filesystem paths, the same category the existing config/secrets entries already cover, not
the connection-route authority `internal/connections/routepath` exists to guard.

91.5% coverage on `internal/integrations` (`go tool cover -func`), reached the same way as D2:
obvious tests first, then one targeted test per still-uncovered line - every deliberately-triggered
error path (malformed route mid-diff/mid-approve, a hand-built lock entry that would bypass
`Approve`'s own field-stripping, a schema-rejected lock document) is a real regression test, not
just a coverage number.

**D3 · Declarative runtime** · DONE · deps: D2b, S3, B2 · S3 resolved: `expr-lang/expr`, used as
parser/evaluator only — see docs/01-architecture.md D47 and §5's "expr is a parser and evaluator;
D3 is the sandbox"
Creates: `internal/integrations/declarative/` (pipeline executor and an `expr`
environment whose parser-special predicate builtins retain native syntax and have their collection
argument wrapped by `expr.Patch` for call-boundary charging; ordinary scalable helpers are D3-owned;
also the output builder and signal emitter) and `internal/integrations/manifestload/` (pre-parse limits: byte cap,
YAML depth/node/alias caps, duplicate-key rejection, then schema validation, then aggregate
expression/template ceilings).
Also creates: the resource budget from §5 — streaming decode with `inputMB`/`jsonDepth`/`jsonNodes`
caps, a load-time `exprNodes` check, and one shared `iterations` counter decremented by every
`each`/`map`/`filter`/`sortBy` and template expansion; the explicit supported-function list (D47)
that is the actual manifest DSL surface, versioned as a compatibility promise independent of
`expr`'s own stdlib.
Tests: fixture-driven — the real, checked-in `plugins/glances/manifest.yaml` and
`plugins/immich/manifest.yaml` run to a real Widget Document, both through a fake broker
(`fixtureBroker`, exercising the runtime's own template/pipeline logic in isolation) and through
the **real** `capabilities.Broker` + `connections.Registry` + a real `Grant` built from the
manifest's own routes (`TestGlances_RealBrokerEndToEnd`) — the latter added on review, since the
fake broker alone never proves D3's constructed `HTTPRequest` actually satisfies a real Grant's
authorization; a companion test drops one route from `ApprovedRoutes` and confirms the pipeline
step calling it is denied by the broker, not silently allowed. **Adversarial** (in
`manifestload`'s own test suite, added on review — none of this existed at first commit despite
the checks themselves being present in `load.go`): an oversized manifest, excessive YAML
depth/node count, aliases (forbidden outright, not accounted), duplicate keys, an
over-512-AST-node single expression, twelve expressions each under the per-expression cap that
together exceed the 4096 per-operation aggregate ceiling, a literal string past the 64 KiB
literal-byte ceiling, an undeclared slot, a pipeline request with no declared `http` capability, a
static pipeline route with no matching manifest route, a static `{asset}` node with no matching
`use: asset` route, an asset node with no `assets` capability, and an undeclared signal — all
twelve are load errors, each with its own regression test. `sortBy` is charged `n·log₂n` and `map`
call-boundary-charged by length, both proven directly against `compile`/`run`
(`exprenv_test.go`); `TestEveryExprBuiltinIsExplicitlyClassified` enumerates
`github.com/expr-lang/expr/builtin`'s actual registered names and asserts each has exactly one
classification (predicate-charged / D3-replaced / explicitly allowlisted / denied) — an expr
upgrade that adds a new builtin fails this test rather than silently exposing it.

**A real, confirmed vulnerability found and fixed on review, before this milestone's commit was
pushed:** `declarative`'s expr environment threads the invocation's `capabilities.Grant` and
`context.Context` through the *same* `map[string]any` environment user expressions evaluate
against, under the keys `__grant`/`__ctx` (necessary because `expr.WithContext` requires a named,
addressable variable). `manifestload`'s AST validator rejected `$env` by exact name but did not
know about these two runtime-internal keys, so a manifest expression `{expr: "__grant"}` compiled
and evaluated cleanly - confirmed directly, not assumed, by compiling and running exactly that
expression against a real budget and env before the fix, which returned the raw env value
verbatim. Depending on placement this could render the full authority object (approved routes,
capabilities, limits) into a rendered Widget Document, or let a manifest make control-flow
decisions based on internal authorization state no plugin is supposed to be able to introspect.
Fixed by rejecting any identifier with a `__` prefix in the same validation pass as `$env`, closing
off every internal name the runtime uses (`__grant`, `__ctx`, `__d3_charge`) and any future one by
construction rather than requiring the denylist to be extended by hand each time one is added;
`TestLoad_RejectsInternalIdentifierAccess` is the regression test.

**Not fixed, recorded instead (docs/03-backlog.md):** `manifestload.Limits` uses plain `int`
fields, so an explicit `cacheEntries: 0` (schema minimum 0, a real distinct value from "unset" -
the exact distinction `internal/integrations.Limits` was built with pointer fields in D2b to
preserve) is indistinguishable from omission and gets silently widened back to the default by
`WithDefaults`. Dormant today - D3 never calls `Broker.CacheGet`/`CachePut`, since the frozen
four-node template grammar and pipeline step shape have no cache-triggering construct - but a real
bug the moment caching is wired in.
AC: an integration is added by dropping one YAML file in and approving it once, with no rebuild.

**D4 · Generic HTTP/JSON card** · 0.5 d · deps: D3 · **DONE**
Objective: Homepage's `customapi` equivalent, defined inline in a card without a manifest — the
migration workhorse.
AC: a card with `integration: http-json` plus a `view:` block renders metrics from any JSON API
(the plan's own text said `mapping:` - stale wording from before C1's config schema shipped;
`schemas/config.v1.schema.json`'s actual, already-frozen field is `view` -
`Card.View map[string]any`, and `examples/veduta.yaml`'s own checked-in adguard card already uses
it. Followed the real schema, not the plan's earlier wording).

Does not implement a second execution engine: `docs/01-architecture.md`'s "http-json escape
hatch" already specifies "the same expression and resource budgets as any declarative
integration," so `internal/integrations/manifestload.SynthesizeHTTPJSON` builds a single-operation
`Manifest` in memory from a card's `params.path`/`params.method`/`params.query` and its `view:`
block, and hands it to `internal/integrations/declarative` (D3) completely unchanged - an
http-json card gets the identical charged-builtin expression sandbox, budgets, and
`$env`/`__`-prefix/`matches`/custom-call rejection a real manifest does, not a parallel,
unreviewed one. `internal/integrations/httpjson` is the thin layer above that: it builds the
self-approving lock entry (http-json is exempt from approval - docs/01 section 6, "builtin
integrations... have no separate trust boundary" - a sentinel digest `HTTPJSONDigest` on both the
manifest and the lock entry, since there is nothing external for a digest to drift against when
the "manifest" is synthesised fresh from the same card config every invocation) and the `Grant`
authorising exactly the one request the card names.

Two things not obvious from the AC, decided while building:
- **Route grant self-consistency, not weakening.** With no manifest file and no approval record,
  `ManifestRoutes`/`ApprovedRoutes` are both trivially derived from the same `params.path` the
  card itself configures - not independently meaningful the way a real manifest's request vs. an
  administrator's approval are. The connection's own `ConnectionPolicy` (`allowedPaths`) is
  untouched and remains the real, independent gate - proven directly, not assumed:
  `TestBuild_ConnectionAllowedPathsStillGates` shows a path outside `allowedPaths` is still denied
  even though it's the card's own configured path.
- **Bare field names, not `{expr: ...}`.** `view:` values are written the way Homepage's
  `customapi` mapping already was - `num_blocked_filtering / num_dns_queries`, no wrapping syntax
  - which doesn't match how D3's pipeline binds a response (`env[step.As] = decoded`, so fields
  live under a name, not at the top level). Rather than changing D3's already-reviewed env
  construction, `manifestload.bindBareFieldsToData` rewrites the AST before compiling: every bare
  identifier except the two real top-level bindings (`params`, `now`) becomes member access on
  `data` (the pipeline step's own binding name) - `num_dns_queries` becomes `data.num_dns_queries`.
  This turned out to also be a stronger, structural version of the D3 review's `__grant` fix:
  writing `__grant` as a view value rewrites to `data.__grant`, ordinary (and always absent) field
  access on the JSON response, not the real internal env key - `data` can never contain it, so
  there's nothing for the shared `__`-prefix check to even need to catch here.
  `TestBuild_BareFieldNamesCannotReachInternalEnvKeys` proves the rendered document never carries
  Grant-shaped content.

Tests: `TestBuild_AdGuardExampleRendersMetrics` runs `examples/veduta.yaml`'s real, checked-in
adguard card through a real `capabilities.Broker`/`connections.Registry`/`Grant` end to end
(same discipline as D3's own `TestGlances_RealBrokerEndToEnd`, not a permissive fake); the
connection-`allowedPaths` and internal-env-key tests above; `SynthesizeHTTPJSON`'s own tests cover
GET-default, POST/other-method rejection, a malformed path, an oversized view expression, and
`matches` rejection; `bindBareFieldsToData` is tested directly against a small table (bare names,
arithmetic, comparison, ternary, dotted access, and confirming `params`/`now` are left alone).

**D5 · Connection health and admin endpoints** · 0.5 d · deps: D1 · ⇉ with D3/D4 · **DONE**
Creates: `GET /api/v1/connections`, `POST /api/v1/connections/{id}/test`, health tracked per connection.
Tests: the response contains no credentials (asserted by scanning the JSON for every configured
secret value); test endpoint distinguishes DNS / TCP / TLS / auth / HTTP-status failures.

D1 already left `connections.Health{Reachable, Error}` and a one-line implementation with a doc
comment naming this milestone as where it grows a real classifier - it did: `classifyError` walks
the error chain with `errors.As` (through `*url.Error`'s `Unwrap`, so wrapping never hides the
real cause) to distinguish `*net.DNSError` (DNS), TLS-specific types (`tls.
CertificateVerificationError`, `x509.HostnameError`/`UnknownAuthorityError`/
`CertificateInvalidError`, `tls.RecordHeaderError`), a `*url.Error` that timed out or any other
`*net.OpError` (TCP/dial), then - once the round trip itself succeeds - 401/403 (auth) vs. any
other non-2xx (HTTP status) vs. 2xx (healthy). All five stages proven against **real** failures,
not mocked errors: a reserved `.invalid` hostname for DNS, a closed listener for TCP, an untrusted
`httptest.NewTLSServer` certificate for TLS, real 401/403/500 responses for the other two.

**A real, confirmed credential leak found and fixed while building this, before any response
shape existed to hide it:** a `query`-type auth connection injects its resolved secret directly
into the request URL (`injectAuth`); a network-level failure's `*url.Error` formats as
`Op "URL": Err`, so the raw credential would appear verbatim in `Health.Error` the moment such a
connection's probe failed at the DNS/TCP/TLS layer - not a hypothetical, since the config schema
has supported `auth.type: query` since C1. Fixed by scrubbing `Health.Error` through
`secrets.DefaultRegistry()` - the same process-wide registry the log handler already uses, since
every `secrets.Value` tracks itself there the moment it is constructed, active before this code
ever runs. `TestHealth_NetworkFailureNeverLeaksAQueryAuthCredential` is the regression test, and
`connectionSummary` (the REST DTO) is additionally built with no field a credential could ever
occupy in the first place - id, kind, health, nothing else - so "never credentials" is enforced by
the response's shape, not solely by remembering to scrub a richer one.

**External review of D2b through D5, before any of it shipped further (2026-09):** an outside
review of the manifest/lock/broker/declarative-runtime/connections code built across these four
milestones raised ten numbered findings plus a type-duplication and a plan-sequencing critique.
Verified independently against the actual code (not taken on trust) and addressed as follows -
full detail in each fix's own commit and test names, this is the summary:
- **Confirmed and fixed:** a redirect could reach an unauthorized route or downgrade HTTPS
  (`connections/client.go`'s `redirectPolicy` now enforces same-scheme and, when configured,
  `allowedPaths` on every hop - `routepath.HasPathPrefix`, `redirect_test.go`); `responseMB` was
  never enforced by the broker (`capabilities.Broker.HTTP`, `TestBroker_HTTP_ResponseMBEnforced`);
  `outputKB` was hardcoded to 64 KiB regardless of the approved limit
  (`widgets.ValidateWithLimit`, `TestInvoke_ApprovedOutputKBIsActuallyEnforced`); a caller-supplied
  deadline could *replace* rather than only *narrow* the manifest's approved `timeoutMs`, and the
  computed deadline was never actually attached to the HTTP context or the expr environment
  (`declarative/runtime.go`'s `Invoke`, `TestInvoke_CallerDeadlineCanOnlyNarrowNeverExtendTheApprovedTimeout`);
  the shipped Jellyfin example has been non-functional since D1 shipped, not merely "pending a
  decision" as the backlog previously said (`docs/03-backlog.md`, updated); both `manifest.go` and
  `manifestload/load.go` read a manifest's bytes multiple independent times for digest vs.
  structure, a real TOCTOU on digest-bound approval (`canonical.LoadBytes`/`DigestBytes`, both
  loaders now read once); static connection `headers` were configured but never sent
  (`connections/registry.go`'s `Do`, `TestDo_StaticHeadersAreSentAndOverrideTheCaller`);
  `allowedPaths` used a raw `strings.HasPrefix`, so `/api` wrongly matched `/apievil`
  (`routepath.HasPathPrefix`, shared by both `capabilities.connectionAllows` and the new redirect
  check); the approval API had no body-size ceiling and accepted trailing content after the JSON
  body (`api.Server.limitBody`, strict `json.Decoder` framing in `integrations.go`); hot reload
  updated only the config snapshot, not the `auth: none` safety check, which ran once at startup
  only (`cmd/veduta/main.go`'s `configLoader`,
  `TestConfigLoader_RejectsAuthNoneOnHotReloadNotOnlyAtStartup`) - this fixes only the safety-check
  half of what the review's finding named; the connection registry and its resolved secrets still
  do not reload as part of the same generation (an already-tracked, separate D5 limitation, see
  `docs/03-backlog.md`'s first Open entry - not newly introduced or newly fixed here); two concurrent approvals could
  race and lose an update, and `approvedBy` was client-supplied and unauthenticated
  (`api.Server.approveMu`, a fixed `unauthenticatedApprovedBy` sentinel,
  `TestIntegrationApprove_ConcurrentApprovalsOfDifferentIntegrationsDoNotLoseAnUpdate`,
  `TestIntegrationApprove_ClientCannotSupplyApprovedBy`).
- **Initially pushed back on, then confirmed correct on a second review pass:** the claim that
  dynamic output could emit an undeclared signal. The first pass traced `manifestload`'s template
  compiler, found that an object node's key set is always static (YAML mapping keys, never an
  `{expr}` node), and concluded from that alone that the hole was unreachable - true for an
  object-*shaped* `output`/`signals` node, but the compiler does not require either to be
  object-shaped at all: `output: {expr: "..."}` (a mapping whose only key is `expr`) compiles the
  *entire* output - the whole document, "signals" included - as one opaque expression, with no
  static key to check against `op.Signals` in the first place, since there is nothing to walk into
  until the expression actually runs. Confirmed reachable directly: an operation whose `output` is
  `{expr: '{"title": "t", "blocks": [], "signals": r}'}`, with `r` bound to an upstream JSON
  response naming a signal the manifest never declared, reaches `widgets.ValidateWithLimit` and
  produces exactly that undeclared signal - and, with the runtime's own post-evaluation check
  (`declarative/runtime.go`, added on the first pass "as a defensive measure") temporarily removed,
  the invocation succeeds with the undeclared signal in the response. The check is load-bearing,
  not future-proofing - `TestInvoke_DynamicOutputEmittingAnUndeclaredSignalIsRejected` and
  `TestInvoke_DynamicOutputWithOnlyDeclaredSignalsSucceeds` are its regression tests.
- **Redirect fix completed on a second review pass:** the first pass's redirect fix enforced
  connection-policy `allowedPaths` on a redirect hop but could not re-check the *lock's* route
  grant, since that needs a `Grant` plumbed into `connections.Registry`, which nothing did -
  correctly challenged as incomplete rather than accepted as done. Closed via
  `Grant.AuthorizesRedirect` plus a small context-carried hook
  (`connections.WithRedirectAuthorizer`) that `capabilities.Broker.HTTP` attaches before calling
  `registry.Do`, so `redirectPolicy` - built once at connection-construction time, with no `Grant`
  in scope of its own - can call back into the invoking Grant via the redirected request's context
  (which Go's `http.Client` carries unchanged through every hop). See `docs/03-backlog.md`'s
  Resolved section for the full account and its regression tests.
- **Residual gap recorded, not fixed here** (`docs/03-backlog.md`): the concurrent-approval fix
  closes the lock-write race within one process but not between the REST API and a
  concurrently-running `veduta integration approve` CLI invocation.
- **Refactor critique (type duplication across `integrations.Limits`/`manifestload.Limits`/
  `EffectiveLimits`/`capabilities.Limits`) confirmed as a real contributor** to the `outputKB`/
  `responseMB` bugs above, but full unification declined - each type earns its shape from a prior,
  deliberate decision. Mitigated with `TestManifestloadLimitsMatchesTheFieldSet` and
  `TestCapabilitiesLimitsIsARealSubsetOfLimitBounds` (`internal/integrations/limits_test.go`), see
  `docs/03-backlog.md` for the full reasoning.
- **Plan-sequencing critique confirmed:** E1 (and, transitively, E2/E3) needs F1's SQLite storage
  for `connection_state`/`settings`/`asset_cache` despite Phase E being written before Phase F, and
  H1's own `deps: F1` was never reconciled with "H1 moves before E3." F1 is now called out as
  needing to be pulled forward to immediately before E1/Phase E, both at F1/E1/E2/E3's own entries
  and in Part 3's critical path; G4's stale `X-Emby-Authorization` text (superseded by S2's
  completed **spec** pass, not its still-outstanding live pass) is corrected to the real
  `Authorization` header.

**Second review pass (2026-09), same review's own findings re-verified rather than taken as
closed:** a follow-up review of the fixes above found several real gaps in the fixes themselves,
addressed here rather than left for a third pass:
- **The manifest-TOCTOU fix (finding #4) had reintroduced an unbounded read.** Both
  `manifestload.Load` and `integrations.LoadManifest` read a manifest's bytes with a plain
  `os.ReadFile` before comparing the result's length against the byte cap - meaning a
  multi-gigabyte manifest would be fully allocated before being rejected, the exact unbounded-read
  the cap exists to prevent, just moved one line later. Both now go through a `readBoundedFile`
  helper (`io.LimitReader(f, maxBytes+1)`) that never allocates past the cap regardless of the
  file's actual size.
- **The redirect fix (finding #1) was genuinely incomplete**, not merely conservative - see the
  "Redirect fix completed on a second review pass" bullet above.
- **The undeclared-signal pushback (finding #6) was wrong** - see the "Initially pushed back on,
  then confirmed correct" bullet above.
- **`responseMB` (finding #2) was enforced only after the full response had already been read** at
  the connection's own, wider `MaxResponseBytes` ceiling - a post-hoc check on already-buffered
  bytes, not a bound on how much was ever read. `connections.Request` gained a
  `MaxResponseBytes` field the caller can use to narrow (never widen) the connection's own limit;
  `capabilities.Broker.HTTP` now passes the grant's `ResponseMB` through it, so `registry.Do`
  itself stops reading at `min(connection limit, grant limit)` - see
  `TestDo_CallerCeilingNarrowsTheConnectionLimit` and
  `TestDo_CallerCeilingNeverWidensTheConnectionLimit`.
- **The deadline fix (finding #2) checked only a stored wall-clock deadline, never `ctx.Err()`.**
  `declarative.budget.check()` now checks both - closing the case where a parent context is
  cancelled for a reason other than reaching the manifest's own timeout (a disconnected client, a
  future scheduler fencing a stale invocation), which previously would not stop a long-running
  expr-only loop (no HTTP call in it to notice the cancellation another way) until the wall-clock
  deadline anyway. See `TestBudgetCheck_ObservesContextCancellationNotJustItsOwnDeadline` and
  `TestRun_StopsPromptlyWhenContextIsCancelledMidExpression`.
- **`widgets.ValidateWithLimit` (finding #2) had no upper bound of its own** on the caller-supplied
  `maxBytes` - correct that a manifest's reconciled `outputKB` is already clamped to the schema's
  256 KiB core maximum before it ever reaches this function, but the function itself trusted every
  caller to have already done that, rather than enforcing it as a second, independent backstop.
  Added `MaxDocumentBytesHardCap` (256 KiB, cross-checked against `internal/integrations`' own
  core maximum by `TestOutputKBHardCapMatchesCoreMaximum`) as an absolute ceiling
  `ValidateWithLimit` clamps to regardless of what it is asked for. Also found: the existing
  "wider limit" test never actually exercised an output wider than the 64 KiB default (a single
  text block's content is itself capped at 2048 bytes by the schema, so the fixture could not
  reach past ~2 KiB) - rewritten using multiple schema-valid `list` blocks to genuinely test both
  the wider-limit and hard-cap paths.
- **Two plan-doc self-contradictions from the first pass, corrected:** A3 was cited as
  "already landed" in two places while its own line says "partially landed" - narrowed to name
  specifically which part (the HTTP-server scaffolding, not the still-open config-flags/request-id
  work) F1/E1 actually depend on. Part 3's critical path chain omitted A3 despite F1 (later in the
  same chain) depending on it - added.
- **S2's status was misstated in two places** as "S2's completed live pass" when the live pass is
  still outstanding (S2's own header: "spec pass DONE, live pass outstanding") - it was the
  **spec** pass that settled the `Authorization` header question. Corrected in both places.
- **S2 should block E1, not only E3 and G4:** E1's own asset-proxy content-type guard
  ("non-image content type refused") is exactly what S2's still-outstanding live pass informs
  ("the actual Content-Type of an Immich thumbnail... decides whether the asset proxy can compare
  headers at all"), previously wired only to E3. Added to S2's `blocks` line and E1's `deps`.
- **E3's synchronous invoke path was assumed, not scoped:** the first pass's dependency note
  argued E3 doesn't need F2/F3 because it invokes the integration synchronously per request, but
  named no deliverable that actually builds that path. `GET /api/v1/cards` currently returns
  `state.Pending` unconditionally for every card - E3's entry now names replacing that stub with a
  real `Invoke` call as its own new scope, not something already provided.

Wiring note: `internal/connections.Registry` had never actually been constructed in production
code before this milestone - D1 through D4 built and tested it in isolation, with Phase F (the
scheduler) always the intended place to hold a live one, and Phase F does not exist yet. D5 is the
first real caller, so `cmd/veduta/main.go`'s `serve` now builds one once at startup (`api.Config`
gained a `Registry` field, wired the same way `ConfigStore` is). Disclosed limitation, not silently
shipped: this registry does not rebuild when the config hot-reloads with different connections -
recorded in docs/03-backlog.md, since nothing before Phase F actually depends on it being live.

### Phase E — Assets and vertical slice #1 (2.5 d)

**E1 · Asset token and proxy endpoint** · 1 d · deps: D2, F1, S2 · **DONE** (the live S2 check is
still deployment validation, but no longer blocks correctness because response type is always
sniffed from bytes and never trusted from the upstream header). (F1: see note below - Phase E is
written before Phase F in this document, but F1's own dependency, A3, only needs A3's
already-landed HTTP-server portion (F1 adds its own `--data-dir` layout rather than needing A3's
still-open config-flags work first - see the note below for why this distinction matters), so F1
must in practice be pulled forward ahead of E1. S2: the live pass, not yet run - see S2's own entry)
Creates: `internal/capabilities/assets/` (mint, verify, HMAC key bootstrap in `settings`),
`GET /api/v1/assets/{token}` with all §7 guards.
Creates also: `connection_state` revision bootstrap and rotation on material config or resolved
secret change.
Tests: forged/edited/expired token rejected; constant-time compare; non-image content type refused;
oversized response refused; traversal refused; token for a deleted connection refused; **a token
minted before a connection's baseURL, auth or secret changed is refused afterwards** (revision
bump); **a token whose integration approval was revoked after minting is refused at serve time**;
a transform outside the allowlist is refused; a token minted for slot A cannot be re-signed for
connection B; the response never contains upstream credentials in any header; **the token payload
contains no hash of any configuration value** (asserted structurally).

**Dependency correction, found in the D2b/D3/D4/D5 review (2026-09):** the two bullets above ("Creates
also: `connection_state` revision bootstrap" and "persisted signing key") both name real SQLite
tables from §10 (`connection_state`, `settings`) that this document's own F1 entry says it creates
("the thirteen tables from §10"). E1 was written with only `deps: D2`, silently assuming storage
that does not exist yet at that point in the document's own phase order (Phase E precedes Phase F).
Since F1's own dependency, A3, is only *partially* landed (A3's own line above: request-id
middleware and config flags remain) but the part F1 actually needs - the HTTP-server scaffolding
`internal/api` already provides - is the part that's done, the fix is not to give E1 its own
bespoke bootstrap store, but to pull F1 forward: land F1 immediately before E1, keep the rest of
Phase F where it is. This does not require A3's still-open config-flags work first: F1's own line
already says it creates the `--data-dir` layout itself, so F1 is adding that flag, not waiting on
A3 to add it. `docs/03-backlog.md`'s existing entries for the asset-token signing key and
`connection_state` already say "Priority: E1" for this reason; this note makes the dependency
explicit in the plan itself rather than leaving it implied only in the backlog.

**E2 · Asset disk cache** · 0.5 d · deps: E1, F1 · **DONE**
Creates: `internal/storage/assetcache/` (sha256-addressed files, LRU eviction to a byte budget,
metadata in `asset_cache`). `asset_cache` is one of F1's own §10 tables; now that E1 formally pulls
F1 forward (see E1's note above), F1 is available by the time E2 starts and the earlier
in-memory-cache fallback is unnecessary.
Tests: cache hit avoids upstream; eviction respects the budget; corrupt file is re-fetched;
concurrent requests for the same asset coalesce.

**E3 · Immich integration — VERTICAL SLICE #1** · 1 d · deps: D3, E1, S2, F1 · **DONE in the
fixture-backed production path; real-server cold-latency validation remains the S2 live pass** (transitively, via E1 -
not F2/F3: this vertical slice invokes the integration synchronously per request, as the critical
path in Part 3 already implies by reaching E3 without F2/F3 in the chain; the scheduler's
single-flight/backoff/circuit-breaker machinery formalises this once F2 lands, it is not a
precondition for the first working demo)
Objective: **the demo.** Dashboard → Immich → six most recent photos → signed proxy → browser.
Creates: `plugins/immich/manifest.yaml` (declarative), `testdata/immich/*.json`, golden documents,
docs page, **and the synchronous invoke path itself** - `GET /api/v1/cards` (`internal/api/
server.go`) currently returns `state.Pending(c.ID)` for every configured card unconditionally (a
deliberate stub: "configured cards are returned honestly as pending so the production render path
is usable" until a real invoker exists), so *something* has to replace that stub with a real
`declarative.Instance.Invoke` call per request for E3's own AC to be true at all. **Named
explicitly here on review (2026-09):** the dependency note above asserted this synchronous path as
already implied by the critical path, but nothing in this entry previously listed building it - it
is new scope for E3, not something D3/E1 already provide. F2 later replaces this direct per-request
call with the real scheduler (single-flight, backoff, circuit breaker, caching) - this entry's own
version is intentionally the simplest thing that can be true: invoke on request, no retry, no
cache, no fencing.
Tests: fixture test producing a golden Widget Document; an end-to-end test with an httptest server
impersonating Immich, asserting the browser-visible HTML contains no API key and images load through
`/api/v1/assets/`.
AC: with a real Immich configured, six photos render in under 2 s cold; the Immich API key appears
nowhere in the page source, network tab, or logs.

### Phase F — Persistence, scheduling, live updates (4 d)

**F1 · SQLite storage and migrations** · 1 d · deps: A3 · **DONE**
Creates: `internal/storage/` (open with WAL/pragmas, embedded forward-only migrations, the thirteen
tables from §10), `--data-dir` layout.
Tests: migrations apply on an empty DB and are idempotent; **changing a card's integration,
operation, params or slot bindings changes its `card_hash` and discards the retained document rather
than displaying the previous card's data; editing a rule expression resets its debounce window**; concurrent readers during a write;
corrupted DB reports a clear error; the janitor prunes `signal_history` and `events` to their retention.

**F2 · Scheduler** · 1 d · deps: D3, F1 · **DONE**
Creates: `internal/scheduler/` (per-card jobs, jitter, worker pool, exponential backoff, circuit
breaker, `ExecutionIdentity` fencing), single-flight keyed by
`sha256(integration id + version/digest, operation, sorted slot→(connection id + revision) map, canonical params)`.
Tests: ten cards sharing one upstream call produce **one** HTTP request; **two cards binding
different connections to the same slot do NOT collapse**; **a card binding two slots does not
collide with a single-slot card**; a connection credential change bumps the revision and prevents
reuse of an in-flight result; a failing card backs off and, after N failures, opens its circuit — surfacing as
`stale`/`error` with `circuitOpenUntil` and a half-open `nextRunAt`, **never** as `disabled`, and the
probe actually fires and closes the circuit on success;
schedules rebuild on a config swap without losing state; deadline overrun cancels the invocation;
**a card referenced by a rule keeps its schedule with no SSE client connected for an hour** — the
regression test for the round-2 idle-pause contradiction (T1).

**F3 · Card state and stale-while-revalidate** · 1 d · deps: F1, F2, B2 · **DONE**
Creates: `internal/state/` persisting the `CardState` envelope, `GET /api/v1/cards`,
`GET /api/v1/cards/{id}`, `POST /cards/{id}/refresh` (rate-limited, single-flighted).
Tests: a failed run keeps the previous document and sets `execution.state=stale` with the new error;
a run that never produced a document sets `state=error` with `document: null`; an unapproved
integration renders `disabled`/`unapproved`, while an open circuit renders `stale` or `error` with
`circuitOpenUntil` and never `disabled`;
the integration's `hints.ttlSeconds` is clamped to the card schedule; `execution.generatedAt` is
always core-stamped even when the document contains a conflicting value; **a property test asserting
every state the store can emit satisfies the state-combination invariants** in the schema.
AC: after a restart the dashboard renders last-known-good data immediately, visibly marked stale.

**F4 · SSE hub and live frontend** · 1.5 d · deps: F3, B3 · **DONE, including the H1
per-session cap**
Creates: `internal/api/sse.go` (hub, per-session and global caps, 20 s heartbeat, 256-entry replay
ring, `Last-Event-ID`, `X-Accel-Buffering: no`), `web/src/lib/stream.ts` (store, backoff reconnect,
connection indicator).
Tests: Go test with 100 concurrent subscribers; slow-consumer disconnect does not block the hub;
**ring overflow, a server restart and an unknown `Last-Event-ID` each produce `event: reset` and the
client refetches `/cards`; a duplicated replay is idempotent because every event is a complete
envelope; reconnecting mid-publication loses nothing**; Playwright test asserting a card updates
without a reload.

### Phase G — WASM integrations and vertical slice #2 (4 d)

**G1 · WASM runtime** · 1.5 d · deps: S1, D2 · **DONE (sandbox; ARM performance acceptance outstanding)**
Creates: `internal/integrations/wasm/` (Extism/wazero host, compilation cache, sha256 verification,
memory/deadline/output limits, instance lifecycle).
Tests: **the conformance suite from S1, promoted to CI** — no filesystem, no env, no sockets, no
native HTTP, deadline kill, memory trap, output cap, and a corrupted module rejected before compile.
Implementation and scope: [S1 / G1 sandbox decision](spikes/s1-wasm-sandbox.md).

**G2 · Host functions** · 0.5 d · deps: G1 · **DONE**
Bind `veduta_http`, `veduta_cache_get/put`, `veduta_asset_ref`, `veduta_log` to the broker; encode
denials as plugin-visible errors rather than traps.
Tests: each host function's denial path; a plugin that ignores an error and retries is budget-capped.

**G3 · Plugin SDK and build tooling** · 1 d · deps: G2 · **DONE (Rust; Go guest SDK deferred)**
Creates: `sdk/rust/` (typed Widget Document builders + host bindings), `plugins/Makefile`, a
`plugins/examples/hello/` template, and a `veduta plugin validate <file.wasm>` command that runs the
production import policy, limits, instantiation path and an ABI smoke invocation against a
third-party module. G3's measurements found that the official Go PDK requires WASI and produces a
module that misses the guest size and cold-validation goals, so the Go SDK is deferred rather than
weakening the sandbox. The host-language review keeps the existing Go backend; see
`docs/decisions/0001-backend-language.md`.
AC: a new plugin can be scaffolded and built in under five minutes following the README.

**G4 · Jellyfin plugin — VERTICAL SLICE #2** · 1 d · deps: G2, S2 · **DONE (live-server validation outstanding)**
Chosen because it needs real logic: send the `Authorization: MediaBrowser Token="..."` header (S2's
completed **spec** pass settled on this over the legacy `X-Emby-Authorization`/`X-Emby-Token` forms
- F6 in `docs/spikes/s2-upstream-reality-check.md`, marked `[spec]` there, not `[live]`; S2's live
pass against a real server is still outstanding - see `examples/veduta.yaml`'s Jellyfin connection
for where the spec-pass decision already landed),
resolve the user, list recently-added items, and mint poster asset refs.
Tests: fixture test against the S2 specification-shaped Jellyfin responses produces the shared
golden document through the production Wasm runtime. The golden deliberately contains no upstream
URL because the plugin never receives one. **The same golden test must also pass if the integration
is reimplemented as builtin Go**, proving the runtime swap is faithful. Real-server capture remains
the external S2 backlog item.
AC: five posters render in the same visual style as the Immich grid, with no Jellyfin key reaching
the browser.

### Phase H — Authentication (2 d) — **H1 moves before E3**; ⇉ with F/G otherwise

**Ordering change:** H1 (sessions) is now scheduled immediately after C2 and before E3/A4, so the
first build that talks to a real credentialed service can also be exposed safely. Until H1 lands the
server binds loopback only. H2 remains where it is and must land before any public release.

**Ordering correction, found in the D2b/D3/D4/D5 review (2026-09):** H1's own `deps: F1` line was
already correct, but this section's "moves before E3" claim did not carry that dependency along
with it - as originally written, H1 was scheduled before E3 while F1 (Phase F) was still scheduled
after E3, meaning H1 could not actually build where this section placed it. E1's entry above now
pulls F1 forward to immediately before E1/Phase E for the same reason (`connection_state` and the
signing key), which resolves this too: with F1 landing before Phase E, H1 (deps: F1) can genuinely
land where this section says it does, before E3.

**Why Part 3's critical path chain does not list H1 or A4, despite this section's own "before
E3/A4" wording (a second inconsistency the same review found, resolved by making the distinction
explicit rather than picking one wording and dropping the other):** "H1 moves before E3" is a
*build-order* recommendation for a team planning to expose the running server on a real network
sooner rather than later, not a dependency of the critical path's own goal, which A3's own gate
already answers directly - the listener refuses a non-loopback bind until H1 lands, so a
**loopback-only** demo (the critical path's actual target: "a compelling demo," not "a
publicly-reachable one") needs no auth at all and genuinely does not depend on H1. The same is
true of A4 (a container image/release build): nothing about producing one requires sessions to
exist first, so "before A4" is the same build-order preference, not a technical dependency - A4's
own `deps: A2` line is unaffected and correct as it stands. Both are real, useful sequencing advice
for a team optimising for "safe to expose early," which is a different optimisation target than
the critical path's "fastest to a demo" - stated as two different answers to two different
questions here, rather than left for a reader to reconcile as one.

**H1 · Sessions** · 1 d · deps: F1 · **DONE**
Creates: `internal/auth/` (argon2id via `x/crypto`, session store, cookie flags, CSRF double-submit,
login rate limiting and lockout), login UI, `POST/DELETE /auth/session`, `GET /auth/me`.
Tests: wrong password is constant-time-ish and rate-limited; session fixation prevented by rotation;
CSRF rejected on a cross-origin mutation; expired session cleaned up; logout revokes server-side.
Landed with SHA-256-only session identifiers in SQLite, strict/HttpOnly cookie flags, a four-stream
per-session SSE ceiling, and the API under `/api/v1/auth/*` as specified by the versioned API table.

**H2 · Forward-auth, `auth: none` gating, audit log** · 1 d · deps: H1 · **DONE**
Creates: trusted-header mode with a trusted-proxy CIDR allowlist, `internal/audit/`, a persistent UI
banner when auth is disabled, and a refusal to start with `auth: none` when actions or secrets exist
unless `--i-know-what-im-doing`.
Tests: a spoofed auth header from an untrusted source IP is ignored; audit entries written for login,
logout, config apply, plugin load, and every action.
Landed with direct-peer CIDR validation, same-origin mutation checks, CLI-only or admin-group
privileged policy, five-minute password sudo windows, persistent audit records for authentication,
configuration/plugin lifecycle, approvals, manual refreshes, connection tests and capability
denials. Auth-mode changes require restart so a hot reload cannot retain stale middleware.

### Phase I — Docker and host metrics (2 d)

**I1 · Docker connection and integration** · 1.5 d · deps: D1, D3 · **DONE**
Creates: `internal/connections/docker.go` (~200 LOC over the Engine API via unix socket or TCP; no
`docker/docker` dependency), a builtin `docker` integration (container list, state, health, image,
uptime; optional stats), compose docs for the socket proxy.
Tests: against recorded Engine API responses; a socket-proxy-restricted endpoint returning 403
degrades gracefully; the Docker capability is not reachable from any plugin (asserted).
AC: a card lists containers with per-container status; actions remain disabled.
Landed as a core-only `DockerRegistry` extension with Unix/TCP transports, a bounded fixed GET
allowlist, Docker-aware connection health, the builtin container card, and a socket-proxy compose
guide. Plugin-facing `Registry.Do` continues to reject Docker-kind connections.

**I2 · Host overview via Glances/Beszel** · 0.5 d · deps: D3 · **DONE**
Creates: `plugins/glances/manifest.yaml` (CPU, memory, disks, network, sensors, uptime) and a
`beszel` variant, plus docs showing the one-line Glances container to run.
AC: the coding-server card from the target screenshot renders with no SSH and no credentials on the host.
Landed with CPU, memory, filesystem, network, temperature and uptime rendering for Glances, a
Beszel Hub system-record variant, golden runtime fixtures, corrected example plugin paths, and
deployment guidance for a one-command Glances API container.

### Phase J — Events, rules, notifications (2.5 d)

**J1 · Events and signal history** · 0.5 d · deps: F3 — writes declared numeric signals with
`history: true` to `signal_history`, appends events, prunes on a janitor, exposes `GET /api/v1/events`.
Tests: only declared signals are stored; a signal that changes type is rejected rather than coerced.
**J2 · Rules** · 1.5 d · deps: J1, S3 — `expr` predicate over `signal(card, name)` and `state(card)`
only, + `for:` debounce + severity + resolve; pure evaluator over
`(prev signals, new signals, history, execution state)`.
Also creates: `rule_state` persistence and the three-valued evaluator from §12.
Tests: flapping does not fire; `for:` requires sustained truth; **a rule naming an unknown card or an
undeclared signal is a config-load diagnostic**, not a runtime crash or a silently dead alert; a rule
cannot reach into blocks. Freshness suite, one test per row of the §12 table: a stale card yields
`unknown` and resets the window rather than accumulating it; an absent or null signal yields
`unknown`; a wrong-typed value yields `unknown` and emits one event; an open circuit does not
accumulate; a stale `true` that recovers as fresh `false` resolves; **a restart mid-window resumes
from `rule_state` rather than restarting the timer**; `state()` still alerts through all of it.
**J3 · Notifications** · 1 d · deps: J2 — `Notifier` interface, ntfy and webhook implementations,
outbox with dedupe key, capped retry, per-channel rate limit and hourly ceiling, auto-suspend on flood.
Tests: delivery retried on 5xx and abandoned after N; dedupe suppresses duplicates inside the cooldown;
a notification body never contains a secret; the hourly cap suspends and emits one meta-event.

### Phase K — Homepage importer (2 d)

**K1 · Parse Homepage configs** · 1 d · deps: C1 — `internal/homepageimport/` reading
`services.yaml` (nested groups, `href`, `description`, `icon`, `ping`, `siteMonitor`, `widget`/`widgets`,
`server`/`container`), `bookmarks.yaml`, `settings.yaml`, `widgets.yaml`, and Docker labels
(`homepage.*`, incl. `widgets[n]` indices and dotted header keys) into an intermediate model with a
warning list. Tests: a large real-world corpus in `testdata/homepage/` parses without panic; unknown
widget types produce warnings, not failures.
**K2 · Map and apply** · 1 d · deps: K1, D3 — group→section, service→card, `widget.type`→integration
(a mapping table covering the ~25 most common types, everything else → `http-json` or a link-only card),
`widget.url`→connection (deduplicated by base URL), `widget.key`→a `${secret:…}` reference plus a
printed list of environment variables to set. Applied through `config.Apply` so comments and formatting
survive; imported connections start **disabled pending review** (C-level trust boundary).
AC: `veduta import homepage --dir ./homepage-config` prints
`Imported 34 services · 29 complete · 3 without widgets · 2 need manual configuration`, writes a valid
config, and never writes a secret value into YAML.

### Phase L — Release readiness (3 d)

**L1 · Docs** · 1 d — getting started, configuration reference generated from the JSON Schema,
integration authoring guide (declarative and WASM), security model page, migration guide. ⇉
**L2 · Icon proxy and cache** · 0.5 d — `GET /api/v1/icons/{spec}` resolving `mdi:`, `si:`,
`sh:` (dashboard-icons) and URLs, with a disk cache and an offline pack; never a hard CDN dependency. ⇉
**L3 · Hardening pass** · 1 d — CSP and security headers, `go test -fuzz` on the document validator,
manifest parser and token verifier; the import-boundary test (`integrations/*` may not import
`storage`/`secrets`/`connections` internals); a CI secret-scan asserting no configured secret value
appears in any HTTP response; dependency and vulnerability check (`govulncheck`); load test of 50 cards
on a Raspberry Pi 4 class target.
**L4 · Release 0.1.0** · 0.5 d — multi-arch images, checksums, changelog, generated `THIRD-PARTY-NOTICES.md` covering the frontend and locked Rust plugin dependencies and served from the UI, a "Source" link in the footer satisfying AGPL §13, a short trademark policy, a screenshot generated by
the visual-regression suite, and an explicit "experimental: the plugin ABI will change" note.

---

## Part 3 — Sequencing

**Total effort: ~42 ideal developer-days.** For one developer at public-release quality — review,
iteration, docs, packaging, the things that always appear — **8–12 calendar weeks** is the honest
number. Roughly a quarter of it (the demo path below) is front-loaded, which is what keeps momentum.

**Critical path (the shortest route to a compelling demo):**
`Part 0 → S2 → S4 → A1 → A2 → A3 → B1 → B2 → B3 → B4 → C1 → C2 → D1 → D1b → D2 → D2b → D3 → F1 → E1 → E3` — Immich
photos on a beautiful dashboard behind a credential-free proxy, with route-limited authority. (A3
added to this chain in the D2b/D3/D4/D5 review, 2026-09 - it was missing despite F1, later in the
same chain, depending on it.)
Roughly 16–18 developer-days, demonstrable, screenshot-able, and it validates every architectural
decision that matters. **S1a runs alongside in week one** as an early warning; **S1b and the whole
WASM phase are off this path** — the original plan contradicted itself by placing S1 first while
describing it as only "informing" the broker.

**F1 is now on the critical path, not after it** (found in the D2b/D3/D4/D5 review, 2026-09): E1's
`connection_state` revision and persisted signing key are real §10 SQLite tables, so F1 must land
before E1, not after E3 as this line originally implied. F1's own dependency is only A3 - and only
the HTTP-server portion of A3 that's already landed, not the config-flags/request-id-middleware
portion still open (see E1's own note above) - so pulling it forward costs nothing here that
wasn't already owed. **Then** `F2 → F3 → F4`
makes the rest of persistence and scheduling live, and `S1b → G1 → G2 → G4` makes it extensible.

**Safe to parallelise:**
- Frontend (B1–B5) against backend (C1–D5) — the Widget Document schema (B2) is the only shared
  contract, so land it first and it decouples the two tracks entirely.
- A3/A4 against A2.
- D5 against D3/D4.
- H1/H2 against anything in F/G/I.
- L1/L2 against everything from Phase E onward.
- G3 (SDK) against G4 (Jellyfin plugin), once G2 exists.

**Must stay serial:**
- S1b before G1 (the runtime design depends on the spike's findings); S1a is early but blocks nothing.
- Part 0 before B2, D2, D3, F3, J1 — the four frozen contracts are encoded by all of them.
- B2 before any runtime — everything produces Widget Documents, and changing that schema late
  invalidates every golden test.
- D1b → D2 → D2b → D3: canonicalisation, then enforcement, then approval, then the runtime that
  must obey them. The declarative runtime must not predate the route checks it is subject to.
- D1 → D2 → D3: the security core is layered and each layer's tests assume the one below.
- E1 before E3, F1 before F2 before F3 before F4.
- H2 before any public release announcement.

**Do not start before the architecture is proven** (i.e. before Phase G lands): additional
integrations beyond Immich, Jellyfin, Docker and Glances. The single most likely way to waste a
month is to write twenty declarative manifests against a manifest schema that then changes.

---

## Part 4 — Test strategy

| Layer | Scope | Gate |
| --- | --- | --- |
| Go unit | config, secrets, connections, broker, widgets, rules, scheduler | every PR |
| Fixture/golden | recorded upstream JSON → byte-identical Widget Document, per integration | every PR |
| Security/conformance | the S1b negative suite promoted to CI, plus D1/D2/D2b/E1 denial tests, and the Part 0 adversarial schema corpus | every PR, **never skippable** |
| API integration | `httptest` server + real SQLite in a temp dir, full request paths | every PR |
| Frontend unit | Vitest per block renderer + formatters | every PR |
| E2E | Playwright against `--fixtures` mode | every PR |
| Visual regression | pinned browser/fonts/clock, 3 baselines | every PR, human-approvable diffs |
| Load | 50 cards / 8 connections on an ARM target | before each release |

Two rules worth committing to early: **(1)** every integration ships with recorded fixtures and a
golden document, so nothing requires a running Immich to test; **(2)** every security control gets a
test that proves the *denial*, not just the allow path — a capability check with no negative test is
not a capability check.
