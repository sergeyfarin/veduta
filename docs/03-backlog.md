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
| [Appearance options are deferred until asked for](#appearance-options-are-deferred-until-asked-for) | Frontend | On demand |
| [Asset format coverage is narrower than the allowlist](#asset-format-coverage-is-narrower-than-the-architectures-final-allowlist) | Assets | 0.2 transform milestone |
| [Asset-cache startup does not reconcile orphan files](#asset-cache-startup-does-not-reconcile-orphan-files) | Assets | Low |
| [Lock-write race against a concurrent CLI approval](#the-lock-write-race-is-closed-only-within-one-process-not-against-a-concurrent-cli-approval) | Integrations | Low |
| [`httpConnection.headers` are not secret-capable](#config-httpconnectionheaders-values-are-plain-strings-not-secret-capable) | Config schema | Low-medium |
| [`CacheEntries` can't represent an explicit zero](#manifestloadlimitscacheentries-cant-represent-an-explicit-zero) | Integrations | When declarative caching lands |
| [Approve flow can't grant a limit above its default](#the-documented-approve-flow-has-no-way-to-grant-a-limit-above-its-documented-default) | Docs | Low |
| [Go plugin SDK needs a WASI-free toolchain](#go-plugin-sdk-requires-a-maintained-wasi-free-toolchain) | Plugins | Low |
| [The lock file is written 0600, but is meant to be committed](#veduta-lock-yaml-is-written-0600-by-a-uid-the-operator-is-not) | Deployment | Low |
| [A panicking integration takes the whole process down](#a-panic-inside-a-card-refresh-kills-the-process-and-leaks-its-single-flight-entry) | Scheduler | Medium |

---

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

### Appearance options are deferred until asked for

Recorded by [decisions/0002-theming-and-visual-customisation.md](decisions/0002-theming-and-visual-customisation.md).
Veduta ships two fixed presets - `dashboard.appearance: clean | veil` - and no public knobs. A
preset privately owns its surface opacity, scrim, border alpha, shadow and blur.

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

Priority: on demand. Nothing here blocks anything; the entry is the record of a decision not to
build, which is easy to forget and expensive to rediscover.

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

### A panic inside a card refresh kills the process, and leaks its single-flight entry

Found when a genuine panic in the declarative runtime crashed a running container, 2026-09-12 -
the nil-params bug now fixed in `internal/integrations/declarative/runtime.go` (see
[03-backlog-resolved.md](03-backlog-resolved.md)). The panic itself is closed; that it was fatal
to the process is not.

`Manager.runShared` calls `d.Run(runCtx)` with no recovery, on a goroutine started by
`Manager.Apply`, so a panic anywhere under a card refresh unwinds past `loop` and terminates the
program. Under `restart: unless-stopped` that is a crash loop: every restart re-runs the same card
and panics again. Only `internal/api/server.go` has a `recover()`; a card refresh does not come
through it, so nothing catches this.

The dashboard's whole stance elsewhere is that one failing card degrades to an error tile while
the rest keeps serving - an unreachable upstream, a denied route and a restricted Docker proxy all
behave that way. A panic is the one failure mode that does not, and it is the one the operator can
least diagnose, because the process is gone and the card that caused it is not named.

Not fixed on the spot because it is slightly more than wrapping `d.Run` in a `recover()`. The
panic also skips `close(f.done)` and the `delete(m.flights, key)` beside it, so every other
caller waiting on that single-flight entry blocks forever and the key is never reusable. A correct
fix converts the panic into an ordinary `error` for that card - so it becomes an error tile with
the stack in the log - and makes the flight cleanup happen on the way out whatever the reason,
which means restructuring that block around a `defer`. The declarative runtime is in-process; only
the WASM one is sandboxed, so this is the guest-code path that can actually do it.

Priority: medium, and worth doing before the 0.1.0 tag if there is time - a self-hosted dashboard
that exits on a bad card is a poor first impression, and the crash loop hides the cause.
