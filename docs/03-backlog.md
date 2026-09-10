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
| [SSE per-session cap leaks a slot when a slow consumer is dropped](#sse-per-session-cap-leaks-a-slot-when-a-slow-consumer-is-dropped) | API | Medium |
| [`ContainsSecretInDocument` has no production caller](#containssecretindocument-has-no-production-caller) | Secrets | Medium |
| [Asset format coverage is narrower than the allowlist](#asset-format-coverage-is-narrower-than-the-architectures-final-allowlist) | Assets | 0.2 transform milestone |
| [Asset-cache startup does not reconcile orphan files](#asset-cache-startup-does-not-reconcile-orphan-files) | Assets | Low |
| [Lock-write race against a concurrent CLI approval](#the-lock-write-race-is-closed-only-within-one-process-not-against-a-concurrent-cli-approval) | Integrations | Low |
| [`httpConnection.headers` are not secret-capable](#config-httpconnectionheaders-values-are-plain-strings-not-secret-capable) | Config schema | Low-medium |
| [`CacheEntries` can't represent an explicit zero](#manifestloadlimitscacheentries-cant-represent-an-explicit-zero) | Integrations | When declarative caching lands |
| [Approve flow can't grant a limit above its default](#the-documented-approve-flow-has-no-way-to-grant-a-limit-above-its-documented-default) | Docs | Low |
| [Go plugin SDK needs a WASI-free toolchain](#go-plugin-sdk-requires-a-maintained-wasi-free-toolchain) | Plugins | Low |

---

### Open question: does Jellyfin still earn being the WASM proof case?

After the Approach-A rewrite (recorded in [03-backlog-resolved.md](03-backlog-resolved.md)), the
Jellyfin plugin no longer does "send auth header →
resolve the current user → list": it does one sorted `GET /Items`, two typed `GET /Items` counts
and `GET /Sessions`, then folds them into one document. That is still cross-endpoint aggregation a
linear declarative pipeline cannot express today (fan-out to four calls, per-item poster handling,
a missing-image notice), but it is closer to the declarative line than the original design was.
Worth deciding deliberately, when G-phase is revisited, whether G4 stays the WASM vertical slice or
the proof case moves to an integration that genuinely needs per-request branching / retry logic.
Not blocking anything.

### `ContainsSecretInDocument` has no production caller

Split out of the resolved design entry in
[03-backlog-resolved.md](03-backlog-resolved.md) ("where does *a secret value in a Widget Document
is rejected* live?"). That entry settled the design and built the primitive:
`internal/secrets.Registry.ContainsSecretInDocument` walks every text-bearing field of all nine v1
block types, and `TestContainsSecretInDocument_EveryBlockType` is mutation-tested. Its own closing
note was honest that nothing called it yet, and named the intended caller: "that caller is Phase F's
scheduler, which does not exist."

**Phase F now exists, and it still does not call it.** `grep -rn ContainsSecretInDocument` finds
only the definition and its own tests, so no real produced Document is checked in production - a
plugin that echoed a credential into a card title would still be rendered. The compensating
controls that do run are the log scrubber and L3's CI secret-scan across HTTP responses, so this is
a missing layer rather than an open hole, which is why it is here and not a release blocker.
Priority: medium - wire it into the scheduler's publish path (reject or blank the document and
surface an error state), with a test that a document carrying a configured secret never reaches
`/api/v1/cards`.

### SSE per-session cap leaks a slot when a slow consumer is dropped

Found reviewing [03-backlog-resolved.md](03-backlog-resolved.md)'s "Resolved in H1: SSE per-session
cap" entry, whose closing claim - "disconnect removes both counters atomically" - holds only for an
orderly disconnect. It does not hold for F4's drop-slow-consumers path, and the two features were
built one milestone apart without the interaction being re-checked.

`internal/api/sse.go`'s `publish` drops a consumer whose 32-deep buffer is full with `close(ch)` +
`delete(h.clients, ch)`, and never touches `h.sessions`. The `done` closure `subscribe` returns -
the only place that decrements `h.sessions[sessionID]` - is guarded by `if _, ok := h.clients[ch];
ok`, which is already false for a client `publish` removed, so it returns without decrementing
either. The process-wide `maxSSEClients` counter is released correctly; only the per-session one
leaks.

Effect: a session whose streams are dropped `maxSSEClientsPerSession` (4) times accumulates a
permanent count of 4 and is refused every subsequent SSE subscription for the life of the process,
with `len(h.clients) == 0`. Reconnects do not clear it - the counter is keyed by the hashed session
ID, which survives reconnection - so recovery needs a restart or a fresh login. A slow or
backgrounded browser tab on a busy dashboard is enough to trigger it; no hostile client is needed.

Confirmed with a probe against the current code: after four subscriptions for one session are all
dropped by `publish`, `h.sessions["sess"]` is still 4 while `h.clients` is empty, and the next
`subscribe` for that session is rejected.

Priority: medium - a live-update outage for one user, self-inflicted and not a security boundary,
but it fails closed in the annoying direction and has no operator-visible symptom. Fix is small:
decrement the session counter in `publish`'s drop path, ideally by routing both removal sites
through one `removeClient(ch)` helper that owns both counters, plus a regression test that drops a
session's streams via `publish` and then subscribes again successfully.

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
