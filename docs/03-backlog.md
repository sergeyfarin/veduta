# 03 — Gaps, issues and improvement opportunities

Distinct from [docs/02-implementation-plan.md](02-implementation-plan.md)'s milestone table: that
document is deliberately-scoped, dependency-ordered *planned* work. This one is the opposite kind
of list - things noticed *while* building something else, real enough to write down, not yet worth
their own milestone or not in scope for the milestone that found them. An entry here is a small
debt or an open decision, not a task assignment; when one gets picked up, say so and where the fix
landed rather than deleting the entry silently, so the history of "we knew about this since when"
survives.

Each entry: what the gap is, which milestone found it, why it wasn't fixed there, and its rough
priority (which milestone should absorb it, or "before X" for a hard blocker).

---

## Open

### `api.Config.Registry` is built once at startup and does not follow config hot-reload

Found while building D5, which is the first thing to actually construct
`internal/connections.Registry` in production code at all (D1 through D4 only built and tested it
in isolation; Phase F, the intended long-term owner of a live one, does not exist yet).
`cmd/veduta/main.go`'s `serve` builds the registry once, from the snapshot `config.Open` returns
at startup, and hands it to `api.Config.Registry` - unlike `ConfigStore` itself (an
`atomic.Pointer[Snapshot]`, kept current by the same file watcher), nothing rebuilds this registry
if the config later reloads with different, added or removed connections. `GET /api/v1/connections`
and `POST /api/v1/connections/{id}/test` would then show a connection that no longer exists in the
current config, or omit one just added, until the process restarts. Priority: Phase F - a live
scheduler is the first thing that actually needs connections to reload correctly (a card bound to
a newly-added connection has to work without a restart), so building the atomic-swap mechanism
belongs there rather than being spot-fixed here ahead of a real need.

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

### D2b's approval endpoint has no sudo-window gate and no audit trail yet

`docs/02-implementation-plan.md`'s D2b entry calls for `POST /api/v1/auth/sudo`, a fresh
re-authentication window gating `POST /api/v1/integrations/{id}/approve`, and every approval
audited with actor, IP and diff. None of that exists: sessions are milestone H1 (deps: F1, which
does not exist either) and `internal/audit/` is H2 (deps: H1) - D2b's own listed deps are only
`D2, C1`, so the plan itself asks for machinery from milestones that have not been reached yet.
Building a fake sudo window with no real session to gate would be worse than not building one, so
`internal/api/integrations.go`'s `POST .../approve` handler currently has no additional gate
beyond whatever reaches it at all (bounded today by the same loopback-only bind every other
unauthenticated endpoint relies on - see `Config.AllowPublicWithoutAuth`). `veduta integration
approve` (the CLI) is unaffected, since docs/01-architecture.md already documents CLI approval as
available unconditionally regardless of auth mode.
Priority: **H2** - wire a sudo-window check and an audit write into
`internal/api/integrations.go`'s approve handler once `internal/auth` (H1) and `internal/audit`
(H2) exist; the handler's own logic (digest check, grant-subset check, lock write) does not need
to change.

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

### Limits/Manifest type duplication across packages is deliberate, but was a real contributor to two enforcement bugs

Found in the D2b/D3/D4/D5 review (2026-09): `integrations.Limits` (pointer-fielded, manifest-
declared), `manifestload.Limits` (plain-int, already reconciled), `integrations.EffectiveLimits`,
and `capabilities.Limits` (a deliberately narrower broker-only subset - see its own doc comment)
all separately name overlapping resource ceilings. The review is right that this duplication
contributed to two real bugs this same review found and fixed: `capabilities.Broker.HTTP` never
read `Grant.Limits.ResponseMB` at all, and `internal/widgets.Validate` had no way to accept a
narrower-than-default `outputKB` from `manifestload.Limits`, so both fields were present in the
schema and in at least one Go type but silently unenforced by the code that should have checked
them. The review's proposed fix (unify into one type) is declined: each shape earns its difference
from a real, previously-made decision - `integrations.Limits` needs pointer fields specifically so
an explicit `cacheEntries: 0` is distinguishable from "unset" (see the `CacheEntries can't
represent an explicit zero` entry below for the one place that distinction still isn't threaded
through), `manifestload.Limits` is post-reconciliation and has no such ambiguity to represent, and
`capabilities.Limits` is intentionally narrower because the broker never enforces the declarative
runtime's/WASM sandbox's own budgets (memoryMB, timeoutMs, jsonDepth, exprNodes, iterations, ...).
Collapsing these into one type would either lose that distinction or leak enforcement concerns
across a package boundary that `docs/01-architecture.md` deliberately keeps separate.
Mitigated instead: `internal/integrations/limits_test.go`'s
`TestManifestloadLimitsMatchesTheFieldSet` and `TestCapabilitiesLimitsIsARealSubsetOfLimitBounds`
now cross-check every package's Limits field set against `limitBounds` (already checked against
the manifest schema by the existing `TestLimitBoundsMatchManifestSchema`), so a field silently
missing from one of these types - the shape of both bugs this review found - now fails a fast,
targeted test instead of surfacing as a wrong runtime ceiling. This checks that a schema field has
a same-named home in every type; it does not (and structurally cannot, without duplicating the
enforcement logic itself) check that the field is actually *read* by the right enforcement code
path - that part still needs one test per limit, the way `TestBroker_HTTP_ResponseMBEnforced` and
`TestInvoke_ApprovedOutputKBIsActuallyEnforced` do. Priority: none currently open - this is a
completed, permanent mitigation, recorded here (per this file's own stated purpose) so the
trade-off and its reasoning survive even though nothing further is scheduled.

### AssetRef tokens are missing the connection-revision field ("cf") that makes revocation enforceable

`internal/capabilities.AssetRef` (D2) mints a token structurally matching
docs/01-architecture.md section 7's payload, but deliberately omits `cf` (the connection
revision): that field is what makes "reject tokens for materially changed connections"
enforceable (a repointed host, a rotated secret, a changed TLS policy), and it depends on
`connection_state` - a persisted, instance-keyed revision that does not exist because no storage
layer exists before milestone E1. The signing key itself is also ephemeral (generated fresh in
memory on every process start, per `NewBroker`'s own doc comment) rather than the persisted
`settings`-table key section 7 describes, so every restart invalidates outstanding refs today -
acceptable for D2's own scope (authorisation), not for E1's (the asset proxy's actual lifecycle).
Priority: **E1** - `Broker.AssetRef`'s signature does not need to change, only its
implementation, once `connection_state` and the persisted signing key exist.

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

### `auth: none` + actions guard is not implemented

The schema's own description for `auth.mode: none` says: "additionally requires the
`--i-know-what-im-doing` flag when any action or secret is configured (semantic check)." C3
implemented the secrets half: `serve` now loads a `*Snapshot`, resolves every secret before
publishing it, and refuses `auth.mode: none` plus any secret reference unless the override was
passed. "Any action... configured" remains blocked: `ActionsBlock` renders permanently disabled
until Phase H gives actions a real execution path. Priority: Phase H; extend the same startup guard
once an enabled action has a real configuration representation.

### `${secret:NAME}` cannot be embedded in a larger string - but Jellyfin's real auth header needs exactly that

Found for real, not hypothesised, while smoke-testing C2's resolver against the actual shipped
`examples/veduta.yaml` end to end for the first time (`config.LoadPath` → `secrets.ResolveAll`).
The Jellyfin connection's auth value must be `Authorization: MediaBrowser Token="${secret:
JELLYFIN_KEY}"` (`docs/spikes/s2-upstream-reality-check.md`'s F6 - this is Jellyfin's actual,
documented scheme, not a choice this project made). `secretRefPattern` only recognises
`${secret:NAME}` as an entire scalar value ("there is no defined way to redact half a string" -
`internal/config/secretref.go`), so as committed, this value is silently a **literal string
containing the placeholder text verbatim** - it would never resolve, and would send the wrong
header the moment a real HTTP client exists (Phase D). No error existed anywhere for this before
today. Mitigated, not fixed: `internal/config/secretsuspicious.go`'s `suspiciousSecretRefs` now
emits a **warning** (`TestLoad_RealExampleConfig` asserts it fires on the real file) for any
scalar containing `${secret:` that doesn't match the whole-value pattern, so this is at least
visible at `--check-config` time instead of failing silently at runtime. The real fix needs
`SecretRef` to become template-aware (a string with one or more named placeholders, composed and
wrapped in a single opaque `secrets.Value` - not a per-placeholder redaction problem, since the
*whole* composed result can just always print as `***`). **Update, found in the D2b/D3/D4/D5
review (2026-09):** D1 shipped without resolving this. `examples/veduta.yaml`'s Jellyfin
connection still carries the literal `${secret:JELLYFIN_KEY}` placeholder embedded inside a larger
`auth` value, and D1's `internal/connections` auth injection only supports the schema's typed
`auth.type` values (`bearer`/`basic`/`header`), none of which composes a secret into a larger
string either - so the shipped example is not just a documentation gap but a **non-functional
example connection**: `--check-config` only warns (via `suspiciousSecretRefs`), it does not fail,
so an operator copying this example gets a connection that silently sends the literal placeholder
text as a header value instead of a real token. Priority raised to **before D6 (or whichever
milestone next touches `examples/veduta.yaml` or ships the CLI's example-generation path)** -
either give `SecretRef` the template-aware composition described above so the example can actually
work, or replace the shipped Jellyfin example with a connection shape the current schema can
actually execute (e.g. a case where the whole auth value is one secret reference) until that
composition work happens.

### A redirect to an in-policy host can still reach a route the lock never approved

Found in the D2b/D3/D4/D5 review (2026-09), fixed partially: `internal/connections/client.go`'s
redirect policy now refuses a same-request redirect that changes host, downgrades scheme, or (when
`allowedPaths` is configured) lands outside every allowed path subtree - see
`routepath.HasPathPrefix` and `redirect_test.go`. What it still cannot do is re-run the *lock's*
route-level authorization (`capabilities.connectionAllows`, the actual grant check) on the
redirected path, because that check needs a `*capabilities.Grant` (built from the manifest/lock at
invoke time), and `redirectPolicy` is constructed inside `connections.Registry`, a layer below and
independent of `capabilities.Broker`. A manifest approved for `GET /api/stats` only, talking to a
connection whose `allowedPaths` is permissive (or unset), could still be redirected by a
compromised or misconfigured upstream to another *connection-policy-allowed* but
*lock-unapproved* path on the same host. Priority: whenever `Grant` plumbing reaches
`connections.Registry.Do` (no milestone currently owns threading a `Grant` that deep - it would
need to become a parameter of `Do`/`redirectPolicy` rather than living only in `capabilities`);
until then, operators who need this closed should set restrictive `allowedPaths` per connection,
which the fix above does enforce today.

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

---

## Resolved

### S3 (expr vs cel bake-off) is decided: `expr-lang/expr`, used as parser/evaluator only

Found while starting C1, which lists S3 as a dependency; C1 routed around it since `rule.when` was
only pattern-scanned there, never evaluated. Resolved by decision rather than by running the
originally-planned dual-library benchmark: the benchmark's own premise (prove `expr`'s
"context-aware mode instruments loops with cancellation checks" by writing a hostile expression)
turned out to be checkable directly against `expr`'s source instead of empirically - `WithContext`
only propagates cancellation to context-aware custom functions, never to `expr`'s own built-in
`map`/`filter`/`sortBy`, so no benchmark would have shown either candidate interrupting a hostile
loop for free. That reframes the decision around ergonomics (D3's manifest DSL is
templating-shaped, which is what `expr` already looks like; CEL optimises for boolean policy
predicates and would push a DSL redesign) with safety handled by a **D3-owned** execution budget,
not by either library's own resource accounting - confirmed `expr.DisableBuiltin`/
`DisableAllBuiltins` remove a name from the builtin table before name resolution, so D3 can
reliably disable every scalable builtin and replace it with a charged implementation of the same
name. Decision, corrected `WithContext` claim, and the explicit supported-function-list design:
docs/01-architecture.md decisions D7 and D47, and §5's "expr is a parser and evaluator; D3 is the
sandbox." D3 itself (not yet started) is where this is actually implemented and tested.

### Config: `baseUrl` whitespace regex accidentally rejected the letter `s`

Found while adding C3's startup security test with the ordinary hostname `service`. The JSON
Schema encoded `\\s` with one escaping layer too many, so the regex engine saw a character class
excluding a backslash and the literal letter `s`, rather than whitespace. Hosts and paths containing
`s` were rejected while existing examples happened not to expose it. Resolved in C3 by correcting
the JSON escaping and keeping `http://service:8080` in the regression test.

### Config: `SecretRef` carried no position, blocking C2's "diagnostic naming the config location"

Found while planning C2, which needs to report *where* a missing secret was referenced, not just
that one was missing - C1's `SecretRef{Literal, Name}` had no file/line/col, and nothing else
retained one after `Load` returned a `*Snapshot`. Resolved in C2, not by adding position fields to
`SecretRef` itself (that would need a reflection-based walk matching decoded struct fields back to
schema paths, and would be ambiguous whenever the same secret name is referenced more than once -
which name lives are the case for). Instead: `internal/config/secretlocations.go` scans the merged
tree by *content* (any scalar matching `${secret:NAME}`) rather than by structural path, producing
`Snapshot.SecretRefs []SecretLocation{Name, File, Line, Column}` with one entry per occurrence.
`secrets.ResolveAll` resolves each distinct name once and emits a `config.Diagnostic` - reusing
the existing type rather than inventing a parallel one - for every occurrence of a name that
failed, so a secret referenced in three places and missing is three real locations, not one
anonymous complaint. See `TestLoad_SecretRefsCollectsEveryOccurrence` and `TestResolveAll`.

### Config: where does "a secret value in a Widget Document is rejected" live?

Found while planning C2, which states this as its own acceptance test. `internal/widgets` cannot
import `internal/secrets` (docs/01-architecture.md's frozen import-boundary rule: integrations,
and the widgets package they emit into, never see credentials at all), so the check cannot live in
`widgets.Validate`. Resolved in the other direction instead: `internal/secrets` imports
`internal/widgets` (nothing forbids that direction - the frozen rule constrains what integrations
can reach, not what core packages may import) and `Registry.ContainsSecretInDocument(doc)` walks
every text-bearing field of all nine v1 block types explicitly (a type-switch, matching this
project's existing preference for that over reflection - see `internal/widgets/blocks.go`'s own
dispatch), reusing the same `Registry.ContainsSecret` substring check the log scrubber uses.
`TestContainsSecretInDocument_EveryBlockType` covers all nine block types and is itself mutation-
tested: deleting one type's case from the switch was confirmed to fail the test before this was
considered done. What's still pending, honestly: nothing calls this yet against a *real* produced
Document - that caller is Phase F's scheduler, which does not exist. This is the primitive ready
for that caller, not the end-to-end wiring.
