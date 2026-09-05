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

### S2 — Upstream reality check: Immich + Jellyfin · 0.5 d · **blocks E3, G4**

Against real servers, capture as `testdata/`: Immich `/api/server/statistics`, the metadata search
payload, and the exact thumbnail endpoint + auth mechanism; Jellyfin's `X-Emby-Authorization`
format, `/Users/{id}/Items?SortBy=DateCreated`, and `/Items/{id}/Images/Primary`. Record pagination,
whether image endpoints accept the same auth as JSON endpoints, response sizes, redirects — **and
the exact route set each integration needs**, which is now manifest content, not an implementation
detail. Prevents discovering in E3 that Immich thumbnails need a session cookie.

### S3 — Expression and validation bake-off · 0.5 d · **blocks C1, D3, J2**

Implement the same three mappings in `expr-lang/expr` and `cel-go`: Immich stats, a Jellyfin item
list, and `signal("coding-server","fs.root.percent") > 90 for 5m`. Compare ergonomics for non-programmers, error messages, cost, dependency weight — **and
interruptibility, now an acceptance criterion**: write a hostile expression (nested `map` over a
200 000-element array) and prove the evaluator can be stopped by a context deadline. `expr`'s
context-aware mode instruments loops with cancellation checks; if that does not actually interrupt
the hostile case, that is a reason to choose the other candidate. Also measure AST-node counting for
a load-time `exprNodes` limit. In the same spike, confirm
`santhosh-tekuri/jsonschema/v6` errors carry a usable `file:line:col` when combined with `yaml.v3`
Node positions. **Decision produced:** D7.

### S4 — Visual prototype · 1 d · **blocks B1**

Static HTML/CSS of the target dashboard: Jellyfin poster row, Immich photo grid, infrastructure
metrics and progress cards, light and dark, including the **stale** and **error** treatments the
envelope now makes first-class. This is the screenshot that has to make people want the project.

---

## Part 2 — Milestones and backlog

Legend: **⇉** can run in parallel with its siblings · **→** strictly serial.

### Phase A — Skeleton (2 d)

**A1 · Repository bootstrap** · 0.5 d · deps: none
Objective: a repo that builds, lints and tests in CI on day one.
Creates: `go.mod` (Go 1.24+), `Makefile`, `.golangci.yml`, `.github/workflows/ci.yml`,
`cmd/veduta/main.go`, `internal/version/`, `LICENSE` (AGPL-3.0 + the §7 plugin exception), `sdk/LICENSE` and `schemas/LICENSE` (Apache-2.0), `LICENSING.md`, SPDX headers, `CONTRIBUTING.md` with DCO, `.editorconfig`.
Tests: `make check` runs `go vet`, `golangci-lint`, `go test ./...`, `gofmt -l` (must be empty).
Also in CI: `reuse lint` and a dependency-license check that fails on GPLv2-only/SSPL/BUSL/unlicensed.
AC: CI green on a clean clone; `go build ./cmd/veduta` produces a binary that prints its version;
CI matrix builds `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `darwin/arm64`.

**A2 · Svelte SPA + embedding** · 1 d · deps: A1
Objective: `veduta` serves the frontend from the binary.
Creates: `web/` (Svelte 5 + Vite + TS, no SvelteKit), `internal/api/static.go` with `embed.FS`,
`web/vite.config.ts` with a dev proxy to `:8099`, `make dev` (Vite + `air`-style reload), `make build`.
Contracts: static handler serves `index.html` for unknown non-`/api` paths (SPA fallback), sets
`Cache-Control: immutable` for hashed assets and `no-cache` for `index.html`.
Tests: Go test asserting `GET /` returns HTML and `GET /assets/*.js` returns JS with the right
content type; a `web` build test in CI.
AC: `go build` after `npm run build` yields one binary that serves the app with no Node at runtime.

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

**B1 · Design tokens and layout** · 1 d · deps: A2, S4
Creates: `web/src/styles/tokens.css`, `themes/{light,dark}.css`, `web/src/lib/Grid.svelte`,
`Card.svelte`, `Section.svelte`, typography and spacing scales, `prefers-color-scheme` + explicit
theme override.
AC: the S4 prototype is reproduced within Svelte; a card grid reflows correctly at 360/768/1280/1920 px;
contrast passes WCAG AA in both themes (checked in CI).

**B2 · Widget Document, signals, and the CardState envelope** · 1.5 d · deps: Part 0 · ⇉ with B1
Creates: `internal/widgets/document.go` (typed structs, per-block-type item unions),
`internal/widgets/validate.go` (schema + hard limits + markdown sanitisation),
`internal/state/cardstate.go` (the envelope; `execution` is written only by the core),
`web/src/lib/types/{widget,cardstate}.ts` **generated** from the schemas.
Contracts: `widgets.Validate(raw []byte) (Document, error)` — the single gate every runtime passes
through; `state.CardState` — the single shape every API and SSE response returns.
Tests: the adversarial corpus from Part 0 runs as a Go table test; limit violations (13 blocks, 13
metrics, 65 KiB, 3 KiB markdown, raw HTML, `javascript:` link, `aspect: 0:0`, a metric item carrying
an image) each rejected with a specific error; a document attempting to set `execution` fields is
rejected; signals not declared in the manifest are rejected; fuzz `Validate` for panics.
AC: Go and TS types are provably generated from one schema (CI fails if regeneration diffs); no
code path lets an integration write any field under `execution`.

**B3 · Block renderers I** · 1 d · deps: B1, B2
Blocks: `status`, `metrics`, `key-value`, `progress`, `list`, `text`.
Creates: `web/src/lib/blocks/*.svelte`, `blocks/registry.ts`, `format.ts` (bytes/percent/duration/
relative-time/number with locale), unknown-type placeholder.
Tests: Vitest per component incl. formatting edge cases (0, negative, huge, null); a lint rule +
CI grep asserting `{@html}` appears nowhere in `web/`.

**B4 · Block renderers II (media)** · 1 d · deps: B3
Blocks: `image`, `image-grid`, `poster-grid`, `table`, `markdown`, `actions` (rendered disabled in 0.1).
Includes aspect-ratio boxes, lazy loading, blur-up placeholder, error/broken-image state, and
overflow behaviour for long tables.
AC: a 6-photo grid and a 5-poster row render with zero cumulative layout shift (measured in the
Playwright run).

**B5 · Fixture dashboard + visual regression baseline** · 0.5 d · deps: B3, B4
Creates: `testdata/dashboards/showcase.json` (deterministic documents, checked-in local images),
`--fixtures` dev flag serving them, `web/tests/visual.spec.ts`, pinned Playwright browser in CI,
frozen clock, single font/OS environment.
AC: three baseline screenshots (light, dark, mobile) committed; the suite is green twice in a row
with no flake; a deliberate CSS change fails the suite.

### Phase C — Configuration (2.5 d)

**C1 · Config schema, loader, validation** · 1.5 d · deps: A3, S3
Creates: `schemas/config.v1.schema.json`, `internal/config/{load,merge,validate,snapshot}.go`,
`examples/veduta.yaml`.
Contracts: `config.Load(paths...) (*Snapshot, Diagnostics)`; `Snapshot` is immutable and
`atomic.Pointer`-swappable.
Tests: golden configs (valid, unknown key, bad reference, duplicate id, cycle); every error carries
`file:line:col`; `conf.d` merge order; parsing a 500-card config stays under 50 ms.
AC: `veduta --check-config` prints human-readable diagnostics and exits non-zero on error.

**C2 · Secrets** · 0.5 d · deps: C1
Creates: `internal/secrets/` with `env:`/`file:` providers, `secrets.Value` redacting type, a slog
scrubber hook.
Tests: a `Value` never appears in `%v`, `%+v`, JSON, or a slog line; a missing secret is a
diagnostic naming the config location, not a panic; a secret value appearing in a Widget Document
is rejected by validation.

**C3 · Watcher, atomic reload, status** · 0.5 d · deps: C1
Creates: `internal/config/watch.go` (fsnotify + 300 ms debounce), `GET /api/v1/config/status`.
Tests: invalid edit keeps the previous snapshot live and reports the error; valid edit swaps within
1 s; rapid successive edits coalesce; watcher survives editor rename-and-replace saves (vim/VS Code).

### Phase D — Connections, broker, declarative runtime (5 d) — the security core

**D1 · Connection registry and HTTP client** · 1.5 d · deps: C1, C2
Creates: `internal/connections/` (types, registry, client factory, auth injection, per-connection
rate limiter and semaphore, IP-pinning dialer, redirect policy, response size cap).
Tests (security-critical, all must exist): auth injected for header/query/bearer/basic; path
traversal rejected; absolute URL in `Path` rejected; redirect to another host refused; oversized
response truncated with an error; timeout honoured; rate limiter enforced; a rebinding DNS stub
does not change the dialed IP mid-request; `InsecureSkipVerify` requires explicit config and logs a warning.

**D1b · Route canonicalisation and matching** · 1 d · deps: D1, Part 0
Objective: the one routine every authority check shares.
Creates: `internal/connections/routepath/` — `Canonicalise(raw) (string, error)` and
`Match(pattern, path) bool`, used by manifest load, lock load, `http.request`, asset mint, asset
serve and connection `allowedPaths`.
Tests: `%2e%2e`, `%2E%2E`, `%2f`, `%5c`, literal backslash, control characters, malformed escapes,
`//`, `.` and `..` segments each rejected; `Canonicalise` is idempotent (property test); decoding
never changes segment count; `*` never crosses `/`; `**` is not accepted anywhere; **a fuzz target
asserting that no input is accepted by `Match` after canonicalisation but rejected before it, or
vice versa**; a test asserting all six call sites (manifest load, lock load, `http.request`, asset mint, asset serve, connection `allowedPaths`) route through this package (import check).
AC: no other package in the tree performs path comparison or unescaping.

**D2 · Capability broker with route grants** · 1.5 d · deps: D1, D1b, Part 0
Creates: `internal/capabilities/` — `Grant` (incl. `Routes`), `Broker`, route matcher (glob only:
delegating all path handling to D1b), HTTP/cache/log implementations, per-invocation
budgets, typed denials, denial metrics + audit.
Tests, all mandatory: capability not granted → `ErrCapDenied`; slot not granted → `ErrSlotDenied`;
**allowed slot + allowed path + wrong method → `ErrRouteDenied`**; **allowed slot + unapproved path
→ `ErrRouteDenied`**; a query key outside a route's `queryKeys` allowlist is denied while `nil`
(unconstrained) and empty (no query at all) behave differently; a body whose `Content-Type` differs
from the declared one after normalisation is denied; a body over the effective ceiling is denied;
**each of the three policies is tested independently — a request permitted by the manifest and the
lock but forbidden by the connection's `allowedPaths` is still denied**; `AssetRef` against a `use: data` route denied and `HTTP` against a
`use: asset` route denied; a plugin-supplied `Authorization`, `Host`, `Content-Length` or
connection-owned header is dropped, not forwarded; **a plugin-supplied query key that the connection
owns (`auth.type: query`) is discarded**; authorisation is evaluated as three independent checks
(manifest, lock, connection) with a test proving no glob-intersection shortcut exists; budgets
enforced; cache namespacing prevents cross-plugin reads.
AC: 100% statement coverage on the four-check preamble of every broker method; a route-denial test
exists for every method in the `Route.Method` enum.

**D2b · Integration lock and approval flow** · 1 d · deps: D2, C1
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

**D3 · Declarative runtime** · 2 d · deps: D2b, S3, B2
Creates: `internal/integrations/declarative/` (manifest loader, template-grammar validator for the
four node kinds, pipeline executor, `expr` environment, output builder, signal emitter) and
`internal/integrations/manifestload/` (pre-parse limits: byte cap, YAML depth/node/alias caps,
duplicate-key rejection, then schema validation, then aggregate expression/template ceilings).
Also creates: the resource budget from §5 — streaming decode with `inputMB`/`jsonDepth`/`jsonNodes`
caps, a load-time `exprNodes` check, and one shared `iterations` counter decremented by every
`each`/`map`/`filter`/`sortBy` and template expansion.
Tests: fixture-driven — a manifest + recorded HTTP responses produce a byte-identical golden
Widget Document; **adversarial: a billion-laughs manifest is refused before parsing completes; a
manifest with duplicate mapping keys is rejected rather than silently overwritten; a manifest of
hundreds of maximum-sized expressions is refused by the aggregate ceiling; a `sortBy` over a large
array is charged n·log n and exhausts the budget; a 50 MB upstream response is refused at decode
without allocating it; a nested `map`/`filter`/`sortBy` over a large array exhausts the iteration budget and fails the
card rather than the process; an expression with 5 000 AST nodes is rejected at load; a pipeline that
builds a huge intermediate list and then takes six elements is stopped on intermediate size, not
final output; a benchmark asserts a hostile manifest cannot exceed its deadline by more than one
evaluation step**; **a pipeline request with no matching manifest route is a load error, not a runtime
denial** (the declarative runtime must not be a way around D2's checks); an `{asset}` node against a
non-asset route is a load error; expression errors report a YAML path; a manifest requesting an
undeclared slot or missing the `http` capability is rejected at load; an operation emitting an
undeclared signal fails validation.
AC: an integration is added by dropping one YAML file in and approving it once, with no rebuild.

**D4 · Generic HTTP/JSON card** · 0.5 d · deps: D3
Objective: Homepage's `customapi` equivalent, defined inline in a card without a manifest — the
migration workhorse.
AC: a card with `integration: http-json` plus a `mapping:` block renders metrics from any JSON API.

**D5 · Connection health and admin endpoints** · 0.5 d · deps: D1 · ⇉ with D3/D4
Creates: `GET /api/v1/connections`, `POST /api/v1/connections/{id}/test`, health tracked per connection.
Tests: the response contains no credentials (asserted by scanning the JSON for every configured
secret value); test endpoint distinguishes DNS / TCP / TLS / auth / HTTP-status failures.

### Phase E — Assets and vertical slice #1 (2.5 d)

**E1 · Asset token and proxy endpoint** · 1 d · deps: D2
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

**E2 · Asset disk cache** · 0.5 d · deps: E1
Creates: `internal/storage/assetcache/` (sha256-addressed files, LRU eviction to a byte budget,
metadata in `asset_cache`).  Note: depends on F1 for the table — either land F1 first or use an
in-memory cache and follow up.
Tests: cache hit avoids upstream; eviction respects the budget; corrupt file is re-fetched;
concurrent requests for the same asset coalesce.

**E3 · Immich integration — VERTICAL SLICE #1** · 1 d · deps: D3, E1, S2
Objective: **the demo.** Dashboard → Immich → six most recent photos → signed proxy → browser.
Creates: `plugins/immich/manifest.yaml` (declarative), `testdata/immich/*.json`, golden documents,
docs page.
Tests: fixture test producing a golden Widget Document; an end-to-end test with an httptest server
impersonating Immich, asserting the browser-visible HTML contains no API key and images load through
`/api/v1/assets/`.
AC: with a real Immich configured, six photos render in under 2 s cold; the Immich API key appears
nowhere in the page source, network tab, or logs.

### Phase F — Persistence, scheduling, live updates (4 d)

**F1 · SQLite storage and migrations** · 1 d · deps: A3
Creates: `internal/storage/` (open with WAL/pragmas, embedded forward-only migrations, the thirteen
tables from §10), `--data-dir` layout.
Tests: migrations apply on an empty DB and are idempotent; **changing a card's integration,
operation, params or slot bindings changes its `card_hash` and discards the retained document rather
than displaying the previous card's data; editing a rule expression resets its debounce window**; concurrent readers during a write;
corrupted DB reports a clear error; the janitor prunes `signal_history` and `events` to their retention.

**F2 · Scheduler** · 1 d · deps: D3, F1
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

**F3 · Card state and stale-while-revalidate** · 1 d · deps: F1, F2, B2
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

**F4 · SSE hub and live frontend** · 1.5 d · deps: F3, B3
Creates: `internal/api/sse.go` (hub, per-session and global caps, 20 s heartbeat, 256-entry replay
ring, `Last-Event-ID`, `X-Accel-Buffering: no`), `web/src/lib/stream.ts` (store, backoff reconnect,
connection indicator).
Tests: Go test with 100 concurrent subscribers; slow-consumer disconnect does not block the hub;
**ring overflow, a server restart and an unknown `Last-Event-ID` each produce `event: reset` and the
client refetches `/cards`; a duplicated replay is idempotent because every event is a complete
envelope; reconnecting mid-publication loses nothing**; Playwright test asserting a card updates
without a reload.

### Phase G — WASM integrations and vertical slice #2 (4 d)

**G1 · WASM runtime** · 1.5 d · deps: S1, D2
Creates: `internal/integrations/wasm/` (Extism/wazero host, compilation cache, sha256 verification,
memory/deadline/output limits, instance lifecycle).
Tests: **the conformance suite from S1, promoted to CI** — no filesystem, no env, no sockets, no
native HTTP, deadline kill, memory trap, output cap, and a corrupted module rejected before compile.

**G2 · Host functions** · 0.5 d · deps: G1
Bind `veduta_http`, `veduta_cache_get/put`, `veduta_asset_ref`, `veduta_log` to the broker; encode
denials as plugin-visible errors rather than traps.
Tests: each host function's denial path; a plugin that ignores an error and retries is budget-capped.

**G3 · Plugin SDK and build tooling** · 1 d · deps: G2 · ⇉ with G4 start
Creates: `sdk/rust/` and `sdk/go/` (typed Widget Document builders + host bindings), `plugins/Makefile`,
a `plugins/examples/hello/` template, and a `veduta plugin validate <file.wasm>` command that runs the
conformance suite against a third-party module.
AC: a new plugin can be scaffolded and built in under five minutes following the README.

**G4 · Jellyfin plugin — VERTICAL SLICE #2** · 1 d · deps: G2, S2
Chosen because it needs real logic: build the `X-Emby-Authorization` header, resolve the user, list
recently-added items, and mint poster asset refs.
Tests: fixture test against recorded Jellyfin responses producing a golden document — **the same
golden test must also pass if the integration is reimplemented as builtin Go**, proving the runtime
swap is faithful.
AC: five posters render in the same visual style as the Immich grid, with no Jellyfin key reaching
the browser.

### Phase H — Authentication (2 d) — **H1 moves before E3**; ⇉ with F/G otherwise

**Ordering change:** H1 (sessions) is now scheduled immediately after C2 and before E3/A4, so the
first build that talks to a real credentialed service can also be exposed safely. Until H1 lands the
server binds loopback only. H2 remains where it is and must land before any public release.

**H1 · Sessions** · 1 d · deps: F1
Creates: `internal/auth/` (argon2id via `x/crypto`, session store, cookie flags, CSRF double-submit,
login rate limiting and lockout), login UI, `POST/DELETE /auth/session`, `GET /auth/me`.
Tests: wrong password is constant-time-ish and rate-limited; session fixation prevented by rotation;
CSRF rejected on a cross-origin mutation; expired session cleaned up; logout revokes server-side.

**H2 · Forward-auth, `auth: none` gating, audit log** · 1 d · deps: H1
Creates: trusted-header mode with a trusted-proxy CIDR allowlist, `internal/audit/`, a persistent UI
banner when auth is disabled, and a refusal to start with `auth: none` when actions or secrets exist
unless `--i-know-what-im-doing`.
Tests: a spoofed auth header from an untrusted source IP is ignored; audit entries written for login,
logout, config apply, plugin load, and every action.

### Phase I — Docker and host metrics (2 d)

**I1 · Docker connection and integration** · 1.5 d · deps: D1, D3
Creates: `internal/connections/docker.go` (~200 LOC over the Engine API via unix socket or TCP; no
`docker/docker` dependency), a builtin `docker` integration (container list, state, health, image,
uptime; optional stats), compose docs for the socket proxy.
Tests: against recorded Engine API responses; a socket-proxy-restricted endpoint returning 403
degrades gracefully; the Docker capability is not reachable from any plugin (asserted).
AC: a card lists containers with per-container status; actions remain disabled.

**I2 · Host overview via Glances/Beszel** · 0.5 d · deps: D3
Creates: `plugins/glances/manifest.yaml` (CPU, memory, disks, network, sensors, uptime) and a
`beszel` variant, plus docs showing the one-line Glances container to run.
AC: the coding-server card from the target screenshot renders with no SSH and no credentials on the host.

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
**L4 · Release 0.1.0** · 0.5 d — multi-arch images, checksums, changelog, generated `THIRD-PARTY-NOTICES.md` served from the UI, a "Source" link in the footer satisfying AGPL §13, a short trademark policy, a screenshot generated by
the visual-regression suite, and an explicit "experimental: the plugin ABI will change" note.

---

## Part 3 — Sequencing

**Total effort: ~42 ideal developer-days.** For one developer at public-release quality — review,
iteration, docs, packaging, the things that always appear — **8–12 calendar weeks** is the honest
number. Roughly a quarter of it (the demo path below) is front-loaded, which is what keeps momentum.

**Critical path (the shortest route to a compelling demo):**
`Part 0 → S2 → S4 → A1 → A2 → B1 → B2 → B3 → B4 → C1 → C2 → D1 → D1b → D2 → D2b → D3 → E1 → E3` — Immich
photos on a beautiful dashboard behind a credential-free proxy, with route-limited authority.
Roughly 16–18 developer-days, demonstrable, screenshot-able, and it validates every architectural
decision that matters. **S1a runs alongside in week one** as an early warning; **S1b and the whole
WASM phase are off this path** — the original plan contradicted itself by placing S1 first while
describing it as only "informing" the broker.

**Then** `F1 → F2 → F3 → F4` makes it live, and `S1b → G1 → G2 → G4` makes it extensible.

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
