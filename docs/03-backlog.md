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
| [Does Jellyfin still earn being the WASM proof case?](#open-question-does-jellyfin-still-earn-being-the-wasm-proof-case) | Plugins | Open decision |
| [Should there be a frontend plugin surface at all?](#open-question-should-there-be-a-frontend-plugin-surface-at-all) | Frontend | Open decision |
| [The approval API has no client](#the-approval-api-has-no-client) | Frontend | Open decision |
| [Appearance options are deferred until asked for](#appearance-options-are-deferred-until-asked-for) | Frontend | On demand |
| [The visual baseline's per-pixel threshold hides whole-area changes](#the-visual-baselines-per-pixel-threshold-hides-whole-area-changes) | Frontend | Low-medium |
| [Asset format coverage is narrower than the allowlist](#asset-format-coverage-is-narrower-than-the-architectures-final-allowlist) | Assets | 0.2 transform milestone |
| [Asset-cache startup does not reconcile orphan files](#asset-cache-startup-does-not-reconcile-orphan-files) | Assets | Low |
| [Lock-write race against a concurrent CLI approval](#the-lock-write-race-is-closed-only-within-one-process-not-against-a-concurrent-cli-approval) | Integrations | Low |
| [`httpConnection.headers` are not secret-capable](#config-httpconnectionheaders-values-are-plain-strings-not-secret-capable) | Config schema | Low-medium |
| [A pipeline path cannot carry a card parameter](#a-pipeline-path-cannot-carry-a-card-parameter) | Manifest DSL | 0.2 |
| [`CacheEntries` can't represent an explicit zero](#manifestloadlimitscacheentries-cant-represent-an-explicit-zero) | Integrations | When declarative caching lands |
| [Approve flow can't grant a limit above its default](#the-documented-approve-flow-has-no-way-to-grant-a-limit-above-its-documented-default) | Docs | Low |
| [Go plugin SDK needs a WASI-free toolchain](#go-plugin-sdk-requires-a-maintained-wasi-free-toolchain) | Plugins | Low |
| [The lock file is written 0600, but is meant to be committed](#veduta-lock-yaml-is-written-0600-by-a-uid-the-operator-is-not) | Deployment | Low |
| [ARM runtime performance has no measured evidence](#arm-runtime-performance-has-no-measured-evidence) | Plugins | Deferred past 0.1 |
| [No visualisation block, but the README promises charts](#native-visualisation-blocks-do-not-exist-and-the-readme-promises-one) | Frontend | **README before 0.1**, block 0.2 |
| [Markdown has no authoring syntax for structure](#markdown-is-prose-only-with-no-authoring-syntax-for-structure) | Frontend | Deferred |

---

### A pipeline path cannot carry a card parameter

Found while writing `plugins/arcane` and `plugins/homeassistant`. A v1 pipeline step's `path` must
be a literal string: `internal/contracts/semantic.go` refuses an expression-valued path because
route coverage would otherwise stop being decidable at approval time, which is the point of
declaring routes at all. The manifest schema's `valueNode` allows an expression there, and the
declarative runtime evaluates one perfectly well - the semantic layer is what says no. That
mismatch is itself worth resolving in either direction.

The cost is concrete in two shipped integrations:

- **Arcane** scopes everything by environment id as a path segment (`/api/environments/{id}/...`),
  so `plugins/arcane` watches the local environment (`0`) only. A remote host or agent added to
  the same Arcane cannot be given a card.
- **Home Assistant** serves one entity at `/api/states/<entity_id>`. `plugins/homeassistant`'s
  `sensor` operation instead reads the whole `/api/states` array and selects one entity from it -
  correct, but it transfers the entire state set to render one number, which on a large
  installation is megabytes per refresh per card.

A middle way exists and is probably the right shape: let a route declare named path segments whose
values come from `params` under a declared pattern (`/api/environments/{environmentId}/containers`
with `environmentId` constrained by the operation's own params schema). Coverage stays decidable -
the glob is still fixed, and the substitution is bounded by a schema the approver can read - while
the path stops being a template the checker cannot reason about. Worth doing before more
integrations are shaped around the limitation rather than around their upstream.

### Open question: does Jellyfin still earn being the WASM proof case?

After the Approach-A rewrite (recorded in [03-backlog-resolved.md](03-backlog-resolved.md)), the
Jellyfin plugin no longer does "send auth header →
resolve the current user → list": it does one sorted `GET /Items`, two typed `GET /Items` counts
and `GET /Sessions`, then folds them into one document.

**Measured, 2026-09-12.** A throwaway declarative manifest was built against the current DSL and
run over the plugin's own fixtures, so the rest of this entry is evidence rather than estimate.
Two of the three limitations named above are not real: the four-call fan-out works as four ordinary
pipeline steps (`plugins/glances` already fans out to six), and per-item poster handling works via
`filter(recent.items, .imageTags != nil && "Primary" in .imageTags)`. What actually blocks a
rewrite was three general DSL gaps, none of them specific to Jellyfin and all three worth fixing on
their own merits.

**Two are now closed** (both in [03-backlog-resolved.md](03-backlog-resolved.md)): an asset node is
a value, so an image can carry `alt` and `aspect`; and `{ if: …, then: … }` omits a key or element,
which covers both the missing production year and the missing-image notice. **The third is closed too**: a non-2xx pipeline
response now fails the invocation, matching what the wasm plugin has always done.

What is left is therefore no DSL gap at all, only one deliberate reduction: pinning the CamelCase profile drops
the plugin's PascalCase tolerance. Spike S2 verified a real Jellyfin 12 honours the explicit
profile, so that is a defensible trade rather than a regression - but it is a decision to take
knowingly. `internal/integrations/wasm/jellyfin_scenarios_test.go` pins the behaviour any
replacement has to reproduce (missing poster, missing year, PascalCase upstream, failing
sub-request). Not blocking anything.

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

### The approval API has no client

Noticed 2026-09-14, from the reasonable expectation that a dashboard with a login has a settings
page behind it. It does not, and for the config half that is decision D5 working as intended -
YAML is the single source of truth for 0.1, and the editor is a 0.3 frontend project over
`config.Store.Apply` (architecture section on Writes, and challenge C6). Nothing to fix there.

The integrations half is a different situation. D2b built `GET /api/v1/integrations`,
`GET /api/v1/integrations/{id}/approval` and `POST /api/v1/integrations/{id}/approve`, and H2 gated
them properly: a fresh sudo window in password mode, the configured admin group under forward auth,
`privilegedOperations: cli-only` by default in forward mode. D2b's own acceptance criteria are
written in terms of a UI - "the UI shows a human-readable applicability report". That UI was never
built, and no milestone owns it.

Two consequences, neither fatal:

- **Approving is CLI-only in practice**, including in password mode where the REST path is fully
  authorized. An operator running the container without shell access has an endpoint they can only
  reach with `curl` and a hand-managed sudo window.
- **The REST approve path has no real client**, so it is exercised only by its own tests. The
  handler is well covered, but nothing proves the sequence a browser would actually perform -
  preview, sudo, approve with `expectedManifestSha256`, handle the 409 on a stale digest.

This is not the 0.3 config editor and does not need D5 reopened: approval writes `veduta.lock.yaml`,
which is already a machine-written file, not the operator's commented config. It is a page over
three endpoints that already exist.

Priority: open decision. The question is whether 0.1 ships a read-only integrations view (cheap,
makes the state visible, leaves approval at the CLI where the security model is most defensible),
the full approve flow, or nothing - and whether a settings entry point in the header is wanted at
all before there is more than one thing behind it.

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

Priority: low-medium. It does not affect what ships, only how much CI notices. The fix is a
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

### Config: `httpConnection.headers` values are plain strings, not secret-capable

`schemas/config.v1.schema.json`'s `httpConnection.headers` (line ~556) is
`additionalProperties: {type: string}`, while `notifications.channels.*.webhook.headers` (line
~390) is `additionalProperties: {$ref: secretRef}`. An admin can put `${secret:NAME}` in a webhook
header but not in a custom HTTP connection header - so an upstream API that authenticates via a
non-standard header (not one of the schema's typed `auth.type` values) forces the credential into
the config file in plaintext. Found while implementing C1, reading the schema closely enough to
notice the asymmetry; not fixed there because it is a schema change (needs re-validating existing
configs, arguably a design call, not C1's own scope of "build the loader for the schema as it
exists"). Priority: low-medium, whenever `schemas/config.v1.schema.json` next gets a deliberate
revision - do not roll it into an unrelated milestone's diff.

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
burden. G3 therefore ships Rust as the supported guest language. Priority: low,
revisit when an official or maintained Go PDK can emit `wasm32-unknown-unknown`
with no forbidden imports and passes the G1 conformance suite plus S1a ARM
budgets. The Go backend is a separate decision and remains in place; see
`docs/decisions/0001-backend-language.md`.

### `veduta.lock.yaml` is written 0600 by a uid the operator is not

Found while testing `compose.yaml` against a real approval, 2026-09-12. `integrations.WriteLock`
creates the file mode `0600`, owned by whoever ran the approval - uid 65532 in a container. The
file's own header tells the reader to "commit it alongside veduta.yaml", and its content is
digests, capabilities, routes and limits with no secret in it, so the operator being unable to
read their own record of granted authority without `sudo` is friction with no security return.

The counter-argument is real and is why this is not simply a bug: the lock file *is* the grant
record, so anyone who can write it can widen an integration's authority, and `0600` owned by the
runtime user is a defensible answer to that. But `0600` restricts reading as well as writing, and
`0644` would keep the write restriction while letting the operator diff and commit it.

Priority: low, and a decision rather than a fix - settle it the next time the approval flow is
touched. Documented as a consequence in `docs/docker.md` in the meantime.

### ARM runtime performance has no measured evidence

G1 is marked **DONE (sandbox; ARM performance acceptance deferred past 0.1)** in
[docs/02-implementation-plan.md](02-implementation-plan.md), and that half is still unmeasured.
The sandbox conformance suite is real and runs in CI - no filesystem, no env, no sockets, no
native HTTP, deadline kill, memory trap, output cap, corrupted module rejected before compile.
None of it measures speed or memory. The ARM scheduler test drives synthetic callbacks, so it
exercises scheduling on ARM rather than WASM on ARM: it never compiles a real module, and it never
runs Jellyfin's.

What has no number attached, on ARM hardware: cold compilation of a real plugin, warm invocation,
and resident memory per instance. Those are precisely the figures S1a's budgets were written
against, and precisely the ones that decide whether a Pi-class host is a supported target or an
aspiration.

**Decided 2026-09-16: taken out of the 0.1 gate and deferred, for want of hardware.** No
ARM board is available to measure against, and the alternatives - buying one, or accepting qemu
numbers that inflate compile time and would need their own invented threshold - are not worth
holding a tag for. 0.1 therefore ships with the WASM runtime's ARM cost unmeasured, deliberately
and on the record here, rather than by nobody choosing.

Nothing user-facing is being over-claimed by that: the README, `docs/getting-started.md` and
`docs/docker.md` promise multi-arch *images*, never Pi-class *performance*, so no release-note
correction is owed. The constraint this entry now carries is forward-looking - **do not start
describing Pi-class hosts as a supported target for WASM integrations until the numbers exist.**
Declarative integrations are unaffected; they never enter the WASM runtime.

When it is picked back up, the cheapest route is already half-built: L3's load gate runs the real
50-card workload on GitHub's native four-core ARM64 runner under a five-second Pi-class budget, so
native ARM CI hardware is in the pipeline today. Extending that job with S1a's three figures -
cold compilation of a real plugin, warm invocation, resident memory per instance - would settle
arm64 without owning a board. It would not settle armv7, which has no native runner. Resolving it
means recording those numbers in the G1 row and dropping the qualifier; the S1b kill criteria
(>50 ms warm invocation, >20 MB RSS per instance) stay the thresholds to judge them against.
Priority: after 0.1, and before any claim of Pi-class support.

### Native visualisation blocks do not exist, and the README promises one

Found 2026-09-16 while assessing an external design recommendation to embed Mermaid and Vega-Lite
as card content. The assessment's incidental finding matters more than its subject: the Widget
Document has no `chart`, `sparkline` or `gauge` block. `chart` is in fact the string the renderer
tests use as their example of an *unknown* block type
([BlockRenderer.test.ts](../web/src/lib/blocks/BlockRenderer.test.ts), `UnknownBlock.test.ts`).

The data for one is already there and already declared. J1 retains bounded numeric history for
declared signals, and `schemas/plugin-manifest.v1.schema.json`'s own description of that retention
says it is "for `for:` windows and sparklines" - so the schema has been promising a renderer that
was never built.

A native block is the shape that fits: points in, an SVG element tree out, drawn by a Svelte
component like every other block. No library, no injection sink, no new threat model. Mermaid and
Vega-Lite are the shape that does not - both render by constructing DOM or SVG themselves and
inserting it, which needs either `{@html}` (failed in CI by
[no_html_directive_test.go](../internal/contracts/no_html_directive_test.go)) or the sandboxed
iframe block already considered and rejected under
[should there be a frontend plugin surface at all?](#open-question-should-there-be-a-frontend-plugin-surface-at-all).
Recording that here so the comparison is not re-derived the next time someone asks about Mermaid.

**The README half is a hard pre-0.1 item.** README.md's opening sentence sells Veduta as showing
"photos, posters, camera frames, charts - not just numbers". Three lines below it the page is
scrupulous about actions not being executable in 0.1. The charts claim should be corrected or
dropped before the tag, whichever way the block itself is scheduled.

Priority: **README wording before 0.1**; the block itself 0.2.

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
current card types cannot express, not the general appeal of the idea.
