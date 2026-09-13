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
shipped and still does not call this primitive, so the wiring half is tracked separately.
**Closed (2026-09-12):** the scheduler now calls it on both the produced and the restored path -
see "`ContainsSecretInDocument` had no production caller" below.

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
process-wide counter but not the per-session one, permanently exhausting a session's budget. The
per-session cap itself is implemented as described; its release path is not complete.
**Closed (2026-09-12):** both removal sites share one `removeClient` helper that owns every
counter, so the original claim now holds for a dropped consumer too - see "SSE per-session cap
leaked a slot when a slow consumer was dropped" below.

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

## Resolved after Phase L

### Release archives shipped no first-party plugins

Noticed while reviewing whether plugins must live inside the single binary. They already did not:
`integrations.LoadManifest(src.Dir)` reads a manifest (and its `module:` wasm) from an arbitrary
directory at runtime, so side-loading is the only loading path there has ever been, and a
third-party integration uses exactly the same one a first-party integration does. The gap was on
the distribution side: `.github/workflows/release.yml` staged only `veduta` plus the licence and
notice files into each tarball, so a user who downloaded a release got none of `plugins/glances`,
`plugins/immich`, `plugins/beszel` or `plugins/jellyfin`, and no documented directory to put them
in. Meanwhile the Dockerfile did ship them, via five hand-maintained `COPY` lines, so the image and
the archive already disagreed about what a release contains.

Resolved by making the distributed set explicit and single-sourced. `hack/stage-plugins.sh` holds
the list, stages it into a destination directory, and verifies every `.wasm` against the `sha256`
its own manifest pins; both the release workflow and the Dockerfile call it, so the image and the
archives cannot diverge. The archive verification step now diffs the extracted `plugins/` against
`stage-plugins.sh --list` (failing in either direction, so neither an omission nor a stray Rust
source passes), digests every shipped manifest, and runs `plugin validate` over every shipped
module.

The layout question it raised is settled and documented rather than left implicit: archives are
relocatable with `plugins/` beside the binary (`source: path:./plugins/immich`), the recommended
system layout matches the container image at `/usr/share/veduta/plugins`, that directory belongs to
the release and is replaced wholesale, and operator-supplied integrations belong outside it. See
`docs/getting-started.md`, `docs/migration.md` for the upgrade sequence, and
`docs/01-architecture.md` section 5 for why first-party integrations are deliberately *not*
`source: builtin`: builtin is lock-exempt, and exempting the integrations most likely to hold real
credentials from the control that governs them would invert the trust model. The schema description
for `integrations[].source` now carries the same distinction, so the generated configuration
reference explains it too.

Left open deliberately: this settles on-disk location and verification, which the deferred "plugin
marketplace, signing and OCI distribution" line in `docs/01-architecture.md` section 15 needs
before an ecosystem can distribute into it, but it adds no signing and no registry.

### A declarative asset node could not carry `alt` or `aspect`

Found by building a throwaway declarative Jellyfin manifest against the DSL as it stood. An asset
node was only recognised when `asset` was the *sole* key of its object, and it evaluated to
`map[string]any{"ref": ref}` - a whole `image`, not a value. Since `widget-document.v1`'s `image`
is `{ref, alt, aspect, blurhash}` with `additionalProperties: false`, there was no way to write a
broker-minted image *and* its alt text: adding `alt` beside `asset` stopped it being an asset node,
and nesting it under `ref` produced `{"ref": {"ref": …}}`. The effect was not Jellyfin-specific -
**no declarative integration could give an image alt text**, and `plugins/immich` shipped exactly
that, an `image-grid` whose photos had no `alt` because the DSL made it impossible.

Resolved by making the node evaluate to the ref string, which is what `plugin-manifest.v1` already
said it was: `valueNode` lists `assetNode` alongside literals and `exprNode`, so the runtime
returning an object had been contradicting the schema's own typing. Manifests now write
`ref: { asset: … }` and the surrounding `image` carries whatever else it needs. Immich's photos got
their `alt` in the same change, which moved its manifest digest - `examples/veduta.lock.yaml` and
`testdata/canonical/expected-digests.json` are regenerated, and
`TestAssetNodeIsAValueSoImagesCanCarryAltText` fails if the shape regresses.

This is a breaking manifest-DSL change, taken deliberately and while it is still cheap: the DSL is
pre-0.1, the only asset node in existence was Immich's, and a manifest edit is digest-visible, so
any third-party manifest would need re-approval regardless. Nothing about the authority model
changed - `validateOutput` walks templates generically, so an asset node nested at `image.ref` is
still checked against the operation's `use: asset` routes exactly as before.

### An absent optional field rendered as the string `<nil>`

Found in the same spike as the asset-node gap. An upstream field that is sometimes missing had no
safe declarative spelling. `{expr: item.productionYear}` yields JSON `null`, which fails validation
for any typed field (`subtitle` is `shortText`, a string with no null member) and takes down the
**whole document**, not just that item. The obvious workaround was worse:
`{expr: string(item.productionYear)}` produces the literal four-character string `<nil>`, which
validates happily and renders on the card. Verified against a fixture with no `productionYear` -
the poster's subtitle came out as `"<nil>"`. So the author's choice was between a card that dies on
one missing field and a card that shows `<nil>` to the user, with nothing in CI to catch either:
the shipped manifests avoided it only because their fixtures happen to be complete.

Resolved with a fifth node kind, `{ if: {expr}, then: … }`, which omits the object key or array
element that contains it rather than emitting null - the one thing a value cannot express. Three
deliberate restrictions: the condition must be a **boolean**, since truthiness would make
`if: {expr: item.name}` quietly mean "when the name is non-empty"; there is **no else**, because a
fallback value is what expr's `?:` already does; and an omitted value is **refused** in a request
path, query value, header or asset path, where "no value" is not a meaningful request. The
condition counts against `exprNodes` and the node against the template budget like any other.

This also closes the conditional-output half of the Jellyfin question: a missing-image notice is
now expressible declaratively.

### The declarative runtime never checked a response's status code

Found while scoping a declarative rewrite of Jellyfin, but never specific to it: it affected every
shipped declarative integration. `instance.Invoke` called `decodeJSON(resp.Body, …)` on whatever
the broker returned and never read `resp.StatusCode`. An upstream 401, 403 or 500 whose body
happened to be JSON was folded into the document as though it were data - a cheerful, entirely
fictional card - and one whose body was HTML surfaced as a JSON decode error naming the pipeline
step rather than the status. Immich's own manifest comments that `/api/server/statistics` returns
403 for a non-admin key, which is exactly this path. The wasm side never shared the defect:
`plugins/jellyfin/src/lib.rs` rejects non-2xx explicitly.

Resolved by making a non-2xx pipeline response fail the invocation, with an error naming the step,
method, path and status. The card then shows an error, which is what an upstream error is. The two
runtimes now agree, which was also the last DSL-side blocker on rewriting Jellyfin declaratively.

Decided deliberately and left narrow: there is **no per-step opt-out**. An integration that wants
to read 404 as "absent" is a real use case, but none exists today, and the escape hatch is easier
to add against a concrete need than to remove once manifests depend on it. `TestPipelineRejects
NonSuccessStatus` covers 301/401/403/404/500/503 with a valid JSON body, so it fails if the status
ever stops being what rejects them, and `TestPipelineAcceptsEverySuccessStatus` pins the 2xx range
including 204 and 299.

One test fake had to change with it: `fixtureBroker` returned a zero `StatusCode`, which is not
something the real broker can produce - `capabilities.Broker.HTTP` always copies the response's
status.

### `ContainsSecretInDocument` had no production caller

Carried as an open entry since 2026-09-10, split out of this file's "where does *a secret value in
a Widget Document is rejected* live?" - that entry settled the design and built the primitive, and
was honest that nothing called it, naming the intended caller as "Phase F's scheduler, which does
not exist." Phase F shipped and still did not call it: `grep -rn ContainsSecretInDocument` found
only the definition and its own tests, so no produced Document was checked in production and a
plugin that echoed a credential into a card title would have been rendered.

Resolved in `internal/scheduler`. A `Manager` now holds a `*secrets.Registry` - `New` takes the
process-wide one every resolved `secrets.Value` registers itself with, so the check is on by
construction rather than by remembering to wire it at a call site, and `NewWithSecrets` gives a
test its own instance. `refresh` checks the document the run returned and, on a match, drops it and
fails the run with `ErrSecretInDocument`, whose message is fixed and value-free because it becomes
the card's visible error text; `classify` maps it to `state.ErrorInvalid`, so the card shows an
error and the breaker eventually opens rather than the document silently vanishing. `Apply` re-runs
the check over a document restored from storage, which may predate the secret that now matches it -
that is the other path a document can take into `m.states` and so into `GET /api/v1/cards`.

Two things were fixed along the way. The walker only covered the nine block types, so the envelope
fields that are *served* without being drawn - `link`, `notices[].message`, string-valued `signals`
- and two fields added after it was written (`image.alt`, a table column's `label`) went
unchecked; reaching the client is the exposure, not being rendered, so they are checked now
(`TestContainsSecretInDocument_EnvelopeFields`). And the comments in `document.go` and `scrub.go`
that described the missing caller as pending now describe the caller that exists.

Tests: `TestProducedDocumentCarryingASecretIsRejected` and
`TestRestoredDocumentCarryingASecretIsNotServed` cover both paths at the scheduler, and
`TestLeakingDocumentNeverReachesTheCardsAPI` drives the real `GET /api/v1/cards` route - the
end-to-end form of C2's acceptance test. Each was confirmed to fail against the unwired code.
The log scrubber and L3's CI secret-response scan remain the layers around this one.

### SSE per-session cap leaked a slot when a slow consumer was dropped

Carried as an open entry since 2026-09-10, found reviewing "Resolved in H1: SSE per-session cap"
above, whose claim that "disconnect removes both counters atomically" held only for an orderly
disconnect. F4's drop-slow-consumers path and H1's per-session cap were built a milestone apart
and their interaction was never re-checked: `publish` dropped a consumer with `close(ch)` +
`delete(h.clients, ch)` and never touched `h.sessions`, while the `done` closure that owns the
decrement was guarded by `if _, ok := h.clients[ch]; ok` - already false for a client `publish` had
removed. A session whose streams were dropped four times therefore kept a permanent count of 4 and
was refused every later subscription for the life of the process, with `len(h.clients) == 0`.
Reconnecting did not clear it (the counter is keyed by the hashed session ID, which survives
reconnection), so recovery needed a restart or a fresh login, and a backgrounded browser tab on a
busy dashboard was enough to trigger it.

Resolved as the entry proposed: both removal sites now go through one `removeClient(ch)` that owns
every counter the client holds, so a dropped stream releases its per-session slot exactly like an
orderly disconnect, and a second removal of the same channel is a no-op rather than a double
decrement or a double close. `h.clients` maps a channel to its session ID to make that possible -
the removal site inside `publish` has no other way to know which session to credit.

Tests: `TestSSEDroppedSlowConsumerReleasesSessionSlot` fills a session's four streams, overflows
every 32-deep buffer so `publish` drops them all, and subscribes again successfully - it fails
against the old code with the exact symptom the entry described. `TestSSEDoneAfterDropDoesNotDouble
Release` pins the ordering a real handler produces: `publish` drops the stream, the handler's
deferred `done` runs anyway, and the surviving stream keeps its slot.

## Resolved while preparing the first real deployment

Three defects and two gaps, all found by actually running the image and the release archive on a
host rather than by reading them, 2026-09-12. The tag had not been cut, so none of this shipped.

### A fresh named volume at /data was created root-owned, and nothing could start

The first `docker compose up` against the documented example failed outright. The runtime image
had no `/data` directory, so Docker seeded the named volume mounted there from nothing and it came
out `root:root`, which uid 65532 cannot write. SQLite surfaces that as `unable to open database
file (out of memory)` - a message that names neither permissions nor the path. `docs/docker.md`
had claimed a named volume "is created with the right ownership automatically", which is true only
of an image that ships the directory.

Resolved in the `Dockerfile` by carrying an empty directory out of the build stage with
`COPY --from=build --chown=65532:65532`: distroless has no shell to `chown` with, so owning the
directory at copy time is the only mechanism available. Verified by creating a fresh named volume
and starting the stack.

### The documented `integration diff|approve` invocation could not work

`docs/getting-started.md`, `docs/migration.md` and `docs/integration-authoring.md` all wrote
`veduta integration approve <id> --config <path>`. Go's `flag` package stops parsing at the first
non-flag word, so the id was accepted and `--config` and its value became two further positional
arguments, failing the `NArg() != 1` check and printing usage. Every doc that described the
approval flow described a command that prints its usage and exits 1 - and it is the flow a
first-time operator runs immediately after starting the server. Resolved by putting the flags
first in all three documents.

### Mounting /config read-only breaks approval from the dashboard

`docs/docker.md`'s compose fragment mounted `./config:/config:ro`. Both approval paths write
`veduta.lock.yaml` into that directory - `cmd/veduta/integration.go` and the
`POST /api/v1/integrations/{id}/approve` handler in `internal/api/integrations.go` - and
`integrations.WriteLock` writes atomically, so it needs to create a temporary file in the
directory too. The server starts and serves normally; only the approve button fails, which is a
poor place to discover a mount option. Resolved in `compose.yaml`, which mounts `/config`
writable, with `docs/docker.md` explaining why and when `:ro` is nonetheless reasonable.

### There was no supported way to produce an Argon2id password hash

`auth.mode: password` requires an Argon2id PHC string in `auth.admin.passwordHash`, and Veduta
refuses to bind a non-loopback address without authentication - so producing that string sat on
the critical path of every container install, and nothing in the project produced one. The CLI had
no such command, and neither guide said how to make the value both told the operator to set; only
the test helpers constructed one. The sole workaround was the reference `argon2` CLI, an
undocumented extra dependency on a deployment story whose premise is one static binary in a
shell-less image.

Resolved with `veduta auth hash` (`cmd/veduta/auth.go`, `auth.HashPassword`). The password is read
from stdin and never from an argument, where it would land in shell history and in every other
user's `ps`; stdout carries the PHC string alone so it redirects straight into a secret file. The
default cost is RFC 9106's second recommended configuration (64 MiB, t=3, p=4) - the first, at
2 GiB, is not a per-login allocation a Pi-class host can make.

The property worth protecting is that generation and verification cannot drift: a `veduta auth
hash` capable of emitting parameters `serve` then refuses would be worse than no command at all,
because the failure would appear at start-up on the operator's server with the password already
committed to a secret store. So the cost bounds moved into one `checkCost` that both
`parseArgon2ID` and `HashPassword` call, and `HashPassword` parses and verifies its own output
before returning it. `TestHashPasswordProducesAHashLoginAccepts` drives a real `Service.Login`
rather than the parser alone, and `TestHashPasswordRefusesWhatVerificationWouldRefuse` walks each
boundary. The CLI checks the full range before reading stdin, so an unusable cost is refused
before the operator is asked for a password rather than after - a distinction the first version of
that test failed to pin, passing on "no password on stdin" instead.

Echo is not suppressed on a terminal: doing that portably means a terminal-handling dependency,
and CONTRIBUTING caps the direct Go module budget at about ten, which `go.mod` currently sits
exactly at. The command says plainly that the input is visible, and the docs give the `read -rs`
form for when it matters. Worth revisiting if `golang.org/x/term` ever earns its place for another
reason.

### A declarative card with no `params:` block panicked the server

Found by the `compose.yaml` smoke test above, and the reason that file earns its place: nothing
else had ever run an approved declarative integration from a card that omitted `params:`.

`internal/app/runtime.go` marshals `card.Params` with `json.Marshal`, and a nil map marshals to
the JSON literal `null` - four bytes, so `declarative.(*instance).Invoke`'s `len(req.Params) > 0`
guard passed, `Decode` wrote a nil map over the empty one it had been given, and
`applyParamDefaults` panicked writing the manifest's default into it. The scheduler goroutine has
no recovery, so the process died; under `restart: unless-stopped` it became a crash loop that
re-panicked on the same card every few seconds.

Every existing test passed `{}`. None passed `null`, which is the only value an ordinary
configuration actually produces - `examples/veduta.yaml` gives each declarative card a `params:`
block, so the example that would have caught this was the one configuration that could not.

Resolved by restoring the empty map after decoding, so "no parameters" and "an empty object" mean
the same thing and defaults still apply (skipping `applyParamDefaults` on nil would have been the
smaller change and the wrong one - it would silently drop the manifest's defaults for exactly the
cards that rely on them). `applyParamDefaults` also returns early on a nil map, because a nil map
type-asserts to `map[string]any` and the recursion can reach one through a nested `"key": null`.
`TestInvokeWithNullParamsAppliesDefaultsInsteadOfPanicking` covers `null`, `{}` and no bytes at
all, and fails against the old code with the original panic.

That the panic was *fatal* is tracked separately and is still open - see
[03-backlog.md](03-backlog.md).

### No committed compose.yaml, despite A4 planning one

A4 listed `compose.yaml` among its deliverables and it was never written; `docs/docker.md` carried
two fragments in prose - the Veduta service in one section, the socket proxy in another - that a
first deployment had to transcribe and merge. Neither was exercised by anything, which is how the
`:ro` mount and the missing `/data` owner survived.

Resolved with a `compose.yaml` at the repository root that is a deployment rather than a fragment,
with `docs/docker.md` now explaining its choices instead of restating them. It differs from what
the docs described in five ways, each verified by running it:

- The password hash is a Compose **secret**, not an environment variable. Compose mounts it at
  `/run/secrets/VEDUTA_ADMIN_HASH`, which is exactly where `secrets.DefaultResolver` looks first -
  and `docker inspect` shows a container's environment to anyone who can reach the daemon.
- `read_only: true`, `cap_drop: [ALL]` and `no-new-privileges`. Nothing outside the two mounts is
  written, confirmed by running the stack this way through login, a card refresh and the
  HEALTHCHECK.
- The port publishes to `127.0.0.1` only. Veduta speaks plain HTTP and its session cookie carries
  no `Secure` attribute, so a LAN-visible port means session tokens in the clear; a reverse proxy
  is the intended front door and `8099:8099` is documented as the deliberate downgrade it is.
- The socket proxy sits behind a `docker` profile and is pinned to a version rather than
  `:latest`, and its port is not published to the host.
- No `depends_on` between the two: a missing proxy degrades one card to a warning, which is
  better than a dashboard that will not start. Observed directly - the first refresh after
  `up -d` failed DNS resolution and the card recovered on its own at the next one.

## Resolved after the first real deployment

### A panic inside a card refresh killed the process and leaked its single-flight entry

Carried as an open entry from 2026-09-12, raised by the nil-params panic above: that panic was
fixed on the spot, but that *any* panic was fatal was not. `Manager.runShared` called `d.Run` with
no recovery, on a goroutine started by `Manager.Apply`, so a panic anywhere under a card refresh
unwound past `loop` and ended the program - and under `restart: unless-stopped` that is a crash
loop re-panicking on the same card every few seconds, never naming it.

Resolved in two places, because there are two separate invariants and they belong to different
packages.

**The barrier is in `internal/app`**, wrapping both sites where a card's `Run` enters integration
code. That is the boundary where untrusted code is entered, and it is where the card's identity
and the configured logger are both already in scope - the logger being the one `cmd/veduta` wraps
in the secret-scrubbing handler, which matters, because a panic value or a stack frame can carry
arbitrary in-process data and `slog.Default()` would bypass the scrubber. The panic value and its
stack go to the log at error level with the card id; the card gets `scheduler.ErrRunPanicked`,
whose text is fixed and value-free for exactly the reason `ErrSecretInDocument`'s is - a run error
becomes a tile's visible text, and a dashboard is the wrong place to render a panic value. It
classifies as `state.ErrorInternal` rather than `upstream`: nothing was wrong with the service the
card queries, and an operator reading "upstream" would go and check it.

**The flight release is in `internal/scheduler`**, restructured from an inline release on each
return path into a `defer`. This is the half that was more than a `recover()`: a panic also
skipped `close(f.done)` and `delete(m.flights, key)`, so the key stayed occupied with its channel
never closed and every later refresh of that card - from the loop or from the API - blocked on it
for the life of the generation. The card would have sat pending forever with no error, no retry
and nothing logged. That invariant must not depend on a caller in another package installing a
barrier first, so it is enforced structurally rather than by the barrier's existence.

Tests: `TestGuardedTurnsAPanicIntoAFailedRun` asserts both directions at once - the log carries
the card, the panic value and a real stack frame, and the error text carries neither. The
scheduler's `TestAPanickingRunReleasesItsFlightSoTheCardRunsAgain` drives the panic through
`Refresh` rather than `Apply`, since `Apply`'s loop panics on its own goroutine where it would
take the test process down and prove nothing; against the old code it does not fail but *hangs*,
which is the honest reproduction, so its two-second timeout is the assertion.

Verified end to end by reintroducing the original nil-map panic, building the image and running
the compose stack against it: the card showed `error`/`internal` with the fixed text, the log
carried `card=immich-recent panic="assignment to entry in nil map"` with the `applyParamDefaults`
frame, the Docker card on the same dashboard kept its own state, and the container reported zero
restarts and `healthy`. The same build before the fix crash-looped.
