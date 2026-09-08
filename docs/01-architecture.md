# 01 — Architecture and contracts

Version 0.1 target. Everything here is a proposal to be adjusted by the spikes in
[02-implementation-plan.md](02-implementation-plan.md), except where marked **frozen**
(changing it later is expensive, so it is decided now).

---

## 1. System shape

```
 Browser ── SPA (Svelte 5, no framework beyond Vite) ───────────────────────┐
   │  GET /api/v1/dashboard          (layout + card descriptors)           │
   │  GET /api/v1/stream             (one SSE connection, all updates)     │
   │  GET /api/v1/assets/{token}     (images; no credentials ever)         │
   │  POST /api/v1/cards/{id}/refresh                                      │
   └───────────────────────────────────────────────────────────────────────┘
                                    │ HTTP
 ┌──────────────────────────────────▼───────────────────────────────────────┐
 │ Veduta core (single Go binary, frontend in embed.FS)                     │
 │                                                                          │
 │  http/          config/          scheduler/        state/                │
 │  ├ api          ├ loader         ├ single-flight   ├ card state          │
 │  ├ sse hub      ├ schema valid.  ├ jitter          ├ bounded history     │
 │  ├ assets       ├ fs watcher     ├ circuit break   └ events              │
 │  └ auth         └ Apply(patch)   └ worker pool                           │
 │                                                                          │
 │  integrations/  ── resolves card → (runtime, operation, grant)           │
 │        ├ builtin  (trusted Go)                                           │
 │        ├ declarative (manifest + expr, no code)                          │
 │        └ wasm     (Extism/wazero)   ── all produce a Widget Document ──┐ │
 │                                                                        │ │
 │  ┌────────────────────── Capability Broker ───────────────────────────┐│ │
 │  │ http.request  cache.get/put  assets.ref  log  events.emit          ││ │
 │  │  ▲ every call carries a Grant: {plugin, slots→connections, caps,   ││ │
 │  │    limits}. Credentials are attached here and never travel out.    ││ │
 │  └────────────────────────────────────────────────────────────────────┘│ │
 │                                                                        │ │
 │  connections/  http | docker   (auth, TLS, timeouts, rate limit, health)│ │
 │  secrets/  env: / file: references, redaction                           │ │
 │  rules/ notify/ audit/  storage/ (SQLite WAL + disk asset cache)        │ │
 └────────────────────────────────┬─────────────────────────────────────────┘
                                  │  sandbox boundary
                     ┌────────────▼──────────────┐
                     │  wazero (via Extism)      │  no fs, no env, no sockets,
                     │  jellyfin.wasm  …         │  memory + deadline + output caps
                     └───────────────────────────┘
```

**The invariant, frozen:** an integration can affect the world only through the broker, and the
broker only ever performs requests matching a `(slot, method, path)` route in that invocation's
`Grant` — a route that appeared in the manifest *and* was approved by the administrator in
`veduta.lock.yaml`. Isolation from the Veduta host is half the promise; **limited authority over
the upstream service** is the other half.

### Package layout

```
cmd/veduta/                 main, flags, wiring
cmd/veduta-agent/           (0.3)
internal/
  api/            http handlers, DTOs, SSE hub
  auth/           sessions, argon2id, forward-auth
  config/         load, validate, watch, Apply, snapshot
  secrets/        resolvers (env:, file:), redaction
  connections/    Connection types, registry, HTTP client factory, health
  capabilities/   Broker + implementations (http, cache, assets, log, events)
  integrations/   Registry, Card→Invocation resolution
    builtin/      trusted Go integrations
    declarative/  manifest interpreter (expr)
    wasm/         Extism/wazero runtime
  widgets/        Widget Document types, schema validation, normalisation
  scheduler/      jobs, single-flight, backoff, circuit breaker
  state/          card state, history, events store
  rules/          expr evaluation, for-duration debounce
  notify/         Notifier interface, ntfy, webhook
  audit/          append-only log
  storage/        sqlite open/migrate, queries, asset disk cache
  homepageimport/ importer + warning report
  version/
web/              Svelte 5 + Vite SPA (built → embedded)
schemas/          JSON Schemas (shared by Go and the frontend)
plugins/          first-party integrations (declarative yaml + wasm sources)
sdk/              plugin SDK helpers per language
examples/  testdata/  docs/
```

Dependency direction is strictly downward: `api → services → domain interfaces → adapters`.
`integrations/*` may import `capabilities` and `widgets` only — never `storage`, `connections`
internals, or `secrets`. Enforced by a `go vet`-style import-boundary test in CI.

---

## 2. Configuration

**Frozen decisions:** YAML is the source of truth; SQLite holds runtime state only; there is
exactly one config tree, assembled from `veduta.yaml` plus an optional `conf.d/*.yaml`.

### Loading pipeline

```
files ─► yaml.v3 into *yaml.Node (comments preserved)
      ─► merge conf.d (later files override, arrays replace)
      ─► expand ${secret:NAME} references → SecretRef markers (values NOT inlined)
      ─► marshal Node → JSON ─► JSON Schema validation (schemas/config.v1.schema.json)
      ─► decode into typed structs with KnownFields(true)
      ─► semantic validation (connection refs exist, slots bound, ids unique, cycles)
      ─► build immutable *config.Snapshot
      ─► atomic swap (atomic.Pointer[Snapshot]); readers never lock
```

Errors report `file:line:col` from the Node, plus the JSON Schema path.

### Live reload

`fsnotify` on the config directory, 300 ms debounce, full re-validate, **swap only on success**.
On failure the previous snapshot stays live and the error is pushed to every browser over SSE and
to `GET /api/v1/config/status`. Reload diffs the snapshot and: restarts changed schedules, closes
removed connections, keeps unchanged card state. Secrets sourced from `file:` re-read on reload;
`env:` requires a restart (documented).

### Writes

Every programmatic mutation goes through one function, from day one:

```go
// config.Store
func (s *Store) Apply(ctx context.Context, p Patch) (Result, error)
```

`Patch` operates on the `*yaml.Node` tree so comments and key order survive; the result is
validated, written atomically (`write temp → fsync → rename`), and picked up by the watcher.
v0.1 calls it only from the Homepage importer. The 0.3 config editor is then a UI over an
existing, tested path.

### Secrets

`${secret:NAME}` resolves via ordered providers: `file:` (a path, trimmed — Docker secrets),
then `env:`. Values are wrapped in a `secrets.Value` type whose `String()`/`MarshalJSON()` return `"***"`.
`.Reveal()` is not open to the codebase at large: it is callable only through three narrow
consumer adapters — `connections` (injecting upstream auth), `notify` (channel tokens) and `auth`
(the admin password hash) — each of which takes a `secrets.Value` and returns no secret-bearing
value to its caller. The import-boundary test in CI enforces that no other package references
`Reveal`. A log hook scrubs known secret values from log lines. **Scrubbing is defence in depth, never a
boundary**: base64, URL-encoding or chunking defeats it, and secrets shorter than 8 characters are
excluded entirely to avoid false positives. The boundary is that plugins never receive secrets at
all; the scrubber only limits the blast radius of a core-side mistake. Encrypted-at-rest storage is a 0.3 concern; the interface
(`secrets.Provider`) already allows it.

---

## 3. Connections — the credential boundary

**Frozen:** integrations never see a URL or a credential; they see a slot name.

```go
type Kind string // "http" | "docker"   (0.3: "ssh", "agent")

type Connection struct {
    ID   string
    Kind Kind
    HTTP *HTTPConfig   // exactly one non-nil
    Docker *DockerConfig
}

// TODO(D1): connections that reach the PUBLIC INTERNET (weather, RSS, market data - see spike
// S4) are a different trust category from LAN services. They want a `scope: lan | internet`
// marker, tighter rate limits, and a rule that an integration granted an internet slot may not
// also hold a LAN slot - otherwise a feed integration becomes an exfiltration path for data read
// from a local service.
type HTTPConfig struct {
    BaseURL          string          // scheme+host+optional base path
    Auth             Auth
    Headers          map[string]string
    TLS              TLSConfig       // InsecureSkipVerify, CAFile, ServerName
    Timeout          time.Duration   // default 10s
    MaxResponseBytes int64           // default 8 MiB
    MaxRedirects     int             // default 0 — see §8 threat model
    AllowedPaths     []string        // optional prefix allowlist, default: any path under BaseURL
    RateLimit        RateLimit       // rps + burst, default 5/10
    Concurrency      int             // default 4
}

type Auth struct {
    Type   AuthType // none | header | query | bearer | basic
    Name   string   // header/query parameter name
    Value  secrets.Value
    User   string
    Pass   secrets.Value
}
```

The registry owns one `*http.Client` per connection (pooled, with the connection's TLS config and
a custom `DialContext` that pins the resolved IP for the life of the request). It exposes:

```go
type Registry interface {
    Get(id string) (*Connection, bool)
    Do(ctx context.Context, id string, req Request) (*Response, error) // auth injected here
    Health(ctx context.Context, id string) Health                       // for /connections/{id}/test
}
```

`Request.Path` is joined to `BaseURL` with traversal rejection (`..`, absolute URLs, and
scheme-bearing paths are errors). Rate limiting and the circuit breaker live here, so every
caller — builtin, declarative, WASM, asset proxy — is limited identically.

---

## 4. The Widget Document and the card-state envelope

**Frozen:** an integration produces exactly one artifact — a Widget Document containing
presentation (`blocks`) and semantics (`signals`). Everything about *when* it ran, whether it is
current, and how it failed is **core-owned** and lives in the card-state envelope. An integration
cannot forge a timestamp, claim freshness, or suppress its own error.

Schemas: [`widget-document.v1`](../schemas/widget-document.v1.schema.json),
[`card-state.v1`](../schemas/card-state.v1.schema.json).

```jsonc
// GET /api/v1/cards/jellyfin-recent
{
  "cardId": "jellyfin-recent",
  "document": {                              // integration-owned
    "schemaVersion": 1,
    "title": "Jellyfin",
    "status": { "level": "ok", "text": "Online" },
    "blocks": [ { "type": "poster-grid", "items": [ /* … */ ] } ],
    "signals": {                             // the ONLY thing rules and history read
      "movies":         { "value": 438,        "unit": "count" },
      "library_bytes":  { "value": 8461937274, "unit": "bytes" },
      "active_streams": { "value": 2,          "unit": "count", "level": "ok" }
    },
    "notices": [ { "level": "warn", "message": "Live TV endpoint unavailable" } ],
    "hints": { "ttlSeconds": 300 }           // advisory; the core clamps it
  },
  "execution": {                             // core-owned, integration cannot write it
    "state": "stale",                        // pending | ok | stale | error | disabled
    "generatedAt": "2026-09-04T21:30:02Z",
    "ttlSeconds": 300, "expiresAt": "…", "staleSince": "…",
    "durationMs": 412, "consecutiveFailures": 2, "nextRunAt": "…",
    "source": { "integration": "jellyfin", "integrationVersion": "1.0.0",
                "runtime": "wasm", "operation": "recently-added",
                "slots": { "server": "jellyfin" } },
    "error": { "code": "upstream", "message": "jellyfin: 502 Bad Gateway", "retryable": true }
  }
}
```

### Legal state combinations

`execution.state` is not a free-form label; the schema enforces which fields may and must accompany
each value, so "ok with no document", "error carrying a document" and "stale with nothing to show"
are unrepresentable rather than merely discouraged.

| `state` | `document` | required | forbidden |
| --- | --- | --- | --- |
| `pending` | must be null | — | `generatedAt`, `expiresAt`, `staleSince`, `error` |
| `ok` | required | `generatedAt`, `ttlSeconds`, `expiresAt` | `error`, `staleSince`, `disabledReason` |
| `stale` | required (last good) | `generatedAt`, `staleSince` | `disabledReason` |
| `error` | must be null | `error` | `disabledReason` |
| `disabled` | permitted (last good) | `disabledReason` | `nextRunAt`, `circuitOpenUntil` |

`disabled` means exactly one thing: **this card will not run again without human intervention.**
`disabledReason` is `unapproved`, `permissions-changed`, `integration-missing`, `config-error` or
`operator`.

An **open circuit breaker is not that.** It is temporary backoff that heals itself, so modelling it
as `disabled` was contradictory — the state forbids `nextRunAt`, yet a circuit's whole behaviour is
"probe again at time T". An open circuit is therefore represented as:

- `stale` + `circuitOpenUntil` + `nextRunAt` (the half-open probe) when a last-good document exists;
- `error` + `circuitOpenUntil` + `nextRunAt` when there is nothing to show.

`circuitOpenUntil` is the **earliest permissible** probe, and `nextRunAt >= circuitOpenUntil` — not
equality, because jitter and scheduling pressure legitimately push the actual probe later. Requiring
equality would have made a correctly-behaving scheduler look broken. The schema enforces that
`circuitOpenUntil` implies `nextRunAt`; the ordering is a cross-field invariant checked by the
loader and by the contract suite.

`circuitOpenUntil` is forbidden in `ok`, `pending` and `disabled`. The distinction the user sees is
the one that matters operationally: *waiting for you* versus *backing off on its own*.

Note the two-level failure model: `execution.error` is a *failed run* (core-recorded, with the last
good document retained); `document.notices` is a *degraded success* the integration reports itself.
Conflating them was the original design's mistake.

### Signals

`signals` is a flat map of dotted lowercase names to `{value, unit, level}`. It exists because
blocks are presentation — labels get renamed, items reorder, array positions are unstable — and
building a rules engine on top of them would be building on sand.

- Every signal a card can emit **must be declared in its manifest** (`operations[].signals`), so a
  rule naming a signal that does not exist is a config-load error, not an alert that silently never
  fires.
- Only signals reach `signal_history`; only numeric signals with `history: true` are retained.
- Rules reference them explicitly: `signal("coding-server", "fs.root.percent") > 90`,
  `state("jellyfin-recent") == "error"`.

### Block types in v1

`status`, `metrics`, `key-value`, `progress`, `list`, `image`, `image-grid`, `poster-grid`,
`text`, `markdown`, `table`, `actions`. Deferred to 0.2+: `sparkline`, `timeline`, `chart`,
`carousel`, `video`, `camera`, `form`.

### Rules baked into the schema

- **Item shapes are per block type.** A `metrics` item takes `label`+`value`; a `poster-grid` item
  takes `image`+optional titles. A universal item object let a metric carry a poster and a progress
  bar at once, which no renderer can honour.
- **No styling.** Only semantic hints: `level`, `format`
  (`number|bytes|percent|duration|relative-time|…`), `emphasis`, `align`. Units, locale and rounding
  belong to the renderer, so integrations do not ship English strings or MB-vs-MiB opinions.
- **No HTML.** `markdown` is a restricted subset, sanitised server-side at validation time.
- **Images only by `ref`** — a broker-minted signed token. There is deliberately no `url` field.
- **Actions by id**, referencing actions declared in config. A document cannot express a command.
- **Hard limits, validated before storage:** ≤12 blocks; per-type item caps (12 metrics, 24
  key-value, 48 media, 100 list/table rows); ≤64 KiB serialized; ≤512 chars per free-text field;
  ≤2 KiB markdown; ≤64 signals. Exceeding any limit fails the run with an `execution.error` rather
  than truncating silently.
- **Empty is meaningful.** `list` and the media grids may be empty and render an `empty` string
  ("No active streams"); `metrics`, `key-value`, `progress` and `status` require at least one item,
  because an empty one of those is always a bug.

## 5. Integrations, manifests and runtimes

### Manifest

Schema: [`plugin-manifest.v1.schema.json`](../schemas/plugin-manifest.v1.schema.json).
A manifest **requests** authority; [`veduta.lock.yaml`](../schemas/integration-lock.v1.schema.json)
**grants** it.

```yaml
apiVersion: veduta.dev/v1
kind: Integration
metadata: { id: jellyfin, name: Jellyfin, version: 1.0.0, license: Apache-2.0 }
spec:
  runtime: wasm
  module: jellyfin.wasm
  sha256: "b1946ac9…"                # verified before compile
  slots:
    - { name: server, kind: http, description: Your Jellyfin server }
  capabilities: [http, cache, assets, log]     # required and explicit; no defaults
  limits: { memoryMB: 64, timeoutMs: 3000, outputKB: 64, httpRequests: 8, responseMB: 4 }
  operations:
    - id: recently-added
      defaultRefresh: 5m
      routes:                        # the complete upstream request surface
        - { slot: server, method: GET, path: /Users,            reason: Resolve the current user }
        - { slot: server, method: GET, path: /Users/*/Items,    reason: Recently added items }
        - { slot: server, method: GET, path: /Items/*/Images/Primary, use: asset, reason: Posters }
      signals:                       # what rules and history may reference
        - { name: movies,         type: number, unit: count, history: true }
        - { name: active_streams, type: number, unit: count, history: true }
      params:
        type: object
        properties:
          limit: { type: integer, minimum: 1, maximum: 24, default: 5 }
```

Three things are load-time errors, not runtime surprises: a route naming an undeclared slot; a
`use: data` route without the `http` capability **or** a `use: asset` route without `assets` (an
asset-only integration must not be forced to request `http` it will never use); and a pipeline
request not covered by any declared route.

### Declarative integrations (v0.1 primary mechanism)

Same manifest with `runtime: declarative`, plus a `pipeline` and an `output` template. The template
grammar has exactly four node kinds and **no name-based inference** — a string is always a literal
string, and an expression is always an `{expr}` node:

| Node | Meaning |
| --- | --- |
| `"Photos"`, `42`, `true` | literal |
| `{ expr: "stats.photos" }` | evaluate an `expr` expression |
| `{ asset: { slot, path, query } }` | mint a signed image ref (checked against `use: asset` routes) |
| `{ each: {expr}, as: name, item: {…} }` | repeat a template over a list |

```yaml
      pipeline:
        - as: stats
          request: { slot: server, method: GET, path: /api/server/statistics }
        - as: recent
          request:
            slot: server
            method: POST
            path: /api/search/metadata
            body:
              json:                              # structured, never a string template
                size: { expr: params.limit }
                order: desc
      output:
        title: Immich
        signals:
          photos: { value: { expr: stats.photos } }
        blocks:
          - type: image-grid
            columns: 3
            empty: No photos yet
            items:
              each: { expr: "take(recent.assets.items, params.limit)" }
              as: photo
              item:
                title: { expr: photo.originalFileName }
                image:
                  asset:
                    slot: server
                    path: { expr: '"/api/assets/" + photo.id + "/thumbnail"' }
                    query: { size: preview }
```

#### The declarative runtime needs its own resource budget

WASM plugins are bounded by linear-memory caps and a wazero deadline. **Declarative integrations are
not** — their expressions evaluate inside the Go process, and a hostile (or merely careless) manifest
can nest `map`/`filter`/`sortBy` over a multi-megabyte upstream response and exhaust the host before
any output limit is reached. Output validation happens *after* the work is done, and `expr` being
non-Turing-complete guarantees termination, not bounded resources. A `context` deadline does not help
either: a synchronous evaluator does not check it unless the evaluator is instrumented to.

Since declarative integrations are advertised as safe to install from strangers, they get an explicit
budget, declared in the manifest and clamped by the core:

| Limit | Default | Enforced |
| --- | --- | --- |
| `inputMB` | 4 | streaming decode aborts past the cap, before any expression runs |
| `jsonDepth` | 32 | during decode |
| `jsonNodes` | 200 000 | during decode, across all pipeline responses |
| `exprNodes` | 512 | **at load time** — AST size and nesting, so a bomb never ships |
| `iterations` | 20 000 | one shared per-invocation counter, decremented by every `each`/`map`/`filter`/`sortBy` step and every template expansion |
| `requestBodyKB` | 64 | request bodies; a route's `maxBodyKB` may only narrow it |
| `outputKB` | 64 | final document, as before |

#### The budget must start before the parser

Everything above applies once expressions run. The **manifest itself is untrusted YAML**, and
parsing and schema validation happen first, so a hostile manifest can win before any of it applies.
These are **core constants**, not manifest-declarable — an untrusted document cannot be trusted to
declare its own ceiling:

| Pre-parse limit | Default |
| --- | --- |
| manifest file bytes | 256 KiB |
| YAML depth / nodes | 32 / 20 000 |
| YAML aliases, and total alias expansion | 100 aliases, 1 MiB expanded — the billion-laughs guard |
| duplicate mapping keys | rejected before conversion (they silently overwrite otherwise, and are a smuggling vector for approval diffs) |
| module file bytes, before hashing or compiling | 32 MiB |

And because `exprNodes` is **per expression**, it bounds nothing on its own — a manifest may contain
hundreds of maximum-sized expressions and an arbitrarily wide output template. So there are aggregate
ceilings too, all checked at load:

| Aggregate limit | Default |
| --- | --- |
| expression AST nodes per operation / per manifest | 4 096 / 32 768 |
| output template nodes / depth | 4 096 / 24 |
| total literal-string bytes in a manifest | 64 KiB |

**Iteration charging is by work done, not by call.** `each`/`map`/`filter` charge one unit per
element; `sortBy` charges `n·log₂(n)` (minimum `n`), because charging once per call makes a sort the
cheapest way to spend the most time.

Two further rules: **intermediate** values are counted, not only the final document — a pipeline that
builds a 50 MB list and then takes the first six has already lost — and the evaluator must be
*interruptible*. Adversarial benchmarks (deeply nested expressions over large arrays) ship with the
runtime, not after it.

#### expr is a parser and evaluator; D3 is the sandbox (D47)

`expr.WithContext` propagates cancellation to **context-aware custom functions** — it does not make
`expr`'s own built-in collection operators (`map`, `filter`, `sortBy`, `all`, `any`, `one`, `none`,
`find`, …) check the deadline, and a synchronous evaluator never interrupts itself unless something
instruments it to. Predicate-taking builtins are parser-special, so disabling and replacing them
would also disable the `.field`/`{...}` predicate syntax. D3 therefore leaves those builtins native
and uses `expr.Patch` to wrap their collection argument in a runtime charging call. The whole cost
is reserved before the builtin starts (`n·log₂n`, minimum `n`, for `sortBy` and `reduce`). Ordinary
collection helpers such as `take` are D3-owned functions. A permitted native loop is not preemptible
mid-call; its absolute size is bounded by the JSON node ceiling, while chained and nested calls share
one iteration budget and are interrupted at their next call boundary. `expr` contributes parsing,
compilation and the pure, host-unreachable expression core (arithmetic, comparisons, string
handling, closures); D3 contributes every operation whose cost can scale with its input.

This makes the manifest DSL a **deliberately smaller, explicitly supported surface**, not "whatever
`expr` happens to ship": `map`, `filter`, `sortBy`, `take`, `sum`, `len`, arithmetic/comparison/
boolean operators, string helpers, `bytes`, `date`, and whatever further template-specific helpers
a card actually needs — reviewed for charging semantics before it is added, since an unreviewed
`expr` builtin that turns out to scale with input is a sandbox hole the moment a manifest starts
relying on it. The first draft of this format mixed quoted expressions (`'"Photos"'`), an
`items.expr` escape hatch and a `{{ … }}` body template — three grammars with property-name-dependent
evaluation rules. One language, explicit nodes, predictable escaping, error messages that point at a
YAML path, and a supported-function list that is part of the product's compatibility promise, not an
implementation detail of which library backs it.

**D3's acceptance test:** given an expression containing nested collection operations over a large
upstream payload, evaluation terminates when any configured iteration budget, AST complexity limit,
result-size limit or nesting-depth limit is exceeded. Deadlines are checked at every D3-controlled
call and top-level evaluation boundary; a native predicate builtin already in progress completes
within the independent JSON-size bound. Hostile cases to cover: nested `map(filter(map(...)))`,
an expensive `sortBy`,
deliberately huge intermediate results, deep object traversal, and a custom function that receives
context and is slow to respond to it.

See [`plugins/immich/manifest.yaml`](../plugins/immich/manifest.yaml) for the complete worked example.

### Runtime interface

```go
package integrations

type Runtime interface {
    Name() string                                   // "declarative" | "wasm" | "builtin"
    Load(ctx context.Context, p Installed) (Instance, error)
    Close(ctx context.Context) error                // waits for loaded instances, releases caches
}

type Instance interface {
    Operations() []OperationSpec
    Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
    Close(ctx context.Context) error
}

type InvokeRequest struct {
    Operation string
    Params    json.RawMessage    // validated against operation.params first
    Grant     capabilities.Grant // slots → connection ids, allowed caps, limits
    Deadline  time.Time
}

type InvokeResponse struct {
    Document widgets.Document    // already schema-validated by the runtime wrapper
    Diags    []Diagnostic
}
```

Three implementations satisfy this; nothing above it knows which is in use. That is the migration
path from built-in Go integrations to community WASM ones: re-implement the same operation ids
behind a different `Runtime`, flip `spec.runtime` in the manifest, and every consumer — scheduler,
cache, state, renderer, tests — is unchanged. The golden-fixture tests for the builtin version are
then run unmodified against the WASM version, which is how you prove the swap is faithful.

### WASM specifics

- Host: **wazero** via **Extism**, cgo-free. `PluginRuntime` isolates the choice.
  See the [G1 sandbox decision](spikes/s1-wasm-sandbox.md) for the implemented ABI and conformance coverage.
- `allowed_hosts: []` (empty = deny all — `null` means allow all and must never be used).
  G1 also rejects native HTTP imports at load; conformance tests cover both spellings.
- Per-instance: memory cap (`WithMemoryLimitPages`), `WithCloseOnContextDone(true)` plus a
  per-invocation `context.WithTimeout` (wazero has no fuel metering), no WASI filesystem, no env,
  no args. G1 disables WASI entirely, including stdout/stderr; bounded logging comes through G2.
- Compilation cache on disk keyed by module sha256; modules compiled once, instantiated per call.
- Module bytes verified against `spec.sha256` before compile.

### Host functions (the entire plugin-visible surface)

```
veduta_http(req_json)   -> resp_json     // {slot, method, path, query, headers?, body?}
veduta_cache_get(key)   -> value | null  // namespaced per plugin instance
veduta_cache_put(json)                    // {key, value, ttlSeconds}
veduta_asset_ref(json)  -> token          // {slot, path, transform?}
veduta_log(json)                          // {level, msg, fields}
veduta_emit(json)                          // {type, severity, fields}  (0.2)
```

G2 host calls return one envelope: `{"ok":true,"value":...}` on success, or
`{"ok":false,"error":{"code":"route_denied","message":"..."}}` on failure. Stable error codes
are `capability_denied`, `slot_denied`, `route_denied`, `budget_exceeded`, `deadline_exceeded`,
`cancelled`, and `invalid_request`. Denials are values so a plugin can handle them; the broker still
counts every attempt and caps retries through the shared `hostCalls` budget. HTTP response bodies
are embedded as JSON when valid and exposed as `bodyBase64` otherwise. Cache values are JSON.

There is no filesystem, socket, clock-setting, process, environment or database call. Wall time is
provided in the invocation input, not as a host call, so plugin output is reproducible in tests.

---

## 6. Capability Broker

**Frozen:** authority is `(slot, method, path)`, not `(slot)`. Isolating the plugin from Veduta is
only half the promise; the other half is that a plugin bound to Immich cannot delete an album.

```go
package capabilities

type Route struct {
    Slot        string   // slot name, never a connection id
    Method      string   // GET|HEAD|POST|PUT|PATCH|DELETE
    Path        string   // glob: * matches within one segment. There is no ** in v1.
    Use         UseKind  // UseData | UseAsset
    QueryKeys   []string // nil = any key except connection-owned; non-nil (incl. empty) = allowlist
    ContentType string   // "" = none declared; else mime.ParseMediaType-normalised
    MaxBodyKB   int      // 0 = fall back to the manifest ceiling, then the core default. Never unlimited.
}

// QueryKeys distinguishes nil from empty deliberately: nil means "unconstrained", an empty
// non-nil slice means "no query parameters at all". Collapsing them would silently widen a
// route on every round trip through the lock file.

type Grant struct {
    PluginID   string
    Version    string
    InstanceID string
    Slots      map[string]string // slot → connection id (from config)
    Caps       CapSet            // http | cache | assets | log | events

    // The three policies are carried SEPARATELY and evaluated independently. There is no
    // combined route set: intersecting arbitrary globs is not a well-defined operation, and
    // a field named "effective routes" would invite exactly that bug.
    ManifestRoutes   []Route // what the integration asked for
    ApprovedRoutes   []Route // what the administrator approved in veduta.lock.yaml
    ConnectionPolicy map[string]ConnectionPolicy // per slot: allowedPaths, method policy, ceilings

    Limits Limits // requests, bytes, deadline, host calls, cache entries and bytes
    Ident  ExecutionIdentity // see §11: what this invocation was authorised against
}

// Authorize reports whether one concrete request is permitted. Every broker call runs it, and
// it is the ONLY place a request is compared against policy.
func (g Grant) Authorize(req HTTPRequest, use UseKind) error {
    // canonicalise once, then match the same concrete path against each policy in turn
    // (routepath.Canonicalise → matches ManifestRoutes → ApprovedRoutes → ConnectionPolicy).
}

type Broker interface {
    HTTP(ctx context.Context, g Grant, req HTTPRequest) (HTTPResponse, error)
    CacheGet(ctx context.Context, g Grant, key string) ([]byte, bool, error)
    CachePut(ctx context.Context, g Grant, key string, val []byte, ttl time.Duration) error
    AssetRef(ctx context.Context, g Grant, slot, path string, query url.Values, t Transform) (string, error)
    Log(g Grant, level, msg string, fields map[string]any)
    Emit(ctx context.Context, g Grant, e Event) error
}

type HTTPRequest struct {
    Slot   string            // NOT a URL
    Method string
    Path   string
    Query  map[string]string  // connection-owned keys dropped - see below
    Header map[string]string // ALLOWLISTED, and connection-owned names dropped - see below
    Body   []byte
}
```

### How a request is authorised

A concrete request is permitted only if it satisfies **all three policies, each evaluated
independently against the request itself**:

```
canonicalise(request path)
  → matches some manifest route for the invoked operation   (method, path, use, queryKeys,
  → matches some approved route in veduta.lock.yaml          contentType, body ceiling)
  → satisfies the connection's own allowedPaths / method policy
```

Never a precomputed intersection of globs. `manifest ∩ lock` is computed *only* for display, by
exact-tuple comparison of route identities, and only so a human can review what was approved.

If no manifest route can ever satisfy an operation's declared requests, that is a **load-time**
error surfaced in the UI — not a runtime denial discovered on a Tuesday. A declarative pipeline request
with no matching manifest route is likewise rejected at load: the declarative runtime is subject to
exactly the same route checks as WASM, or it would be the way around them.

### Budgets cover every host call, not just HTTP

`httpRequests` bounds upstream traffic; it does nothing about a plugin that mints 10 000 asset refs,
writes 10 000 cache entries or emits 10 000 log lines inside its deadline. A single `hostCalls`
budget (default 256) is decremented by **every** broker method — `HTTP`, `CacheGet`, `CachePut`,
`AssetRef`, `Log`, `Emit` — and `cacheBytesKB` (default 256) bounds cache storage, which
`cacheEntries` alone does not.

### The four checks every broker call starts with

1. capability present in `Caps`;
2. slot present in `Slots`;
3. **route authorisation** — `Grant.Authorize` against all three policies: method exact, canonical
   path glob, `Use` kind, query keys within the allowlist, request `Content-Type` equal to the
   declared one after normalisation, and body size within the effective ceiling;
4. per-invocation budget not exhausted.

`AssetRef` matches only `Use: UseAsset` routes and `HTTP` only `Use: UseData` routes. Without that
split, an integration granted `GET /api/assets/*/thumbnail` for images could mint refs to any path
its data grant forbids — the asset endpoint would quietly become the wider permission.

Failures are typed (`ErrCapDenied`, `ErrSlotDenied`, `ErrRouteDenied`, `ErrBudgetExceeded`),
counted per plugin, written to the audit log and surfaced in the UI. Repeated denials are what a
malicious plugin looks like, so they are a product signal, not just a log line.

Cache keys are namespaced `plugin:{id}:{manifest digest}:{instance}:{key}` — the digest is in the key so an upgraded plugin starts with a cold cache rather than consuming entries written by the version it replaced. Plugins cannot read each other's state.

### Method policy

There is deliberately **no separate `http.write` capability**. Many read-only APIs use POST —
Immich's `/api/search/metadata` is the flagship example — so classifying by verb would either
break real integrations or teach users to grant writes reflexively. The route *is* the permission.
What the verb changes is the **approval prompt**: any non-`GET`/`HEAD` route is highlighted, listed
first, and requires an explicit keystroke to approve.

### Approval and the lock file

A manifest is a *request* for authority. Nothing takes effect until it appears in
`veduta.lock.yaml` ([schema](../schemas/integration-lock.v1.schema.json)), which records the
manifest digest, the module digest, the approved capabilities and routes, and the limits.

```
load manifest → compute sha256(canonical manifest bytes)
  → compare against lock record
      match      → run with the locked permissions
      no record  → integration is `disabled`; UI and CLI offer approval
      mismatch   → REFUSED, with a printed permission diff, until re-approved
```

The digest is **RFC 8785 (JSON Canonicalization Scheme)** over the strictly-decoded manifest, not
Python's or Go's idea of "sorted-key JSON" — those disagree about Unicode escaping, about `<`, `>`
and `&`, and about float formatting. Two additional rules remove the rest of the ambiguity:
**manifests may not contain floating-point numbers** (integers only, so ECMAScript number formatting
never arises), and **set-like arrays** (`capabilities`, `queryKeys`) are sorted and de-duplicated
before hashing, so reordering them is not a change of authority and does not churn approvals.
Defaults are *not* filled in first: the digest covers what the author wrote, while the lock records
`effectiveLimits` separately, so a core upgrade that changes a default surfaces as a mismatch rather
than silently altering authority. Golden fixtures in `testdata/canonical/` pin all of this, and both
the Go and the Python implementation must reproduce them byte for byte.

The digest is over the **manifest**, not only the module, because the manifest is what grants
authority — pinning only `module.sha256` is circular, since a swapped manifest supplies the new
module hash too. `source: builtin` integrations are exempt: they ship inside the signed binary and have no separate
trust boundary.

#### Approval is a two-step, digest-bound transaction

Approving an integration hands it access to credentialed services, so it must not be a single
`POST` that approves "whatever the manifest currently says" — that is a time-of-check/time-of-use
window in which the reviewed manifest and the approved manifest can differ.

```
1. GET  /api/v1/integrations/{id}/approval
       → { manifestSha256, currentLock, diff: { addedRoutes[], removedRoutes[],
             addedCapabilities[], raisedLimits[], bodyBearingRoutes[] } }
2. POST /api/v1/integrations/{id}/approve
       { expectedManifestSha256, grants: { capabilities[], routes[] } }
3. Server recomputes the digest; if it differs from expectedManifestSha256 → 409 Conflict
   with the new diff. Nothing is approved.
```

The client sends the **exact grants it is approving**, not a bare "yes", so approving a subset is
natural and a race cannot widen the grant. `veduta integration approve <id>` performs the same two
steps.

**Re-authentication.** CSRF proves the browser session issued the request; it does not prove a
person is present. Approval therefore requires a fresh authentication within a short sudo window
(default 5 minutes):

| auth mode | requirement |
| --- | --- |
| `password` | re-enter the password; `POST /auth/sudo` opens the window |
| `forward` | `auth.forward.privilegedOperations`: `cli-only` (**default**) or `admin-group`, the latter requiring `groupsHeader` and a non-empty `adminGroups`. Note carefully: a group header is **authorisation, not proof of current human presence** — it is replayed on every request and cannot substitute for re-authentication. `cli-only` is the default for exactly that reason |
| `none` | approval is CLI-only, always |

#### Route canonicalisation — one routine, six call sites

Route matching is security-critical, so the algorithm is part of the contract rather than an
implementation detail. **One** exported function canonicalises, and it is the only path used by
manifest loading, lock loading, runtime `http.request`, asset-token minting **and** asset-token
serving, and connection `allowedPaths`:

```
canonicalise(raw) → (path, error)
  reject if: it does not begin with "/"
             it contains a backslash, a control character, or whitespace
             it contains a malformed percent escape
             percent-decoding would yield "/" or "\" (%2f, %5c, and their mixed-case forms)
             any segment is "." or ".." before OR after decoding
             it contains an empty segment ("//")
  then:      decode the remaining unreserved escapes exactly once, lowercase the
             percent-hex digits, and re-encode to a single normal form
  invariant: canonicalise(canonicalise(x)) == canonicalise(x), and decoding never
             changes the number of segments
```

Glob patterns are additionally restricted at schema level: `*` matches within one segment and never
crosses `/`; **multi-segment `**` is not part of v1** at all, because "`/a/**/b`" has no unambiguous
reading and no first-party integration needs it. `%` is not a legal character in a pattern.

#### Every limit has one effective value

A limit omitted from a manifest does not mean "anything"; it means its documented default. So the
value in force is always:

```
effective(k) = min(core maximum(k), manifest(k) ?? default(k), approved(k) ?? default(k))
```

The lock records the whole `effectiveLimits` map at approval time. Limits are defined **once**, in
`plugin-manifest.v1#/$defs/limits`, and the lock schema `$ref`s that definition, so manifest and
approval can never drift into speaking about different fields with different bounds.

#### Route identity is the full tuple, not the method and path

Comparing routes by `(slot, method, path, use)` alone would let a lock entry silently drop the
constraints a manifest declared. `queryKeys: [safe]`, `contentType: application/json` and
`maxBodyKB: 4` are *authority*, so identity for approval, diffing and subset checks is:

```
(slot, method, canonical path, use,
 sorted unique queryKeys | null,      // null = "any key except connection-owned", which is WIDER
 normalised contentType | null,       // mime.ParseMediaType: lowercased type/subtype, ALL params kept
 effective maxBodyKB)                 // route value, else the manifest's requestBodyKB, else 64
```

Normalisation is `mime.ParseMediaType` + `mime.FormatMediaType`: the type and subtype and the
parameter *names* are lowercased, the `charset` value is lowercased, parameters are sorted, and
values that need quoting keep their quotes — so `profile="a;b"` survives, which naive splitting on
`;` does not. **All** parameters are retained: dropping them would collapse
`application/vnd.api+json; profile=…` into its base type, and an earlier draft of this document
wrongly claimed `application/json` and `application/json; charset=utf-8` were the same route. They
are different routes, and an integration that sends one must declare that one. An **omitted**
constraint is wider than any explicit one, so dropping `queryKeys` in a lock entry shows in the diff
as a widening rather than as no change. There is no "unlimited" body: omitting both `maxBodyKB` and
`requestBodyKB` yields the core default of 64 KiB.

**Matching is three independent evaluations, not a computed intersection.** A concrete request is
authorised iff it matches a manifest route **and** an approved lock route **and** the connection's
policy. Intersecting arbitrary globs is not a well-defined operation and attempting it invites
exactly the subtle bug this section exists to prevent. The UI displays `manifest ∩ lock` for human
review, computed by exact-tuple comparison, which *is* well defined.

#### The `http-json` escape hatch, and its limits

The built-in `http-json` integration lets a card name a path on a credentialed connection with no
manifest and no approval record. That is defensible — configuration is administrator-controlled and
`builtin` integrations ship in the signed binary — but it is authority-bearing, so it is fenced
explicitly rather than left implicit:

- **fixed surface**: `GET` and `POST` only, no custom headers beyond the broker allowlist, a JSON
  body or none, and no redirect following;
- **the connection's `allowedPaths` still applies**, and is the only thing standing between a card
  and every path on that connection — so a connection intended for `http-json` use should set it;
- the same expression and resource budgets as any declarative integration;
- **called out by name in the Homepage-import review**, because an imported `customapi` widget
  becomes exactly this, on a connection whose credentials were also imported.

#### What a route grant cannot express

Method and path describe authority completely only for REST-shaped APIs. For **GraphQL, JSON-RPC and
generic `/api/action` endpoints the operation is selected inside the request body**, so approving
`POST /graphql` approves queries and mutations alike. This is a real limit of the model and it is
disclosed, not papered over:

- the approval UI flags **body-bearing** routes (any `POST`/`PUT`/`PATCH` with a body), not merely
  non-`GET` verbs, and shows the warning inline;
- routes may narrow themselves with `queryKeys`, `contentType` and `maxBodyKB`;
- a request-body schema constraint is a candidate for a later `apiVersion`, when an integration
  actually needs it.

#### Headers and query parameters are allowlisted, not denylisted

The first draft denied a list of known auth headers, which fails open for anything not on the list.
Instead: a plugin-supplied header is dropped unless it is on a small allowlist (`Accept`,
`Accept-Language`, `Content-Type`, `If-None-Match`, `If-Modified-Since`, `Range`), and it is
**always** dropped if the connection owns it — its auth header, any header in the connection's
`headers` map, plus `Host`, `Content-Length`, `Transfer-Encoding`, `Connection` and `Upgrade`, which
are request-smuggling primitives rather than integration business.

The same rule applies to **query parameters**, which the draft overlooked: when a connection
authenticates with `auth.type: query`, that parameter name is connection-owned and any
plugin-supplied value for it is discarded before the request is built.

## 7. The asset/image proxy

The goal: a browser can render an authenticated Immich thumbnail without ever seeing a credential,
and without the endpoint becoming an open proxy.

**Minting (server-side only).** `Broker.AssetRef(g, slot, path, transform)` validates the slot
against the `Grant`, then builds:

```
payload = {
  v:  1,
  c:  "<connection id>",
  cf: "<connection revision>",      // opaque 128-bit id, NOT a hash of any configuration
  p:  "/api/assets/e47493/thumbnail",  // canonical path, percent-normalised, no query
  q:  "size=preview",                  // canonical query: sorted, percent-normalised, separate from p
  t:  "w=320,f=webp",              // must match ^(w=(160|320|640|1280))?(,f=(webp|jpeg))?$
  pl: "immich@0.1.0",                  // minting integration, for audit and revocation
  exp: <unix>
}
token = base64url(payload) + "." + base64url(HMAC-SHA256(instanceKey, base64url(payload)))
```

The **connection revision** is what makes step 2 below enforceable: without it, "reject tokens for
materially changed connections" is unimplementable, because the token carries only an id that can be
repointed at a different host or credential.

**Lifecycle.** The public revision is a random id; what *detects* change is a private, instance-keyed
`HMAC-SHA256(instanceKey, canonical connection config ‖ resolved secret values)` — where `‖` is
**length-prefixed** concatenation of canonically serialised fields, never raw concatenation, so that
`{user: "ab", pass: "c"}` and `{user: "a", pass: "bc"}` cannot collide stored alongside it
in `connection_state` and never exposed anywhere. On every config load the HMAC is recomputed; if it
differs, a fresh random revision is generated. That gives stable revisions across restarts (tokens
survive a reboot) without a browser-visible value derived from a credential. Three consequences,
stated because "whenever the resolved secret changes" is otherwise stronger than any implementation
can observe:

- **`file:` secrets** are watched directly, not only via the config directory; a changed secret file
  triggers a reload of the affected connections and rotates their revisions.
- **`env:` secrets** cannot be observed while the process runs; they change only across a restart,
  and the startup HMAC comparison catches them then. This is documented, not silently assumed.
- **Deleting and recreating a connection with the same id always rotates**, because the row is
  removed with the connection and a new random revision is generated on recreation.

It is deliberately **not** a hash of the connection's configuration. Asset tokens are visible to the
browser (and to anyone the user shares a screenshot with), and a hash covering a credential would be
an offline oracle against a potentially low-entropy password. The revision is a random 128-bit value
persisted in `connection_state`, regenerated whenever the connection's material configuration or its
*resolved* secret value changes. It reveals nothing, and comparing it is a constant-time equality
check rather than a recomputation. Path and query are signed **separately and
canonically**, so `?x=1&y=2` and `?y=2&x=1` are one cache entry and neither can smuggle extra path
segments through the query.

The plugin receives only `token`. It cannot forge one (no key), cannot alter one (MAC), and cannot
name a connection it was not granted (checked at mint time). **Frozen: plugins never construct refs, and `AssetRef` mints only against `use: asset` routes.**

**Serving.** `GET /api/v1/assets/{token}`:

1. Verify MAC in constant time; reject on failure without detail. Check `exp` (default 24 h).
2. Resolve connection; compare the token's `cf` against the connection's **persisted** revision and reject on mismatch (repointed host, rotated auth type, changed TLS policy, rotated secret). No recomputation, and no configuration value is hashed.
3. Re-check the signed path/query against the minting integration's **approved `use: asset` routes** as they stand now — approval may have been revoked since minting. Then path safety: no `..`, no absolute URL, must be under `BaseURL` (+ `AllowedPaths` if set).
4. Fetch through the connection registry — same auth injection, timeouts, rate limit, IP pinning,
   redirect policy as every other request.
5. Response guards: `Content-Type` must match `image/(jpeg|png|webp|gif|avif)` (sniffed, not
   trusted); `Content-Length`/streamed size ≤ 8 MiB; decode dimension caps.
6. Optional transform (0.2): resize with `x/image/draw` to a small allowlist of widths
   (`160,320,640,1280`), re-encode to webp/jpeg. **0.1 is pass-through** — but "pass-through avoids
   parsing hostile data" would be false: enforcing the dimension caps in step 5 requires
   `image.DecodeConfig`, which parses attacker-controlled headers even when no pixels are decoded.
   That is a real parsing boundary, it is stated rather than glossed, and it is fuzzed (milestone
   L3) with truncated, oversized-dimension and malformed-header images of every accepted type.
   Full decode and re-encode is the larger exposure and is what waits for 0.2.
7. Cache to disk keyed by `sha256(payload minus exp)`, with an LRU byte budget (default 512 MiB)
   and metadata in SQLite. Serve with `Cache-Control: private, max-age=…, immutable`.
8. Response headers: `Content-Security-Policy: default-src 'none'; sandbox`,
   `X-Content-Type-Options: nosniff`, `Content-Disposition: inline`.

The signing key is generated on first boot into `settings` and rotated on demand; rotation
invalidates outstanding tokens, which is acceptable because cards re-render within one refresh.

---

## 8. Threat model

| Threat | Vector | Mitigation |
| --- | --- | --- |
| **Malicious plugin — exfiltration** | Community module tries to steal secrets or reach the LAN | No secrets in the sandbox (broker injects auth); no raw sockets, fs, env; slot-scoped HTTP only; per-invocation budgets; denials logged and surfaced |
| **Malicious plugin — abusing legitimate access** | Module bound to Immich calls `DELETE /api/albums/{id}` | **Route grants**: authority is `(slot, method, path glob)`, intersected with administrator approval in `veduta.lock.yaml` and the connection's own policy. Non-GET routes are highlighted at approval. Declarative integrations are checked identically. `assets.ref` mints only against `use: asset` routes |
| **Permission escalation by manifest swap** | Replacing a local manifest silently widens capabilities | The lock record digests the **manifest**, not just the module; any change to capabilities, routes or limits refuses the integration until re-approved with a printed diff, and the approval is audited |
| **Plugin DoS** | Infinite loop, memory bomb, 5 MB output, 10 000 requests | Memory cap; context deadline + `CloseOnContextDone`; output size cap; request count/byte budgets; per-plugin concurrency 1; global worker pool; circuit breaker backs a repeatedly failing card off to a scheduled half-open probe |
| **Compromised upstream service** | Immich returns a hostile payload | Widget Document schema validation with hard caps; no HTML from integrations; markdown sanitised; images content-type-sniffed and size-capped; JSON decode depth/size limits; upstream failures degrade to a stale card, not a blank dashboard |
| **Malicious HTML/API response → XSS** | Attempt to inject markup through titles or markdown | Svelte escapes by default; **zero** `{@html}` in the codebase, enforced by a lint rule and a CI grep; markdown rendered by a trusted allowlist renderer; strict CSP (`default-src 'self'; img-src 'self'; script-src 'self'; object-src 'none'; frame-ancestors 'none'`) with no inline scripts |
| **SSRF / confused deputy** | Plugin or forged token makes the server hit an arbitrary host | Requests name slots, never URLs; asset tokens are HMAC-signed and slot-checked at mint; path traversal rejected; scheme allowlist (`http`,`https`); redirects not followed across hosts (`MaxRedirects: 0` default) |
| **DNS rebinding** | Hostname validates, then resolves elsewhere | Resolve once per request and dial the pinned IP; re-validate after any permitted redirect |
| **Stolen dashboard session** | Cookie theft or an open tab | `HttpOnly`, `Secure` (when TLS), `SameSite=Lax`; server-side session records revocable individually; rotation on login; idle + absolute expiry; CSRF double-submit token on every mutating request; actions audited with actor + source IP |
| **Config is a trust boundary** | Whoever edits YAML can define a connection to anything with any credential | Config is admin-only, file-system-protected; the Homepage importer marks all imported connections as *review required* and does not enable them until confirmed; changes are audited |
| **Docker socket exposure** | Root-equivalent host access | Socket proxy (read-only, `CONTAINERS=1 INFO=1`) is the documented default; never exposed to plugins; container actions off unless explicitly enabled per connection |
| **SSH credential exposure** | Dashboard holds keys to servers | **Not in 0.1.** When added: dedicated unprivileged user, `known_hosts` pinning (no `accept-new`), fixed command allowlist with typed parameters, never reachable from a plugin, every use audited |
| **Notification abuse** | Flapping rule floods the user's phone | Per-rule `for:` debounce; dedupe key with cooldown; per-channel rate limit and hourly cap; outbox with bounded retry; a rule that fires more than N times/hour auto-suspends and reports |
| **Credentials smuggled through a URL** | `baseUrl: http://user:pass@host` bypasses the typed auth path, its redaction, and makes `auth: none` look secret-free | Connection base URLs reject userinfo, query strings and fragments, structurally and in the loader. Webhook URLs are the deliberate exception — tokens in the path are the norm there — so a channel URL is a `secrets.Value`, logged only as scheme://host plus the channel id |
| **Manifest parsed before it is budgeted** | Billion-laughs YAML aliases, a 500 MB manifest, or thousands of maximum-sized expressions | Pre-parse core limits (bytes, depth, nodes, aliases, alias expansion, duplicate keys, module size) enforced before schema validation, plus aggregate expression/template ceilings at load. See §5 |
| **Host-call flooding** | Plugin mints 10 000 asset refs or log lines inside its deadline | One `hostCalls` budget across every broker method, plus `cacheBytesKB` |
| **Secret leakage into output/logs** | Plugin echoes config, panic prints a URL | Boundary: plugins never receive secrets. Defence in depth only: `secrets.Value` redacts in `String()`/JSON; a log scrubber for values ≥8 chars; a CI test asserting no configured secret appears in any HTTP response. Scanning is explicitly **not** a control — encoding or chunking defeats it |
| **Approval TOCTOU** | Manifest is swapped between the reviewed diff and the approve click | Approval is digest-bound: the client sends `expectedManifestSha256` and the exact grants; a mismatch is a `409` with the new diff and nothing is approved |
| **Approval without a person present** | Stolen session silently approves a hostile integration | Fresh re-authentication (sudo window) required in password mode; CLI-only approval under forward-auth and `auth: none`; every approval audited with actor, IP and diff |
| **Body-selected operations** | `POST /graphql` route approved for queries is used for mutations | Acknowledged limit of route grants. Body-bearing routes are flagged at approval; `queryKeys`/`contentType`/`maxBodyKB` narrow them; body-schema constraints are a future `apiVersion` |
| **Header or query injection into an authenticated request** | Plugin sets `Authorization`, `Host`, or the connection's own `api_key` query parameter | Allowlist, not denylist, for plugin-supplied headers; connection-owned headers and query keys are stripped unconditionally; `Host`, `Content-Length`, `Transfer-Encoding`, `Connection`, `Upgrade` never settable |
| **Asset token as a credential oracle** | Browser-visible token embeds a hash covering a low-entropy password | The token carries an opaque random connection *revision*, never a hash of configuration or secrets |
| **Path-canonicalisation mismatch** | `%2e%2e`, `%2f`, mixed-case escapes, or double decoding differ between the approval check and the request | One shared canonicalisation routine used by manifest, lock, runtime, asset mint, asset serve and connection policy; encoded separators and dot segments rejected; idempotence asserted by test and fuzzed |
| **Asset endpoint as an open image proxy** | Token brute force or replay | 256-bit MAC, constant-time compare, TTL, no error detail, rate limited per session |
| **SSE resource exhaustion** | Many tabs or a script opening streams | Per-session and global stream caps; heartbeats; bounded replay ring |
| **Supply chain** | Malicious update to an installed plugin | sha256 pinning in the manifest; explicit upgrade with a permission diff; signature verification designed for 0.3 |

---

## 9. REST API (v0.1)

All under `/api/v1`, JSON, cookie-authenticated, CSRF token required on mutations.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | liveness; no auth |
| GET | `/version` | build info **+ AGPL §13 source URL for the exact commit**; no auth |
| POST | `/auth/session` | login (password) → cookie + CSRF token |
| DELETE | `/auth/session` | logout |
| GET | `/auth/me` | current identity, auth mode, capabilities |
| GET | `/dashboard` | pages, sections, card descriptors (no data) |
| GET | `/cards` | `CardState` envelope (document + core-owned execution) for every card |
| GET | `/cards/{id}` | one `CardState` |
| POST | `/cards/{id}/refresh` | force refresh (rate-limited, single-flighted) |
| GET | `/stream` | SSE: `card`, `config`, `event`, `hello`, `ping` |
| GET | `/assets/{token}` | image proxy (§7) |
| GET | `/icons/{spec}` | icon proxy + disk cache |
| GET | `/connections` | admin: ids, kinds, health — **never** credentials |
| POST | `/connections/{id}/test` | connectivity + auth probe |
| GET | `/integrations` | installed integrations: version, runtime, lock status (`approved`/`unapproved`/`changed`), granted capabilities and routes |
| GET | `/integrations/{id}/approval` | the current manifest digest plus the permission diff to review — read-only |
| POST | `/integrations/{id}/approve` | approve, sending `expectedManifestSha256` and the exact grants; `409` if the manifest changed since the preview; requires a fresh sudo window |
| POST | `/auth/sudo` | re-authenticate to open a 5-minute window for privileged operations (password mode) |
| GET | `/config/status` | last load result, errors with file:line, checksum |
| POST | `/config/validate` | validate a candidate document without applying |
| GET | `/events` | recent events (bounded) |
| POST | `/import/homepage` | run importer → preview + warning report (does not apply) |
| POST | `/import/homepage/apply` | apply a previewed import |

SSE payload example: `event: card` / `data: <CardState>` — the same envelope the REST endpoints return, so the client has one shape to parse.
Heartbeat `event: ping` every 20 s.

**Replay and reset.** `Last-Event-ID` replays from a bounded in-memory ring (256 entries). A ring
cannot answer an arbitrary id after overflow, and it does not survive a restart, so "exactly once"
is the wrong promise. When the requested id is unknown, too old, or from a previous process
lifetime, the server sends `event: reset` and the client refetches `GET /cards`. Every `card` event
carries a **complete** `CardState`, so applying one is idempotent replacement rather than a delta:
a duplicate replay is harmless, and the reset path is always correct if slower. The stream id
includes a process-lifetime nonce so a restart is detected rather than mistaken for overflow.
Response includes `X-Accel-Buffering: no`.

---

## 10. SQLite schema (v0.1 only)

WAL, `busy_timeout=5000`, `foreign_keys=on`, `synchronous=normal`. Migrations are embedded,
forward-only, numbered files applied in a transaction.

```sql
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);

CREATE TABLE settings (            -- instance singletons: asset HMAC key, install id
  key TEXT PRIMARY KEY, value BLOB NOT NULL, updated_at TEXT NOT NULL);

CREATE TABLE users (
  id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL, created_at TEXT NOT NULL, last_login_at TEXT);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL, created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
  last_seen_at TEXT, user_agent TEXT, ip TEXT);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE card_state (          -- integration-owned document + core-owned execution
  card_id TEXT PRIMARY KEY,
  card_hash TEXT NOT NULL,         -- hash of the card DEFINITION; state is discarded when it changes,
                                   -- so reusing an id for a different integration, operation, slots
                                   -- or params cannot resurrect the previous card's data on restart
  manifest_digest TEXT,            -- canonical digest the retained document was produced under
  approval_revision TEXT,
  slot_revisions TEXT,             -- JSON: slot → connection revision (part of the execution identity)
  envelope BLOB NOT NULL,          -- the complete validated CardState, JSON. One source of truth for
                                   -- the API and SSE; no field of the envelope can go missing here
  state TEXT NOT NULL,             -- denormalised for indexing/queries only: the envelope is canonical
  generated_at TEXT, expires_at TEXT, next_run_at TEXT,
  updated_at TEXT NOT NULL);
CREATE INDEX card_state_next_run ON card_state(next_run_at);

CREATE TABLE signal_history (      -- bounded numeric history for rules `for:` windows + sparklines
  card_id TEXT NOT NULL, signal TEXT NOT NULL, ts TEXT NOT NULL, value REAL NOT NULL,
  PRIMARY KEY (card_id, signal, ts)) WITHOUT ROWID;   -- only declared signals with history: true
CREATE INDEX signal_history_ts ON signal_history(ts); -- pruned to N days by a janitor

CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, type TEXT NOT NULL,
  severity TEXT NOT NULL, source TEXT, card_id TEXT, message TEXT, data BLOB);
CREATE INDEX events_ts ON events(ts);

CREATE TABLE plugin_kv (           -- namespaced by DIGEST, so an upgraded or replaced plugin cannot
                                   -- read cache written by the version it replaced
  plugin_id TEXT NOT NULL, manifest_digest TEXT NOT NULL,
  instance_id TEXT NOT NULL, key TEXT NOT NULL,
  value BLOB NOT NULL, expires_at TEXT,
  PRIMARY KEY (plugin_id, manifest_digest, instance_id, key)) WITHOUT ROWID;
CREATE INDEX plugin_kv_expires ON plugin_kv(expires_at);

CREATE TABLE connection_state (    -- opaque revision backing asset-token validation (§7)
  connection_id TEXT PRIMARY KEY,
  revision TEXT NOT NULL,          -- random 128-bit id; the ONLY value that reaches a token
  material_hmac BLOB NOT NULL,     -- HMAC(instanceKey, config ‖ resolved secrets); never exposed
  rotated_at TEXT NOT NULL,
  last_health TEXT, last_health_at TEXT);

CREATE TABLE asset_cache (         -- metadata only; bytes live on disk
  hash TEXT PRIMARY KEY, connection_id TEXT NOT NULL, content_type TEXT NOT NULL,
  bytes INTEGER NOT NULL, created_at TEXT NOT NULL, last_access_at TEXT NOT NULL);
CREATE INDEX asset_cache_lru ON asset_cache(last_access_at);

CREATE TABLE rule_state (          -- persisted debounce and the `for:` timer queue
  rule_id TEXT PRIMARY KEY,
  rule_hash TEXT NOT NULL,         -- hash of the rule DEFINITION; editing an expression while keeping
                                   -- the id resets debounce instead of inheriting the old window
  since TEXT,                      -- when the predicate most recently became true on FRESH data
  deadline_at TEXT,                -- since + for: a real scheduled wake-up, rebuilt on start
  last_result TEXT NOT NULL,       -- true | false | unknown
  fired_at TEXT, resolved_at TEXT, suppressed_until TEXT);
CREATE INDEX rule_state_deadline ON rule_state(deadline_at);

CREATE TABLE notifications (       -- outbox, dedupe, retry
  id INTEGER PRIMARY KEY AUTOINCREMENT, created_at TEXT NOT NULL, channel TEXT NOT NULL,
  dedupe_key TEXT, payload BLOB NOT NULL, state TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT, last_error TEXT);
CREATE INDEX notifications_pending ON notifications(state, next_attempt_at);
CREATE UNIQUE INDEX notifications_dedupe ON notifications(dedupe_key)
  WHERE dedupe_key IS NOT NULL AND state IN ('pending','sending');
-- Uniqueness makes concurrent evaluations collapse to one row instead of two notifications.
-- Delivery remains AT-LEAST-ONCE by construction: a crash after the remote accepted but before
-- the success commit will retry, and no amount of local bookkeeping can fix that. Documented
-- rather than pretended away; receivers should tolerate a repeat.

CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, actor TEXT, ip TEXT,
  action TEXT NOT NULL, target TEXT, outcome TEXT NOT NULL, detail BLOB);
CREATE INDEX audit_ts ON audit_log(ts);
```

Thirteen tables. Notably absent by design: dashboards, cards, connections, and integration approvals
— all of those live in YAML (`veduta.yaml` and `veduta.lock.yaml`), because they are the reviewable,
version-controllable trust configuration, not runtime state.

---

## 11. Scheduling and state flow

```
snapshot change ─► scheduler rebuilds jobs (one per card, jittered start)
job fires ─► single-flight key = sha256(
                  integration id + version/module digest,
                  operation,
                  sorted slot→(connection id + connection revision) map,   ← all slots, not "the" connection
                  canonicalised params (sorted keys, normalised numbers))
          ─► budget/circuit-breaker check
          ─► Runtime.Invoke(grant, deadline)
          ─► widgets.Validate(document)
          ─► state.Put(card, document)   ── SSE ──► browsers
                                          └─► declared numeric signals → signal_history
                                          └─► rules.Evaluate → events → notify outbox
failure   ─► keep last good document, mark stale, exponential backoff with jitter,
             after N consecutive failures open the circuit: set circuitOpenUntil and
             nextRunAt to the half-open probe time, staying in stale/error (never
             `disabled`, which means "needs a human"), and emit one event (not N)
```

#### Execution identity: every invocation is fenced

Single-flight stops new work from joining a stale invocation. It does **not** stop a stale
invocation from *finishing* — after a connection rebind, a credential rotation, a revoked approval,
a removed card or an edited manifest — and overwriting current state with a result produced under
authority that no longer exists.

Every invocation therefore carries an immutable identity, computed when it is scheduled:

```go
type ExecutionIdentity struct {
    SnapshotGen      uint64            // config snapshot generation
    CardHash         string            // hash of the card definition (integration, operation, params, slots)
    ManifestDigest   string            // canonical manifest digest
    ApprovalRevision string            // lock record revision for this integration
    SlotRevisions    map[string]string // slot → connection revision
}
```

It is checked in two places: **before every broker call** (so a superseded invocation cannot even
reach an upstream) and **before committing state** (so a late result is discarded rather than
written). A config reload cancels the contexts of every superseded invocation; anything that
finishes anyway loses the commit fence and is dropped with one event, not silently.

A single connection id in the key is wrong the moment an operation binds two slots (a card
comparing two servers would collapse into one result); the connection *revision* is needed so a
credential rotation invalidates in-flight sharing rather than reusing a result fetched with the old
one.

Serving is **stale-while-revalidate**: `GET /cards` always answers instantly from SQLite; freshness
arrives over SSE.

**Viewport-aware refresh is out of scope for 0.1**, and so is idle pausing. "Refresh only visible
cards" needs a browser→server subscription protocol that does not exist yet. The previous draft
replaced it with "pause scheduling when no SSE client has been connected for ten minutes", which was
a **product contradiction**: rules and notifications are computed from scheduled observations, so
that design would have switched off "disk full" and "Jellyfin down" alerts precisely when nobody was
watching the dashboard — the exact moment monitoring matters.

**In 0.1 every configured card refreshes on its own schedule, always.** The cost is bounded and
smaller than it looks, because the expensive half is already viewer-driven by construction: the
asset proxy is *pull-based*, so images are fetched only when a browser requests a token. An idle
dashboard polls JSON and fetches no media at all.

A later optimisation can split the difference along lines that do not break monitoring:

| Card kind | Idle behaviour |
| --- | --- |
| referenced by a rule | always active — never pauses |
| presentation-only (no rule, no signals consumed) | may back off to a slow floor when idle |
| media | metadata refreshes; image fetching is already deferred to first view |

That classification is computable from the config (which cards do rules name?), so it needs no new
protocol either — but it is an optimisation, and 0.1 does not need it.

---

## 12. Rules and notifications

```yaml
rules:
  - id: disk-full
    when: 'signal("coding-server", "fs.root.percent") > 90'
    for: 5m
    severity: warning
    notify: [phone]
    resolve: true            # send a resolution notice when the condition clears
notifications:
  channels:
    phone:  { type: ntfy, url: https://ntfy.sh, topic: veduta-home, token: ${secret:NTFY_TOKEN} }
    hooks:  { type: webhook, url: http://n8n.lan/webhook/veduta }
```

One `expr` expression, one duration, one severity. Two functions are in scope and nothing else:
`signal(cardId, name)` reads a **declared** signal, and `state(cardId)` reads core-owned execution
state (`ok|stale|error|…`). Blocks are not reachable, by construction. Both card ids and signal
names are resolved against the manifests at config-load time, so a typo is a startup diagnostic
rather than an alert that silently never fires. The evaluator is a pure function of
`(previous signals, new signals, history, execution state)` — trivially unit-testable. `Notifier` is an interface
with two implementations in 0.1 (`ntfy`, `webhook`); shoutrrr can be added later without
touching the rules engine. Delivery goes through the `notifications` outbox with dedupe keys,
capped retries and an hourly ceiling per channel.

#### What a rule sees when data is stale or missing

Left undefined, this is the difference between a false "disk full" page at 3am from a value
observed an hour ago and a silently suppressed outage. So it is defined, three-valued:

- **`signal(card, name)` yields `unknown`** unless the card's *current* document carries that
  declared signal with a type-valid value. A retained last-good document is still **displayed**
  — that is what `stale` is for — but its signals are not fresh observations and do not answer
  `signal()`.
- **`unknown` propagates**: any comparison involving it is `unknown`, which is **not true**, so a
  `for:` window neither starts nor accumulates. A card that stops reporting cannot silently keep a
  disk-full timer running to completion on data nobody has re-observed.
- **Time only accumulates across consecutive fresh, true evaluations.** A stale or failed refresh
  in the middle of a `for: 5m` window resets the timer rather than pausing or ignoring it.
- **Outages are `state()`'s job, not `signal()`'s.** `state("card") == "error"` is exactly how you
  alert on "this stopped working", and it stays true while the circuit is open — which is why the
  breaker never surfaces as `disabled` (D24).
- **Resolution requires a fresh evaluation that is false**, never merely the disappearance of the
  signal. Otherwise every crash would auto-resolve its own alert.
- **Debounce state is persisted** in `rule_state`, so a restart mid-window resumes deterministically
  instead of silently restarting every timer.

#### `for:` needs its own timer, not a card update

Evaluating rules only when a card updates has a hole: `state("jellyfin") == "error" for 2m` becomes
true, the breaker then schedules its probe half an hour out, and **nothing evaluates the rule at the
two-minute deadline** — the alert never fires precisely in the outage it was written for.

So rules own a persisted timer queue:

```
predicate becomes true on fresh data  → record `since`, schedule a wake-up at `since + for`
wake-up fires                         → reload current card state, re-evaluate, fire if still true
predicate false / unknown / rule edited / card edited → cancel and clear `since`
process start                         → rebuild every pending deadline from `rule_state`
```

The deadline is a real scheduled wake-up, not a side effect of traffic.

| Situation | `signal()` | `for:` window | Effect |
| --- | --- | --- | --- |
| fresh document, value present and type-valid | the value | accumulates while true | normal |
| refresh failed, last-good document retained (`stale`) | `unknown` | resets | card still renders, dimmed; no alert fires or resolves on stale data |
| signal absent or null in a fresh document | `unknown` | resets | a integration that stopped emitting a signal cannot hold an alert open |
| value present but wrong type | `unknown` + one event | resets | reported, not coerced |
| circuit open beyond the debounce | `unknown` | resets | use `state()` to alert on this |
| stale `true` later recovers as fresh `false` | `false` | resets | resolution notice sent, if `resolve: true` |

**Explicit non-goal:** no chaining, no branching, no scripting, no "when X then call Y". If a
user needs that, the webhook channel hands off to n8n/Node-RED, which is the correct answer.

---

## 13. Frontend

- **Svelte 5 + Vite + TypeScript. No SvelteKit** — a static SPA build is embedded via `embed.FS`;
  SvelteKit's router/prerender/adapter layer buys nothing when Go serves everything.
- **No component library.** A ~15-token design system (`--v-bg`, `--v-surface`, `--v-text`,
  `--v-muted`, `--v-accent`, radii, spacing scale, two font sizes families), light and dark,
  themeable via CSS custom properties. Themes are CSS files, not JS.
- **The renderer is trusted and closed.** One Svelte component per block type, a registry keyed by
  `type`, unknown types render a labelled placeholder (forward compatibility). No `{@html}`, ever.
- **Layout:** CSS Grid with per-card column/row spans declared in YAML, `grid-auto-flow: dense`,
  container queries for card-internal breakpoints. No drag-and-drop in 0.1 (it conflicts with
  config-as-code and it is Homarr's turf).
- **Data:** one `GET /cards` on load, then one SSE connection; a small store maps `cardId → document`.
  Automatic reconnect with backoff; a visible "reconnecting" affordance.
- **Images:** `loading="lazy"`, `decoding="async"`, explicit aspect ratios from the block to prevent
  layout shift, a blurred placeholder while loading. This is a rich-media dashboard; image polish
  is a feature, not a detail.
- **Accessibility:** semantic landmarks, focus-visible styling, colour contrast checked in CI,
  status never conveyed by colour alone.

---

## 14. Decision log

| # | Decision | Status | Rationale |
| --- | --- | --- | --- |
| D1 | Go + Svelte + SQLite, single binary | **Frozen** | Deployment simplicity is a core promise |
| D2 | Widget Document is the only integration output | **Frozen** | The entire security and consistency story rests on it |
| D3 | Credentials live in the core; integrations use slots | **Frozen** | See C2/C3 |
| D4 | Asset refs are broker-minted signed tokens | **Frozen** | See C9 |
| D5 | YAML is the single source of truth | **Frozen for 0.1** | Avoids duelling stores; GUI is additive later |
| D6 | Extism on wazero for WASM | Adopted — [G1 sandbox decision](spikes/s1-wasm-sandbox.md) | Per-call instances; ARM performance acceptance remains outstanding |
| D7 | `expr-lang/expr` for mapping and rules | **Decided — S3** | Ergonomics decide it, not safety: D3's manifest DSL is templating-shaped (`map`/`filter`/`sortBy`/`take`/string and date helpers), which is what expr already looks like natively; CEL optimises for boolean policy predicates and would push a manifest-DSL redesign around CEL's macro model rather than a library swap. CEL's actual edge — an interpreter that accounts for its own comprehensions — is answered by D47 instead: expr is used as a parser/evaluator only, never as the sandbox |
| D8 | No SSH in 0.1 | **Decided** | See C4; host metrics come from Glances/Beszel over HTTP |
| D9 | SSE, one stream per tab | Decided | WebSockets only if bidirectional need appears |
| D10 | `modernc.org/sqlite` | Decided | Pure Go; ARM cross-compilation |
| D11 | AGPL-3.0-or-later core; Apache-2.0 `sdk/`, `schemas/`, `plugins/`; AGPL §7 plugin exception; DCO | **Decided** | Matches the niche (Glance, Immich are AGPL); Grafana's split keeps the plugin ecosystem permissive. See [LICENSING.md](../LICENSING.md) |
| D12 | Declarative is the primary extension mechanism in 0.1 | Decided | See C1 |
| D13 | Authority is `(slot, method, path)` routes, not `(slot)` | **Frozen** | Sandboxing protects Veduta; route grants protect the connected service. Applies identically to declarative and WASM |
| D14 | Permissions come from `veduta.lock.yaml`, digested over the manifest | **Frozen** | A manifest requests; only an administrator grants. Module-only pinning is circular |
| D15 | `signals` is the sole substrate for rules, history and alerts | **Frozen** | Blocks are presentation; labels and array positions are not a stable data model |
| D16 | Freshness, staleness, errors and provenance are core-owned (`CardState.execution`) | **Frozen** | An integration must not be able to assert its own freshness or hide its own failure |
| D17 | One expression grammar with explicit node kinds; no string templates, no name-based inference | **Frozen** | Predictable escaping, validatable templates, error messages that point at a YAML path |
| D18 | No viewport protocol **and no idle pausing** in 0.1: every card always refreshes | **Decided** | Pausing when unobserved would have disabled the alerts that exist for exactly that moment. Media cost is already viewer-driven, since the asset proxy is pull-based |
| D19 | Approval is a two-step, digest-bound transaction requiring re-authentication | **Frozen** | Closes the TOCTOU window and proves a person, not just a session, granted access to credentialed services |
| D20 | One shared route-canonicalisation routine; `*` only (no `**`); matching is three independent checks, never a computed glob intersection | **Frozen** | Encoding mismatches between check and use are the classic way route allowlists fail |
| D21 | Connection revision is an opaque random id, not a configuration hash | **Frozen** | Asset tokens are browser-visible; a hash over a credential is an offline oracle |
| D22 | `format` is never load-bearing; patterns are structural pre-filters; **semantics are enforced by real parsers** (`time.Parse`, `net/url`, semver) in the loader, and every pattern must compile under Go RE2 | **Frozen** | `format` is an annotation in both validators — but a pattern is not a parser either: `2026-99-99T99:99:99Z` is pattern-shaped and meaningless, and no readable regex validates ports or bracketed IPv6 |
| D23 | The declarative runtime carries an explicit resource budget (`inputMB`, `jsonDepth`, `jsonNodes`, `exprNodes`, `iterations`), counted over intermediate values, with an interruptible evaluator | **Frozen** | It runs in the host process; termination is not resource-boundedness, and declarative integrations are advertised as safe to install from strangers |
| D24 | An open circuit is `stale`/`error` + `circuitOpenUntil`, never `disabled` | **Frozen** | `disabled` means "needs a human"; a breaker heals itself. The two are operationally opposite |
| D25 | Route identity includes `queryKeys`, `contentType` and `maxBodyKB`, normalised | **Frozen** | Otherwise a lock entry can drop a constraint and the diff shows no change |
| D26 | Connection revision is random and public; change detection is a private instance-keyed HMAC over config ‖ resolved secrets | **Frozen** | Stable across restarts without exposing anything derived from a credential |
| D27 | Under forward auth, privileged operations are CLI-only by default; `admin-group` is opt-in and is authorisation, not presence | **Frozen** | A replayed group header cannot prove a human is at the keyboard |
| D28 | Untrusted manifests are bounded **before** parsing (bytes, YAML depth/nodes/aliases, duplicate keys, module size) and by aggregate expression/template ceilings at load | **Frozen** | `exprNodes` is per expression and bounds nothing on its own; schema validation runs after parsing, which is already too late |
| D29 | Iteration budget is charged by work: `n·log n` for `sortBy`, per element elsewhere | **Frozen** | Charging per call makes a sort the cheapest way to burn the most time |
| D30 | Limits are defined once (`plugin-manifest#/$defs/limits`, `$ref`d by the lock) and every limit has an effective value `min(core, manifest ?? default, approved ?? default)`, recorded in the lock | **Frozen** | An omitted limit means its default, not "anything"; manifest and approval must never speak about different fields |
| D31 | One `hostCalls` budget across every broker method, plus `cacheBytesKB` | **Frozen** | HTTP limits do not stop log, cache or asset-mint flooding |
| D32 | The manifest digest is RFC 8785 over a float-free subset, with set-like arrays normalised; golden fixtures verified by both Go and Python | **Frozen** | "Sorted-key JSON" is not a specification and the two languages disagree |
| D33 | Connection base URLs reject userinfo, query and fragment; webhook URLs are secret-capable values | **Frozen** | A credential in a URL bypasses the typed auth path and its redaction |
| D34 | `effectiveLimits` is mandatory and complete in every lock record | **Frozen** | A partial map leaves some limits governed by whatever the core default happens to be that week |
| D35 | Digest normalisation is **path-aware**: only `/spec/capabilities` and `/spec/operations/*/routes/*/queryKeys` | **Frozen** | `params` accepts arbitrary JSON Schema, so name-based normalisation let two semantically different manifests collide |
| D36 | `signal()` is three-valued: `unknown` unless the current document carries a type-valid declared value; `unknown` resets `for:` windows; debounce state is persisted | **Frozen** | Otherwise stale data either fires phantom alerts or suppresses real ones, depending on implementation accident |
| D37 | `nextRunAt >= circuitOpenUntil`, not equality | **Frozen** | Jitter and scheduling pressure legitimately delay a probe |
| D38 | Media types normalise via `mime.ParseMediaType`, retaining all parameters | **Frozen** | Naive `;` splitting mishandles quoted values; dropping parameters collapses distinct types |
| D39 | The three policies are carried separately in the `Grant` and each concrete request is authorised against all three; there is no combined route set | **Frozen** | A field named "effective routes" invites the glob-intersection bug the design exists to avoid |
| D40 | Every invocation carries an `ExecutionIdentity`, checked before each broker call and before each state commit | **Frozen** | Single-flight stops joining, not finishing; a late result must not overwrite state under revoked authority |
| D41 | `card_state` and `rule_state` carry a definition hash and reset when it changes; `card_state` stores the complete envelope | **Frozen** | Reusing an id for a different definition would otherwise resurrect the previous one's data |
| D42 | `for:` deadlines are persisted scheduled wake-ups, not a side effect of card traffic | **Frozen** | An open circuit produces no card updates, which is exactly when the alert is needed |
| D43 | SSE replay is best-effort with an explicit `reset` event; every `card` event is a complete, idempotent envelope | **Frozen** | A bounded ring cannot answer an arbitrary id after overflow or restart |
| D44 | Notification dedupe is a partial unique index; delivery is at-least-once | **Frozen** | Concurrency needs the database to arbitrate, and remote-accepted-then-crash is unfixable locally |
| D45 | Plugin cache is namespaced by manifest digest | **Frozen** | An upgraded plugin must not consume cache written by the version it replaced |
| D46 | `auth` is a required config block; without authentication the server binds loopback only | **Frozen** | The default listener is all-interfaces and the demo carries real credentials |
| D47 | expr is a parser/evaluator only; D3 owns the execution budget, and every scalable collection builtin (`map`/`filter`/`sortBy`/`all`/`any`/`one`/`none`/`find`/…) is disabled via `expr.DisableBuiltin` and replaced by a D3-owned, charged implementation under the same name | **Frozen** | expr's own resource accounting is not a sandbox (see §5's "expr is a parser and evaluator; D3 is the sandbox"); confirmed `expr.DisableBuiltin`/`DisableAllBuiltins` remove a name from the builtin table before name resolution, so an environment function of the same name resolves in its place — a supported mechanism, not an AST-rewrite hack |

## 15. Deliberately postponed

Multi-segment `**` route globs · request-body schema constraints on routes · idle/viewport-aware
scheduling · multi-user and RBAC · OIDC/passkeys · plugin marketplace, signing and OCI distribution · WASM
Component Model and WIT · remote agent · SSH and host actions · thumbnail generation · charts and
time-series · HTML/XPath/RSS extraction · config GUI · i18n · mobile app · clustering · Kubernetes
service discovery · Homepage feature parity.

Each of these has a named seam in the design above (`Runtime`, `Notifier`, `secrets.Provider`,
`config.Apply`, `Connection.Kind`, block-type registry) so that adding it later is additive.
**Nothing else gets an abstraction now** — in particular: no repository pattern over SQLite, no
event bus, no dependency-injection container, no plugin-versioned migration framework, no
multi-tenancy hooks, no generic "provider" interfaces with one implementation.
