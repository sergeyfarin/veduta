# 03 — Gaps, issues and improvement opportunities

Distinct from [docs/02-implementation-plan.md](02-implementation-plan.md)'s milestone table: that
document is deliberately-scoped, dependency-ordered *planned* work. This one is the opposite kind
of list - things noticed *while* building something else, real enough to write down, not yet worth
their own milestone or not in scope for the milestone that found them. An entry here is a small
debt or an open decision, not a task assignment.

**This file holds only what is still open.** When an entry is closed, move it to
[03-backlog-resolved.md](03-backlog-resolved.md) with an account of where the fix landed, rather
than deleting it - so the history of "we knew about this since when" survives. Source comments
pointing at `docs/03-backlog.md` for a gap that has since been closed will find it there.

Each entry: what the gap is, which milestone found it, why it wasn't fixed there, and its rough
priority (which milestone should absorb it, or "before X" for a hard blocker).

| Open item | Area | Priority |
| --- | --- | --- |
| [Does WASM add enough value to justify further development?](#does-wasm-add-enough-value-to-justify-further-development) | Integrations | Due: M3 landed 2026-09-29 |
| [Should there be a frontend plugin surface at all?](#open-question-should-there-be-a-frontend-plugin-surface-at-all) | Frontend | Open decision |
| [Action execution is not implemented](#action-execution-is-not-implemented) | Product/security | Display-only through 0.2; design first |
| [Appearance options are deferred until asked for](#appearance-options-are-deferred-until-asked-for) | Frontend | On demand |
| [The visual baseline's per-pixel threshold hides whole-area changes](#the-visual-baselines-per-pixel-threshold-hides-whole-area-changes) | Frontend | Medium |
| [Asset format coverage is narrower than the allowlist](#asset-format-coverage-is-narrower-than-the-architectures-final-allowlist) | Assets | 0.2 transform milestone |
| [Asset-cache startup does not reconcile orphan files](#asset-cache-startup-does-not-reconcile-orphan-files) | Assets | Low |
| [Lock-write race against a concurrent CLI approval](#the-lock-write-race-is-closed-only-within-one-process-not-against-a-concurrent-cli-approval) | Integrations | Low |
| [A pipeline step cannot read 404 as absent](#a-pipeline-step-cannot-read-404-as-absent) | Manifest DSL | Low; on a second case |
| [`CacheEntries` can't represent an explicit zero](#manifestloadlimitscacheentries-cant-represent-an-explicit-zero) | Integrations | When declarative caching lands |
| [Approve flow can't grant a limit above its default](#the-documented-approve-flow-has-no-way-to-grant-a-limit-above-its-documented-default) | Docs | Low |
| [Go plugin SDK needs a WASI-free toolchain](#go-plugin-sdk-requires-a-maintained-wasi-free-toolchain) | Plugins | Parked; subject to WASM review |
| [ARM runtime performance is measured on arm64 only](#arm-runtime-performance-is-measured-on-arm64-only) | Plugins | armv7 deferred |
| [Markdown has no authoring syntax for structure](#markdown-is-prose-only-with-no-authoring-syntax-for-structure) | Frontend | Deferred |

---

### Does WASM add enough value to justify further development?

**Parked, 2026-09-16. Requirements under review.** The runtime and Rust SDK exist,
but that alone does not establish a product need for further WASM development.
Jellyfin can be expressed declaratively, and Immich memories now uses that approach. No new WASM
integration, replacement proof case, SDK expansion or runtime feature is scheduled
for 0.1 or automatically committed to 0.2. Keep existing functionality and its
security/regression coverage maintained while this question is open.

Reopen only around a concrete user need. Compare a declarative implementation,
an upstream service that already does the work, and a WASM implementation.
Record the additional behavior WASM enables, its build/maintenance and resource
costs, and any broker or permission changes. Continuing to defer or reducing
WASM's role are valid outcomes. A parser's compatibility with the WASI-free
sandbox must be demonstrated before treating it as an implementation choice.

Candidates discussed, **not scheduled**:

- Provider-independent ICS agenda: accept compatible calendar subscriptions
  rather than couple the feature to Nextcloud. Recurrence, exceptions and
  timezone handling could justify WASM; the existing list block can show an
  agenda. Provider-specific authenticated APIs are a separate scope.
- GitHub releases/activity: prefer declarative JSON. Installed-version and
  deployment-update knowledge should come from services such as Dockhand.
- Rain forecasts: prefer declarative JSON when available. A text-feed parser
  could justify a small plugin; probability and intensity remain distinct.
  Forecast visualization is a core rendering concern, independent of WASM.
- Energy monitoring: use existing Home Assistant sensor data or an upstream
  JSON API first. Direct Prometheus exposition parsing is a possible WASM use
  case; long-term energy accounting remains upstream.

The 0.1 decision to retain the existing Jellyfin implementation is unchanged;
it is not evidence that future integrations need WASM. See
[decision 0003](decisions/0003-jellyfin-wasm-proof-case.md).
Priority: parked until a requirements review after 0.1, not a release blocker.

**Decided, 2026-09-29:** the review runs after the native time-series block (Phase M3) lands, not
before. The rain and energy candidates above need a way to draw a series before the question of
how to fetch one can be judged, and without that block the comparison would be against a card that
cannot show the result.

### Open question: should there be a frontend plugin surface at all?

Raised while deciding whether plugins must live inside the single binary (they need not - see
[03-backlog-resolved.md](03-backlog-resolved.md)). The backend answer generalises badly to the
frontend, and the question deserves recording rather than re-deriving.

Today there is no frontend plugin surface, by two frozen decisions: D2 makes the Widget Document
the *only* integration output, and the renderer is "trusted and closed" - one Svelte component per
block type, a registry keyed by `type`, unknown types rendering a labelled placeholder, and no
`{@html}` ever. A new block type is a deliberate core change, which is what keeps a compromised or
merely careless integration from reaching the DOM.

Three shapes were considered, none adopted:

- **Keep it closed** (the status quo). New block types stay core changes. Costs nothing, and the
  placeholder path means an old frontend degrades rather than breaks against a new block type.
- **Build-time component plugins** - a plugin ships a Svelte component compiled into the SPA. No
  untrusted runtime code, but it breaks the symmetry that makes the backend answer work: it is not
  side-loadable at all, since adding one means rebuilding the frontend and therefore the binary.
- **A sandboxed iframe block type** - plugin-supplied HTML/JS in a locked-down frame over
  postMessage. Genuinely side-loadable, and by far the largest item: a new threat model, a new
  protocol to version, and a direct challenge to D2, which is frozen precisely because the security
  and consistency story rests on it.

Deferred, deliberately, with no work planned. Revisit only if a concrete integration cannot be
expressed as a Widget Document *and* the missing block type is too specific to justify adding to
the core renderer - that pair is the trigger, and neither half has been observed yet. Until then
the answer is the first option.

### Action execution is not implemented

The Widget Document can describe action controls, but the frontend intentionally renders every
one disabled. There is no end-to-end action endpoint or execution flow. Earlier README and
changelog language described Veduta as performing approved actions, which turned a product
direction into a claim about current behaviour.

Do not enable the controls by wiring them directly to an integration operation. The design must
first specify authentication and authorisation at execution time, approval binding, replay and
cross-site request protections, user confirmation for consequential operations, bounded request
inputs, failure reporting, and a persistent audit trail. It must also define which actions belong
in a home dashboard at all.

Priority: design before implementation; not part of the first alpha unless it is separately
scoped, reviewed, and release-gated. Until then, documentation must describe action blocks as
display-only.

**Decided, 2026-09-29:** action controls stay display-only through 0.2. No 0.2 milestone implements
execution, and the next step when it is picked up is a design document covering the list above,
not code.

### Appearance options are deferred until asked for

Recorded by [decisions/0002-theming-and-visual-customisation.md](decisions/0002-theming-and-visual-customisation.md).
Veduta ships two fixed presets - `dashboard.appearance: clean | veil` - and no public knobs. A
preset privately owns its surface opacity, scrim, border alpha, shadow and blur.

Amended 2026-09-13, when two of that ADR's own revisit triggers fired. The preset is now a
per-viewer choice with `dashboard.appearance` as the instance default - stored in the viewer's
`localStorage` exactly as light/dark is, so no server-side preference store appears and D5 is
untouched - and Veil's backdrop is two bundled public-domain vedute instead of a generated
gradient. Neither adds a public knob; what is still deferred is everything below.

This entry exists so the reasoning is not relitigated. An earlier draft of that ADR planned to
build Veil, observe which design tokens differed from Clean, and promote those to typed config.
That was rejected on review: it derives a public API from an implementation diff, exposes coupled
design mechanics as if they were independent user choices, and recreates exactly the
combinatorial validation problem the fixed-preset design avoids. Config expresses intent; tokens
are implementation.

If options are ever added they arrive one at a time, on real demand, in that intent-shaped form -
a named accent palette rather than a hex pair (a single hex cannot clear 3:1 in both colour
schemes; only 32.8% of sRGB can, and the shipped light accent misses on dark by 0.01), a
local or proxied background reference rather than an arbitrary URL, possibly a small set of
bundled font stacks. A preview UI waits until there are enough real choices to be worth
previewing.

Two gaps the 2026-09-13 amendment leaves open, neither blocking:

- **A configured `dashboard.background` still shows less of itself than the bundled paintings do.**
  Its scrim is 0.70 against the bundled 0.18, and the gap is not arbitrary: the bundled files are
  in the repository, so their floors are proven against their actual pixels, while an arbitrary
  image can only be defended worst-case. The honest workaround is documented rather than built -
  tone the image down before configuring it, which is what the bundled ones do. A build step that
  did that toning for the operator is the real fix, and would need a decode/re-encode path the
  asset milestone is already planning. Priority: when that milestone lands.
- **The Canaletto ships at 956x640**, the largest reproduction on Commons of that exact painting.
  It is soft on a large display, and more so now that the scrim only holds back 18% of it. Not
  visible in review at 1440px, plausible above that. Replace it if a higher-resolution
  public-domain scan appears.

Priority: on demand. Nothing here blocks anything; the entry is the record of a decision not to
build, which is easy to forget and expensive to rediscover.

### The visual baseline's per-pixel threshold hides whole-area changes

Found while replacing Veil's backdrop with the bundled paintings. The new backdrop rendered, the
suite compared it against the old gradient baseline, and **it passed** - so did Clean's two
baselines, which had gained a button in the header.

`playwright.config.ts` tunes `maxDiffPixelRatio` to 0.02 with a careful account of why, but leaves
pixelmatch's per-pixel `threshold` at its default of 0.2. That default is generous in exactly the
places a backdrop lives: low-contrast, low-saturation, large. Measured on the two baselines:
**91.58% of pixels differed, and 0.02% of them differed far enough to be counted** - three orders
of magnitude inside a budget that reads, in the config comment, as if it were the whole story.

The budget is not the problem; the config comment's own evidence (a deliberate one-line CSS change
moving "24-36% of pixels") is about a change that alters edges, where the per-pixel threshold is
not the binding constraint. A wash across a large area is the case it does not cover.

Not fixed here, because lowering `threshold` interacts with the inter-container font-rendering
noise the 2% ratio was measured against, and retuning a suite that several baselines depend on is
not a side effect a backdrop change should have. In the meantime the two bundled backdrops are
asserted functionally instead - `veil {light,dark} loads its bundled backdrop` in
`web/tests/visual.spec.ts` checks the computed `--v-backdrop-image` and fetches it - which covers
the failure that matters most (a 404 leaving a blank preset) without touching the thresholds.

**Seen again on a real change, 2026-09-29.** Adding the Climate showcase card for the `series`
block - a whole new two-by-two card on a page of fourteen - regenerated only the mobile baseline;
all four desktop screenshots *passed* against the old images, because the card filled an empty
grid slot whose background differs from a card surface by a few shades, and its lines are thin.
The baselines were force-regenerated (`--update-snapshots=all`). The same investigation found the
footer rendered the build's `git describe`, so a baseline depended on the checkout's tags and
dirtiness and CI had been rendering a different version string all along; the suite now builds
with a pinned version, and a fresh container re-verified the new baselines. That fixes the
nondeterminism, not the threshold. This raises the entry's priority: a new card is exactly the
change the suite should notice.

Priority: medium (raised from low-medium). It does not affect what ships, only how much CI notices. The fix is a
measured `threshold` chosen the way the ratio was: regenerate in the pinned image, re-verify in a
fresh container, and record the noise floor for the new pair rather than assuming the old one
transfers.

### Asset format coverage is narrower than the architecture's final allowlist

E1 deliberately trusts byte sniffing plus `image.DecodeConfig`, never an upstream Content-Type.
The standard-library decoders cover PNG, JPEG and GIF; WebP and AVIF are therefore rejected rather
than accepted without dimension validation. Allowed transform tokens are validated but 0.1 remains
pass-through as the architecture permits. Priority: the 0.2 transform milestone - add audited
decoders/fuzz cases for WebP and AVIF, then implement resize/re-encode for the existing allowlist.

### Asset-cache startup does not reconcile orphan files

E2 detects a corrupt/missing file on lookup and refetches it, but a crash after atomic rename and
before the SQLite metadata write can leave an unreferenced file in the cache directory. It is
unreachable and not a correctness/security issue, but it is not counted by LRU eviction. Priority:
low; add a startup sweep comparing directory names with `asset_cache` rows.

### The lock-write race is closed only within one process, not against a concurrent CLI approval

Found in the D2b/D3/D4/D5 review (2026-09), fixed partially: `internal/api.Server` now serialises
concurrent `POST .../approve` requests with `approveMu sync.Mutex` (see
`TestIntegrationApprove_ConcurrentApprovalsOfDifferentIntegrationsDoNotLoseAnUpdate`), closing the
read-modify-write race between two REST requests hitting the same running process. It does not
close the race between the REST API and a concurrent `veduta integration approve` CLI invocation -
a separate process with its own in-memory view of `veduta.lock.yaml`, holding no lock the API
process could see. Two approvals landing at the same instant, one via each path, can still lose an
update the same way two REST requests used to. Priority: low (this requires an operator to be
running the CLI and the API against the same lock file at the same instant, a narrow window) but
worth closing whenever the lock file gains a real writer abstraction - an OS file lock
(`flock`/`LockFileEx`) around the read-modify-write in whatever function both the CLI and the API
ultimately call would close it without either caller needing to know about the other.

### `manifestload.Limits.CacheEntries` can't represent an explicit zero

Found reviewing D3. `manifestload.Limits` (and the `rawManifest`/lock-derived construction in
`internal/integrations/declarative/runtime.go`'s `Load`) uses plain `int` fields; `WithDefaults`
treats `0` as "not set" for every field, including `cacheEntries`, whose schema minimum is `0` -
the one limit field where an explicit zero ("no caching") is a real, legitimate, distinct value
from omission. `internal/integrations.Limits` (D2b) was deliberately built with pointer fields to
preserve exactly this distinction; `manifestload.Limits` reintroduces the bug D2b's design
avoided. Dormant today: `internal/integrations/declarative` never calls `Broker.CacheGet`/
`CachePut` at all, since neither the pipeline-step grammar nor the four-node template grammar has
a cache-triggering construct - a manifest cannot presently ask for caching, so nothing observes
the wrong default. Priority: whenever declarative caching is wired in (no milestone currently
owns this) - fix by making `manifestload.Limits` pointer-fielded like D2b's, or by threading a
"was this key present" bit alongside the plain `int` some other way.

### The documented approve flow has no way to grant a limit above its documented default

Found while implementing D2b's `Grants`/`ReconcileAtApproval`: `internal/contracts/semantic.go`
(part of the contract suite since before D2b, checked against `examples/veduta.lock.yaml`) already
encodes `effective(k) = min(core maximum, manifest value-or-default, approved value-or-default)`
where the *approved* side comes from the lock entry's own optional `limits:` field, independently
of what the manifest requests - an integration approved with no such override stays at the
documented default no matter what its manifest asks for (glances requests `timeoutMs: 4000`, but
its lock entry needed an explicit `limits: {timeoutMs: 4000}` to actually be granted that, which
`examples/veduta.lock.yaml` was missing until this milestone added it - a real, previously
uncaught fixture bug this reconciliation logic caught the moment it existed to check it).
docs/01-architecture.md section 6's shown `POST /approve` body (`{expectedManifestSha256, grants:
{capabilities[], routes[]}}`) has no `limits` field at all, so `internal/integrations.Grants` adds
one as an additive, non-breaking field; the CLI and the REST handler's own default both echo the
manifest's full requested limits back as that override (matching "approve everything requested",
the same default behaviour routes and capabilities already have), but a client (or an admin
hand-editing the lock file) can narrow it, mirroring the subset-approval behaviour the architecture
doc describes for routes and capabilities. Not a defect needing a fix, but worth recording because
the architecture doc's own illustrative request body does not show this field, and a future reader
implementing a second client from that prose alone would miss it.
Priority: low - clarify in `docs/01-architecture.md` section 6 the next time that section gets a
deliberate revision, so the illustrative POST body includes the optional `limits` field.

### Go plugin SDK requires a maintained WASI-free toolchain

Found during G3. The stock Go Extism guest was 4.3 MiB, took about five seconds
to cold-validate on the development host, and imported 17 WASI functions. That
conflicts with S1's strict no-WASI sandbox and the size/latency goals for
Pi-class hosts. Enabling WASI only for this toolchain would widen the sandbox,
while owning a custom PDK would create an ongoing compiler/ABI maintenance
burden. G3 therefore ships Rust as the supported guest language. Priority: parked
under the [WASM requirements review](#does-wasm-add-enough-value-to-justify-further-development).
Only revisit if that review establishes a need and an official or maintained Go PDK can emit `wasm32-unknown-unknown`
with no forbidden imports and passes the G1 conformance suite plus S1a ARM
budgets. The Go backend is a separate decision and remains in place; see
`docs/decisions/0001-backend-language.md`.

### ARM runtime performance is measured on arm64 only

G1's sandbox half was always evidenced: the conformance suite is real and runs in CI - no
filesystem, no env, no sockets, no native HTTP, deadline kill, memory trap, output cap, corrupted
module rejected before compile. None of it measures speed or memory, and the ARM scheduler test
drives synthetic callbacks, so it exercised scheduling on ARM rather than WASM on ARM: it never
compiled a real module and never ran Jellyfin's.

The three figures S1a was written to produce - cold compilation of a real plugin, warm invocation,
resident memory per instance - therefore had no number attached on ARM hardware, and they are the
ones that decide whether a Pi-class host is a supported target or an aspiration.

**arm64 is measured, 2026-09-17, and it passes with an order of magnitude of headroom.** The
deferral of the day before assumed this needed a board nobody has. It did not: L3's load gate was
already running on GitHub's native four-core ARM64 runner, and that job now runs
[`TestPluginRuntimePiClassBudget`](../internal/integrations/wasm/budget_test.go) beside it. The
test loads `plugins/jellyfin/jellyfin.wasm` - the committed module, the fixtures the golden test
pins, the limits a real approval grants - and measures cold compilation, compilation from a warm
cache, warm invocation over 24 calls, and resident growth per additional loaded instance, read
from `/proc` rather than from Go's heap statistics, since wazero's machine code is mapped and not
on the heap. It asserts S1b's own kill criteria: 50 ms warm invocation, 20 MB resident per
instance, plus a five-second ceiling on cold compilation that S1b never set but a container start
deserves. `-v` is deliberate - the job log carries the numbers, not just the verdict. Because the
release workflow's gate requires a green CI run for the exact tagged commit, these budgets are
release-gating without any further wiring.

The measured figures, from run
[35182718050](https://github.com/sergeyfarin/veduta/actions/runs/35182718050) on
`ubuntu-24.04-arm`, four cores, `GOARCH=arm64`:

| Figure | arm64 | Budget | Headroom |
| --- | --- | --- | --- |
| Cold compile, empty cache | 244 ms | 5 s (local ceiling) | 20× |
| Compile from a warm cache | 12 ms | — | — |
| Warm invocation, median of 18 | 1.765 ms | 50 ms (S1b) | 28× |
| Warm invocation, worst of 18 | 1.993 ms | — | — |
| Resident per added instance | 1.0 MiB | 20 MB (S1b) | 20× |
| Process peak resident | 46.8 MiB | — | — |

So the kill criteria are not close to being tested, which is the answer a spike hopes for: an
in-process WASM runtime on a Pi-class arm64 host costs about two milliseconds and a megabyte per
integration. The arm64 runner is in fact *faster* than the x86 development machine these were
first taken on (244 ms against 470 ms cold, 1.8 ms against 3.3 ms warm), so a slower board would
have to be an order of magnitude worse than this runner before anything here came under pressure.
Cold compilation is worth the persistent cache directory: 244 ms down to 12 ms on a restart.

These numbers reached CI only after the repository went public on 2026-09-17. Every job had
aborted in six seconds since 2026-09-15 on a GitHub billing block, which is why the gate sat
instrumented but unverified for a day; the block was account-wide and survived the visibility
change until the payment state cleared, so it was never a minutes-quota problem.

**What is still open is armv7 only.** GitHub has no 32-bit ARM runner, so the images published for
`linux/arm/v7` carry a WASM runtime whose cost on that architecture is unmeasured - and 32-bit is
where it is most likely to differ, since wazero's compiler and the guest's address space both
change shape there. qemu would measure the emulator. Resolving it needs real hardware, or dropping
armv7 to a build target that is published without a performance claim. Priority: after 0.1, and
before describing armv7 Pi-class hosts as a supported target for WASM integrations. Declarative
integrations are unaffected on every architecture; they never enter the WASM runtime.

Nothing user-facing over-claimed this in the meantime: the README, `docs/getting-started.md` and
`docs/docker.md` promise multi-arch *images*, never Pi-class *performance*.

### Markdown is prose-only, with no authoring syntax for structure

Found 2026-09-16, same assessment. `blockText`'s markdown kind supports emphasis, inline code,
links and lists inside a 2 KB cap ([markdown.ts](../web/src/lib/markdown.ts)), which is the right
subset for prose and nothing more. There is no way for a human writing `veduta.yaml` to express
structure - two metrics side by side, a labelled group, a callout - without a card type existing
for it already.

The generic directive convention (`:::name`, as used by MyST and remark-directive) is the obvious
candidate if this is ever picked up: it is an existing convention rather than a Veduta dialect, a
directive parses to a named node with options rather than to markup, and unknown directives can
degrade to the same labelled placeholder unknown block types already get. It would compile into
the existing block tree, adding an authoring surface rather than a rendering one.

Two things to be clear about before any of it is built. Veduta's renderer is closed by D2, so a
directive can only ever name a block the core already implements - the directive layer does not
widen what can reach the DOM, and must not be allowed to. And the client-side binding/expression
layer such proposals usually come with (`$server.cpu`) is not wanted: the scheduler resolves values
server-side before a document is published, so a second expression evaluator in TypeScript would
add a sandbox to maintain for a problem `expr` already solves in Go, in the better place.

Deferred with no work planned. The trigger to revisit is a concrete authoring request that the
current card types cannot express, not the general appeal of the idea - recorded as a revisit
condition on [decision 0004](decisions/0004-card-expressiveness-and-the-presentation-contract.md),
which also records why the binding layer such proposals arrive with is not wanted.

### A pipeline step cannot read 404 as absent

Found 2026-09-29 in Phase M2, when Home Assistant's `sensor` moved from the whole `/api/states` to
`/api/states/{entityId}`. A renamed or deleted entity used to render "no such entity" with no
signals; it is now a 404, and a non-2xx pipeline response fails the invocation, so the card shows
`returned HTTP 404` instead. That is still honest - the card is in error, emits no signals, and a
rule over it reads unknown - but the message is the upstream's status rather than the reason.

`docs/01-architecture.md` already names this as the trigger for a per-step opt-out ("if an
integration ever genuinely needs to read 404 as absent, that is the point to add one, deliberately
and per step"). One case is not yet enough to design it well: whether the step binds `nil`, a
sentinel, or skips the rest of the pipeline changes every expression downstream of it. Not built in
M2 because the change was about paths, and a status opt-out is a separate DSL decision.

Priority: low. Revisit when a second integration needs it, or if "HTTP 404" on a sensor card turns
out to confuse people in practice.

