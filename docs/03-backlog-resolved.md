# 03 — Resolved gaps and issues

The closed half of [03-backlog.md](03-backlog.md). Entries are kept rather than deleted: each one
records what the gap was, which milestone found it, and where the fix actually landed, so the
history of "we knew about this since when" survives and a reader chasing a `docs/03-backlog.md`
pointer in the source finds the answer here.

Grouped by where the fix landed, not by when the gap was found. Nothing here needs action.

## Resolved during Phase C and D

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

**Update (2026-09-10):** the design half is what is resolved here. Phase F has since
shipped and still does not call this primitive, so the wiring half is tracked as an open
entry in [03-backlog.md](03-backlog.md).

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

### A redirect to an in-policy host could still reach a route the lock never approved

Found in the D2b/D3/D4/D5 review (2026-09), first fixed only partially: `internal/connections/
client.go`'s redirect policy refused a same-request redirect that changed host, downgraded scheme,
or (when `allowedPaths` was configured) landed outside every allowed path subtree - see
`routepath.HasPathPrefix` and `redirect_test.go`. A second review pass correctly challenged this as
incomplete: none of that re-ran the *lock's* route-level grant on the redirected path, so a
manifest approved for `GET /api/stats` only, talking to a connection whose `allowedPaths` was
permissive (or unset - the common case, since allowedPaths is optional), could still be redirected
by a compromised or misconfigured upstream to another *connection-policy-allowed* but
*lock-unapproved* path on the same host. Closed properly, not just noted: `capabilities.Grant`
gained `AuthorizesRedirect(slot, method, path string) error` (`authorize.go`, reusing
`anyRouteAllows`' method+path+use matching, factored out as `routeMatchesMethodAndPath` -
deliberately narrower than a full `Authorize`, since a redirect target carries none of the
original request's query/content-type/body to re-check). `internal/connections` gained a small
context-carried hook (`redirectauth.go`'s `RedirectAuthorizer`/`WithRedirectAuthorizer`) so
`redirectPolicy` - built once at connection-construction time, with no `Grant` in scope - can call
back into whatever the *invoking* `capabilities.Broker.HTTP` call attached to `ctx`, which Go's
`http.Client` carries unchanged through every redirect hop. `Broker.HTTP` attaches exactly that
before calling `registry.Do`. `TestBroker_HTTP_RedirectToAnUnapprovedRouteIsDenied` and
`TestBroker_HTTP_RedirectToAnApprovedRouteSucceeds` prove the fix without allowedPaths configured
at all - the previously-open case - and the first was confirmed to fail against the pre-fix code.

### D5's `GET /api/v1/connections` checked connections sequentially, sharing one context - a slow one starved the rest

Found chasing down a CI-only failure of `TestConnectionsList_NeverLeaksAConfiguredSecret`
(`internal/api/connections_test.go`) while confirming CI was green after the D2b/D3/D4/D5 review
fixes above - it failed the exact same way on the D5 commit itself, before any of this review's
changes, so this predates the review and was not introduced by it. `routeConnections`'s
`GET /api/v1/connections` handler (`internal/api/connections.go`) checked every connection's
health one at a time in a `for` loop, every check sharing the single incoming `r.Context()`. A
connection that hangs until its context's own deadline - exactly what a real network black hole
does, and what CI's network stack did for a dial to a reserved/unrouted test address where a
developer machine might fail fast instead - consumed the *entire* remaining time budget; every
connection checked after it inherited an already-expired context and was reported unreachable
(`rate limit: context deadline exceeded`) for a reason that had nothing to do with its own health.
Confirmed by reproducing the exact same failure locally with a deterministic hanging listener
(accepts but never responds) rather than relying on environment-dependent dead-address behaviour,
and by confirming the new regression test fails against the pre-fix code and passes against the
fix. Fixed by running every connection's health check concurrently (one goroutine per connection,
`sync.WaitGroup`) so one connection's slowness can no longer starve another's remaining budget -
see `TestConnectionsList_OneSlowConnectionDoesNotStarveAnothersHealthCheck`.

## Resolved in Phase E and F

### `api.Config.Registry` is built once at startup and does not follow config hot-reload

Resolved by Phase F: `connections.Dynamic` now swaps a newly resolved registry together with
new scheduler definitions after every accepted config generation. Old invocations are cancelled
and generation-fenced, so they cannot publish after the swap.

Found while building D5, which is the first thing to actually construct
`internal/connections.Registry` in production code at all (D1 through D4 only built and tested it
in isolation; Phase F, the intended long-term owner of a live one, does not exist yet).
`cmd/veduta/main.go`'s `serve` builds the registry once, from the snapshot `config.Open` returns
at startup, and hands it to `api.Config.Registry` - unlike `ConfigStore` itself (an
`atomic.Pointer[Snapshot]`, kept current by the same file watcher), nothing rebuilds this registry
if the config later reloads with different, added or removed connections. `GET /api/v1/connections`
and `POST /api/v1/connections/{id}/test` would then show a connection that no longer exists in the
current config, or omit one just added, until the process restarts - and, since the registry is
what actually holds resolved secret values (baked in at `connections.New` construction time), a
rotated secret is equally stale until restart, not just a changed connection shape. **Confirmed
still open in the D2b/D3/D4/D5 review (2026-09):** that review's finding #9 named this same gap
alongside the `auth: none` reload check; only the `auth: none` half was fixed
(`cmd/veduta/main.go`'s `configLoader` now re-validates it on every reload, see
`TestConfigLoader_RejectsAuthNoneOnHotReloadNotOnlyAtStartup`) - config, resolved secrets and the
registry still do not reload as one coherent generation, and that is this entry, not a new one.
Priority: Phase F - a live scheduler is the first thing that actually needs connections to reload
correctly (a card bound to a newly-added connection has to work without a restart), so building
the atomic-swap mechanism belongs there rather than being spot-fixed here ahead of a real need.

### AssetRef tokens are missing the connection-revision field ("cf") that makes revocation enforceable

Resolved by E1 (the remaining text is the historical finding): the signing key and private material-HMAC key are persisted in `settings`;
`connection_state` holds opaque random revisions which rotate on resolved material changes; tokens
carry only that revision; and serving rechecks revision plus current approval.

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

### `${secret:NAME}` cannot be embedded in a larger string - but Jellyfin's real auth header needs exactly that

Resolved while implementing E/F (the remaining text is the historical finding): `config.SecretRef` is template-aware, every embedded occurrence
is resolved, and `internal/connections` composes the whole result into one opaque `secrets.Value`.
The shipped Jellyfin authorization value now sends the resolved token and remains fully redacted.

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

## Resolved in Phase H

### Resolved in H2: D2b approval sudo-window and audit trail

H2 added `POST /api/v1/auth/sudo`, a five-minute password re-authentication window, authenticated
approval attribution and persistent success/failure audit records. Forward auth defaults to
CLI-only privileged operations and may opt into a configured admin group. `auth: none` remains
CLI-only. `veduta integration approve` remains available regardless of server auth mode.

### Resolved in H2: `auth: none` + actions guard

The schema's own description for `auth.mode: none` says: "additionally requires the
`--i-know-what-im-doing` flag when any action or secret is configured (semantic check)." C3
implemented the secrets half: `serve` now loads a `*Snapshot`, resolves every secret before
publishing it, and refuses `auth.mode: none` plus any secret reference unless the override was
passed. H2 extended the guard to Docker `allowActions` and action blocks in card views. The UI now
shows a persistent warning whenever authentication is disabled.

### Resolved in H1: SSE per-session cap

F4 capped the process at 128 streams and dropped slow consumers without blocking publication.
H1 now keys a second counter by the hashed server-side session ID and permits four streams per
session; an orderly disconnect removes both counters atomically.

**Correction (2026-09-10):** "removes both counters atomically" was asserted for disconnect in
general and is only true of an orderly one. F4's drop-slow-consumers path releases the
process-wide counter but not the per-session one, permanently exhausting a session's budget - see
the open entry in [03-backlog.md](03-backlog.md). The per-session cap itself is implemented as
described; its release path is not complete.

## Resolved before K2 and L3

### Resolved before K2: config publication preceded the rebuilt runtime generation

Phase F now rebuilds resolved secrets, connection revisions, the registry and scheduler definitions
as one runtime generation, then swaps them. `config.Store` publishes the accepted configuration
first, however, and `reloadRuntime` observes it on a 500 ms poll. During that short interval the
dashboard layout can describe the new config while card/connection execution still belongs to the
previous generation. No old invocation can publish after the scheduler swap (generation fencing),
and asset checks fail closed, but the API view is not a single atomic config+runtime snapshot yet.
If runtime composition (for example, lock parsing) fails after config validation, `/config/status`
also still reports the config generation as valid while the error is present only in logs.
The original deadline (before multi-user/auth work) passed. Closed before K2: `config.Store` now
calls a runtime activator after loading and validating a candidate but before publishing it. The
activator builds the candidate generation, applies authentication, rules, notifications and the
scheduler with rollback on failure, then swaps the registry/runtime. Only that complete success
publishes the config generation. Activation errors reject the candidate, retain the last-good
snapshot and appear in `/config/status` diagnostics. The 500 ms runtime poll is removed.

### Resolved before L3: rule transitions were not committed atomically

J2/J3 persist a fired or resolved transition in three steps: append `rule.fired`/`rule.resolved`,
enqueue each notification, then write `rule_state`. Dedupe limits repeated deliveries, but a crash
or database error between those steps can leave an event or outbox row committed while the durable
rule state still describes the previous transition. On restart the manager can therefore append a
duplicate event and, after the cooldown window, enqueue the same transition again. Wrong-signal-type
episodes do not have this gap: J3's follow-up persists their marker before emitting the event.
Closed before L3: the dispatcher now prepares credential-free outbox requests without writing,
and `storage.CommitRuleTransition` commits the rule state, transition event, all outbox rows, and
any channel suspension meta-event in one SQLite transaction. The dispatcher wakes only after the
commit; network delivery remains outside it. Injected last-statement failures prove fired and
resolved transitions roll back state, event, outbox, suspension state and suspension meta-event.

## Resolved in Phase L

### S2 real-server validation (2026-09-09): Immich green + E3 cold-latency met; Jellyfin rewritten to Approach A

`hack/capture-upstream-fixtures.sh` was run against a live **Immich 3.1.0** and a live **Jellyfin
12.0.0**. Scrubbed evidence committed under `testdata/upstream/` (`git add -f`; see its
`CAPTURE-NOTES.md` for what was synthesised).

**Immich — confirmed:**
- `/api/assets/statistics` → 200 `{"images","videos","total"}` (needs an `asset.statistics`-scoped
  non-admin key; a 403 here just means that scope was not granted).
- Thumbnail `Content-Type` is `image/jpeg`, not the OpenAPI's `application/octet-stream` (F3
  resolved; the asset proxy sniffs regardless).
- `nextPage` is the string `"2"`; `thumbhash` and `visibility:timeline` on every asset; sizes well
  inside `inputMB: 4`; no redirects.
- F1 confirmed (`/api/server/statistics` → 403 for a non-admin key).
- E3's "six photos render under 2 s cold" measured end-to-end on a LAN Immich, fresh process +
  empty disk cache: `GET /api/v1/cards` 31 ms + six thumbnails through the signed proxy in parallel
  58 ms ≈ **90 ms**. API key absent from page HTML, card JSON and logs.
- Minor: live server 3.1.0 vs the manifest's cited 3.2.0-rc.0; manifest comment updated to note the
  live check rather than bumping the spec reference.

**Jellyfin — the spec pass's `/Users/Me` + `/Items/Latest` fix was wrong; rewritten to Approach A:**
- A Jellyfin API key carries **no user identity**, so `GET /Users/Me` → 400 and a userId-less
  `GET /Items/Latest` → 400. `/Items/Latest` also ignores `includeItemTypes`. `/Users/{userId}/Items`
  is absent from the 12.0.0 OpenAPI (F4 correct at the spec level).
- Recently-added is now a single `GET /Items?recursive=true&includeItemTypes=Movie,Series&sortBy=DateCreated&sortOrder=Descending&limit=N`
  with the API key; per-type counts are the same endpoint with a type filter; `/Sessions` for
  streams. Three manifest routes, four requests, no `/Users/*`.
- F5 (unauthenticated posters), F6 (`Authorization`-header API key is the only 12.0.0 scheme) and
  F7 (PascalCase default; explicit `profile="CamelCase"` honoured — the plugin pins it) all
  confirmed live.
- Changed: `plugins/jellyfin/manifest.yaml` (routes + header + `httpRequests` 5→4),
  `plugins/jellyfin/src/lib.rs`, `plugins/jellyfin/jellyfin.wasm` (rebuilt, deterministic,
  `sha256 0bd308fd…`), `plugins/jellyfin/testdata/` (`recent.json` replaces `latest.json`/`user.json`),
  `internal/integrations/wasm/jellyfin_test.go`, `examples/veduta.lock.yaml`,
  `testdata/canonical/expected-digests.json`, `hack/capture-upstream-fixtures.sh`. The Go golden
  (`testdata/widgets/jellyfin-recent.golden.json`) is unchanged — fixtures were kept value-identical
  on purpose.

## Completed mitigations, nothing further scheduled

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
represent an explicit zero` entry in [03-backlog.md](03-backlog.md), still open, for the one place
that distinction still isn't threaded through), `manifestload.Limits` is post-reconciliation and
has no such ambiguity to represent, and
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
