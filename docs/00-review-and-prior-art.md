# 00 — Prior art, review, and challenges

Purpose of this document: make sure Veduta is not rebuilding something that already exists,
and stress-test the original design before any code is written.

---

## 1. The competitive map (2026)

| Project | Stack | Config source of truth | Extension model | Where it hurts |
| --- | --- | --- | --- | --- |
| **Homepage** (gethomepage) | Next.js/React | YAML files + Docker labels | Widgets are React+JS **compiled into the app**; a new integration is a PR to the repo | Mostly textual metrics; adding an integration means rebuilding the dashboard; `customapi` widget is limited to flat field extraction |
| **Glance** | Go, single binary, <25 MB RSS | YAML | `custom-api` widget: Go `text/template` over JSON with sub-requests, auth helpers; `extension` widget: **fetches raw HTML from an external HTTP server**, gated by `allow-potentially-dangerous-html: true` | Extensions are unsandboxed HTML injected into your dashboard; templates emit strings not typed data; media presentation is limited |
| **Homarr 1.0** | Next.js, tRPC, WebSockets, Redis, Postgres/SQLite | **GUI database** (drag & drop, "No YAML") | 50+ integrations built in; custom widgets defined through the management UI, including custom JSX | Heavy runtime; config is not reviewable/versionable as code; integrations still ship inside the app |
| **Dashy** | Vue | YAML (+ UI editor writing YAML) | 50+ widgets; custom widgets = write Vue components | Same rebuild-to-extend problem; project velocity has slowed |
| **Gatus** | Go | YAML | Condition DSL + 40+ alert providers | It is an uptime/status product, not a dashboard |
| **Beszel** | Go + PocketBase/SQLite, <10 MB agent | GUI | Fixed metric set | Machine metrics only |
| **Glances / Netdata** | Python / C | — | HTTP APIs | Data sources, not dashboards |
| **Instatic** (different domain, relevant precedent) | — | — | Each plugin runs in **QuickJS compiled to WASM**, no filesystem/env, network only to manifest-declared hosts approved at install | Proof the sandbox model is workable in a product, in a non-dashboard domain |

### What this means for positioning

Three of the four slots are taken:

- "Go + YAML + fast + beautiful" → **Glance already owns this.**
- "GUI, batteries included, RBAC" → **Homarr owns this.**
- "YAML + the largest widget library + Docker discovery" → **Homepage owns this.**

The unoccupied slot, and therefore Veduta's actual thesis, is:

> **Rich media as a first-class dashboard primitive, plus an integration ecosystem that is
> safe to install from strangers.**

Every project above extends via *trusted code you must audit yourself* (a Vue component, a
React widget, a raw HTML endpoint, a Go template). Nobody ships a sandboxed, capability-gated
integration runtime with a typed rendering contract. That is the differentiator worth building
and worth putting in the first paragraph of the README.

**Corollary — a positioning trap to avoid:** do not market Veduta as "lightweight Go dashboard
with YAML". Reviewers will benchmark it against Glance and it will lose on maturity for two
years. Market it as *"your homelab, with pictures — and integrations you don't have to trust."*

---

## 2. Do not reinvent: concrete reuse decisions

| Original plan proposed building | Reuse instead | Why | Risk / fallback |
| --- | --- | --- | --- |
| Custom WASM ABI (`http_request`, `cache_get`, …) + hand-written SDKs for Rust/Go/TS | **Extism** (`extism/go-sdk`, built on wazero, cgo-free) | Extism *is* the "small JSON in / JSON out ABI + host functions" design, already shipped, with PDKs for Rust, Go, JS (QuickJS), C#, Zig, and a manifest carrying `allowed_hosts`, `allowed_paths`, `timeout_ms`, `max_pages`, `max_http_response_bytes`, `max_var_bytes`. Saves weeks and gives multi-language plugin authoring for free. | Verify maintenance velocity in Spike 1. Fallback is the hand-rolled ABI originally proposed — cheap, because `PluginRuntime` isolates it. **Extism's built-in HTTP must be disabled** (see §3.2). |
| Bespoke plugin language SDKs | **Extism js-pdk (QuickJS-in-WASM)** for the community tier | Most homelabbers can write 20 lines of JS; almost none will write Rust. This is the single biggest lever on ecosystem adoption, and it stays fully sandboxed. | JS plugins are slower and bigger; fine for a 60-second refresh. |
| Notification adapters for ntfy/Gotify/webhook/SMTP/Discord/Telegram | Native ntfy + webhook (~150 LOC, zero deps); **containrrr/shoutrrr** later for the long tail | ntfy + webhook covers the 2026 homelab default. Shoutrrr's URL format (`discord://token@id`) embeds credentials in config strings, which fights the secret-reference model — so it goes behind the `Notifier` interface, not in front of it. | None. |
| SSH collectors for host metrics (CPU/mem/disk/systemd/docker) | **Glances / Beszel / Netdata HTTP endpoints** consumed as ordinary declarative integrations | Glances in a container exposes CPU, memory, disks, network, sensors, containers as JSON over HTTP. Zero credentials at risk, works today, and users likely already run one. | If the user has no agent, host monitoring waits for the Veduta agent (0.3). See challenge C4. |
| Docker client | ~200 LOC over the Engine API using `net/http` + unix socket | `docker/docker` drags in a very large dependency tree for four endpoints (`/containers/json`, `/containers/{id}/stats`, `/version`, `/events`). | None; the API surface used is stable and versioned. |
| Rules DSL (`greaterThan:`, `equals:`, …) and a separate mapping DSL (JSONPath + regex) | **`expr-lang/expr`** for both | One sandboxed, non-side-effecting, guaranteed-terminating expression language covers `disk.root.percent > 90` and `photos: .stats.photos + .stats.videos`. Two grammars become one. | CEL (`cel-go`) is the alternative; heavier, protobuf-flavoured. Decide in Spike 3. |
| Custom config validator | **JSON Schema** (`santhosh-tekuri/jsonschema/v6`) as the single source of truth | The same schema file validates the server config, powers `# yaml-language-server: $schema=` autocomplete in the user's editor, and later generates the config GUI. One artifact, three uses. | None. |
| SQLite driver with cgo | **`modernc.org/sqlite`** | Pure Go: cross-compilation for arm64/armv7 (Raspberry Pi is a primary target) with no C toolchain, and much faster CI. Slower than `mattn/go-sqlite3` on write-heavy loads, which this is not. | None at this scale. |
| Image resizing via libvips/ImageMagick | `golang.org/x/image/draw` (CatmullRom) | Keeps the single-binary, no-cgo promise. libvips is 4–8× faster but that only matters at web scale; here it is a handful of thumbnails per minute behind a disk cache. | If resize ever becomes hot, make it an optional build tag. |
| Icon library | **dashboard-icons** CDN + local disk cache + offline pack, plus **lucide** for UI chrome | Homepage/Homarr already normalised on dashboard-icons; users expect their icon names to work. | Local-first requires the cache — never a hard CDN dependency at render time. |
| HTTP router | `net/http` stdlib (Go 1.22+ method+pattern routing) | Chi/gin buy nothing here. | None. |
| Uptime/status-page features | Point at **Gatus** | Out of scope; a Gatus integration is more valuable than a reimplementation. | None. |

**Direct Go dependency budget for v0.1: ≤ 10.** Current intended list: `modernc.org/sqlite`,
`gopkg.in/yaml.v3`, `santhosh-tekuri/jsonschema/v6`, `expr-lang/expr`, `extism/go-sdk`
(+ `tetratelabs/wazero`), `fsnotify/fsnotify`, `golang.org/x/image`, `golang.org/x/crypto`.

---

## 3. Challenges to the original design

Ordered by how much they change the plan.

### C1 — v0.1 promises three extension mechanisms; it should promise one, and *prove* the second

**Original:** declarative + WASM + capability broker all shipped in v0.1, with a plugin
capability model as a headline feature.

**Challenge:** ~85% of the widget catalogue in Homepage and Glance is "GET one JSON document,
pull out four fields, render". That is declarative work. WASM earns its keep only where an
integration needs *logic*: multi-step authentication (Proxmox ticket + CSRF header, Jellyfin
`X-Emby-Authorization`), pagination, merging several endpoints, or computing derived state.
Shipping a plugin *ecosystem* in 0.1 also creates obligations — ABI stability, distribution,
signing, upgrade UX — before a single user exists.

**Recommendation:** v0.1 ships the **declarative runtime as the primary extension mechanism**,
plus **trusted built-in Go integrations**, plus a **working WASM runtime with exactly one real
plugin** (Jellyfin, chosen because it genuinely needs multi-request logic). Call the plugin
runtime *experimental* in 0.1; declare ABI stability at 0.3. The capability broker is built in
0.1 regardless, because all three runtimes go through it.

**Cost of being wrong:** low. The interface work is done either way.

### C2 — Extism's native HTTP would silently undo the credential design

**Challenge:** Extism ships `extism_http_request` with a manifest `allowed_hosts` allowlist. It
is tempting and it is the wrong door: it requires the plugin to hold the API key to
authenticate, which is precisely what §5 of the original design sets out to prevent.

**Recommendation:** configure every plugin with `allowed_hosts: []` (empty = **no** hosts
permitted; note that `null` means *all* hosts — an easy and catastrophic mistake) and expose
`veduta_http` as a host function that takes a **connection slot name**, not a URL. Add a
build-time conformance test asserting that a plugin calling `extism_http_request` fails.

### C3 — "Plugins request connections by name" should be "plugins declare slots; admins bind them"

**Challenge:** if a plugin names `connection: immich`, the plugin has assumed the deployment's
naming, and two Immich servers become awkward.

**Recommendation:** the manifest declares abstract **slots** (`{name: server, type: http}`);
the dashboard config binds a slot to a concrete connection per card. The broker resolves the
slot inside the `Grant`. A plugin literally cannot express "connection I was not given".

### C4 — SSH from the dashboard is the highest-risk, lowest-differentiation feature in the plan

**Challenge:** giving a network-exposed dashboard an SSH private key to your servers makes the
dashboard the most valuable target in the homelab, and it buys metrics that Glances, Beszel and
Netdata already expose over plain HTTP. `sudo systemctl restart jellyfin` via a named action is
still a remote-code-execution primitive, guarded only by the dashboard's session cookie.

**Recommendation:** **remove SSH from v0.1.** Deliver host monitoring by consuming Glances/Beszel
HTTP endpoints declaratively — this satisfies the stated use case (a coding-server overview card)
on day one with no credentials at risk. Reintroduce host control in 0.3 as either (a) SSH with
mandatory `known_hosts` pinning, a dedicated unprivileged user, a fixed command allowlist, and a
per-action confirmation, or (b) the outbound Veduta agent, which is the better long-term answer
anyway. If SSH is kept in 0.1 against this advice, it must never be reachable from a plugin —
not even through a named action — until actions have an audit trail and re-authentication.

**Cost of being wrong:** low; this defers work rather than foreclosing it.

### C5 — Docker socket access needs a documented safe default, not a warning

**Challenge:** `-v /var/run/docker.sock:/var/run/docker.sock` is root-equivalent on the host, and
it is what every dashboard's docs casually tell people to do.

**Recommendation:** ship the compose example with a **read-only socket proxy** (e.g.
`tecnativa/docker-socket-proxy` with only `CONTAINERS=1`, `INFO=1`) as the default path; direct
socket mounting is the documented-but-discouraged alternative. Container *actions* (start/stop/
restart) are off unless explicitly enabled per connection, and are never exposed to plugins.

### C6 — Config-as-code is right, but "no GUI" is the single biggest adoption risk

**Challenge:** Homarr's headline marketing is literally "No YAML, drag and drop configuration",
and it is winning users with it. Homepage's most common complaint is YAML friction.

**Recommendation:** keep YAML as the source of truth for 0.1 — but pay the small costs now that
keep a GUI cheap later: (1) JSON Schema for the config, published and referenced with
`# yaml-language-server: $schema=` so editors autocomplete; (2) parse through `yaml.v3` **Node**
so comments and formatting survive a programmatic write; (3) route every mutation through one
`ConfigStore.Apply(patch)` function from day one, even though 0.1 only calls it from the importer.
Then the 0.3 editor is a frontend project, not a rearchitecture. Explicitly **do not** add a
second source of truth in SQLite.

### C7 — Four extraction grammars in v0.1 is three too many

**Original:** JSONPath + CSS selectors + XPath + regex.

**Recommendation:** v0.1 = **JSON only**, with `expr` expressions for extraction and mapping.
0.2 adds HTML via `goquery` CSS selectors. XPath/RSS/regex only on demonstrated demand. Note
Glance's choice of Go `text/template` here is worth *not* copying: templates produce strings,
which nudges integrations toward emitting markup instead of data.

### C8 — The Widget Document needs limits and a stale/error model, or it will be the DoS surface

**Challenge:** the draft schema has no size bounds and no way to say "this is last-known-good
data from four minutes ago", which is the most common real state of a homelab dashboard.

**Recommendation:** every document carries `meta.generatedAt`, `meta.ttlSeconds`, and an optional
top-level `error`; the renderer visibly dims stale cards rather than blanking them. Hard caps
(≤12 blocks, ≤100 items/block, ≤64 KiB serialized) are enforced by schema validation *before*
the document reaches storage. Formatting is semantic (`format: bytes|percent|duration|relative-time`)
so the renderer, not the plugin, owns locale and units.

### C9 — Asset references must be minted by the broker, never constructed by the plugin

**Refinement of the original:** if a plugin can build the string `immich:asset:<id>:thumbnail`,
then a plugin can build `otherconnection:/etc/passwd`, and the asset endpoint becomes a
confused-deputy SSRF proxy. Make the ref an **HMAC-signed, TTL-bounded opaque token** minted by
a broker host call `assets.ref(slot, path, transform)`, which validates the slot against the
`Grant` at mint time. The plugin receives a token it cannot forge and cannot modify. Details in
[01-architecture.md §7](01-architecture.md).

### C10 — Redirects and DNS rebinding, not "SSRF", are the real network risks

The original correctly notes that LAN access is the point, so generic SSRF blocking is wrong.
The residual risks are narrower and must be handled explicitly: an upstream that 302-redirects
to a *different* connection's host, and DNS rebinding where a hostname resolves to a different
IP between validation and connection. **Recommendation:** redirects are not followed across
hosts by default; resolve the host once and dial the pinned IP for that request; deny non-http(s)
schemes; cap response size and time.

### C11 — "Auth optional" is a poor default; forward-auth support is not optional

**Challenge:** the target audience overwhelmingly runs Authelia, authentik, Tailscale or
Cloudflare Access in front of self-hosted apps. A dashboard that only offers its own login
forces them into double authentication.

**Recommendation:** v0.1 ships (a) single admin with argon2id + secure cookie session, **and**
(b) trusted-header/forward-auth mode with an explicit trusted-proxy allowlist. `auth: none` is
permitted but renders a persistent banner and is refused when any action or secret is configured.
OIDC and multi-user in 0.3+.

### C12 — Scheduling is where a homelab dashboard actually falls over

Not covered in the original. Ten cards pointing at one Jellyfin, each refreshing every 30 s,
plus every browser tab triggering a refresh, will hammer upstreams and the Raspberry Pi.

**Recommendation, in 0.1:** **single-flight** collapse on `(integration, operation, connection,
params-hash)`; per-connection concurrency limit and circuit breaker; jittered schedules;
stale-while-revalidate serving from SQLite; refresh only what is on screen plus a background
floor; global worker pool. This is cheap to build early and very expensive to retrofit.

### C13 — SSE is right, with two operational caveats

Keep SSE. But: reverse proxies buffer it (`X-Accel-Buffering: no` + document `proxy_buffering off`),
and browsers cap ~6 connections per origin over HTTP/1.1 — so **one** stream per tab, multiplexing
all card updates, with heartbeats and `Last-Event-ID` resume from a bounded in-memory ring.

### C14 — Visual regression testing is the right instinct, but it must be deterministic

Screenshot tests fail constantly on real data. **Recommendation:** the visual suite runs against
a frozen fixture dashboard with a pinned clock, checked-in local images (no network), and a
pinned browser version in CI, in one OS/font environment. Real-service tests are separate and
never gate CI.

---

## 4. What survives unchanged

These parts of the original analysis are sound and should be kept as written:

- Go + Svelte + SQLite + single binary with an embedded frontend.
- "Plugins describe what they want; the core decides whether they may."
- The Widget Document as the only thing an integration can produce — **no plugin JS, no plugin HTML.**
- Connections as a first-class core abstraction owning auth, TLS, timeouts and rate limits.
- YAML config as source of truth; SQLite for runtime state only.
- Homepage *importer*, not Homepage *emulation*, with an explicit warning report.
- A `PluginRuntime` interface so the runtime is replaceable.
- Deferring the WASM Component Model; architecting the contract so WIT is a later formalisation.
- Vertical slices over horizontal infrastructure; Immich-photos-on-screen as the first real milestone.
- Spiking the sandbox before anything else.

---

## 5. Revised v0.1 scope (diff against the original table)

| Feature | Original v0.1 | Revised | Why |
| --- | --- | --- | --- |
| Declarative integrations | Yes | **Yes — primary mechanism** | C1 |
| WASM runtime | Yes | **Yes, experimental, one plugin (Jellyfin)** | C1 |
| Custom WASM ABI + SDKs | Build | **Adopt Extism** | §2 |
| SSH monitoring | basic | **Removed → Glances/Beszel over HTTP** | C4 |
| Host actions | implied | **Deferred to 0.2/0.3** | C4 |
| Docker status | Yes | **Yes, via socket proxy by default** | C5 |
| Rules engine | custom operators | **`expr` expressions** | §2, C7 |
| Notifications | webhook + ntfy | Unchanged (native, no dep) | §2 |
| Extraction grammars | JSONPath+CSS+XPath+regex | **JSON + `expr` only** | C7 |
| Auth | single admin, optional off | **+ forward-auth; `none` heavily gated** | C11 |
| Scheduler | "scheduled refresh" | **+ single-flight, circuit breaker, SWR** | C12 |
| Asset refs | plugin-constructed strings | **broker-minted signed tokens** | C9 |
| Config GUI | later | later, but **Schema + Node round-trip + `Apply()` now** | C6 |

Net effect: v0.1 gets *smaller and safer* while the demo — Immich photos and Jellyfin posters
rendered beautifully, live, behind a credential-free image proxy — stays fully intact.

---

## 6. Settled decisions (was: open questions)

1. **Name — `Veduta`, confirmed with two known costs.** No trademark or software-product collision
   found; `npm`, PyPI and Docker Hub are free. But `veduta.dev` is **taken** by an active market
   research company, and the `veduta` GitHub organisation exists and is empty (squatted). Plan
   around it: publish under a personal account or an org like `veduta-dev`/`getveduta`, and pick a
   domain from `veduta.app` / `veduta.sh` / `getveduta.com`. Accept that bare "veduta" searches
   return Canaletto and a consultancy; the project will be found as "veduta dashboard".
   Before the first release, run a real USPTO/EUIPO/WIPO search in Nice class 9/42.
2. **License — AGPL-3.0-or-later for the core, Apache-2.0 for `sdk/`, `schemas/`, `plugins/`.**
   Matches the niche (Glance and Immich are AGPL-3.0; Homepage is GPL-3.0) and mirrors Grafana's
   split, which kept its plugin ecosystem permissive on purpose. See [LICENSING.md](../LICENSING.md)
   for the directory map, the draft AGPL §7 plugin exception, and the §13 network-source obligation
   the product itself has to satisfy. DCO, not a CLA — which forecloses a later relicense.
3. **SSH — removed from 0.1** (challenge C4 accepted). Host monitoring in 0.1 is
   Glances/Beszel over HTTP. Revisit at 0.3 as either pinned-`known_hosts` SSH with a fixed command
   allowlist, or the outbound agent — the agent being the better answer.
4. **Extism vs hand-rolled ABI** — decided by Spike 1 on evidence.
5. **`expr` vs `cel-go`** — decided by Spike 3.
6. **Plugin distribution** (OCI artifacts vs a signed index) — not needed until 0.3; do not design it now.


---

## 7. Review round 2 — findings, verdicts, and what changed

An external review of the first design drop. I reproduced every adversarial case against the
schemas before ruling on it; the corpus is now checked in at `testdata/schema-cases.json` and runs
in CI.

### Accepted in full

| # | Finding | Verdict | Change |
| --- | --- | --- | --- |
| R1 | An HTTP grant permitted any method on any path under the base URL — the sandbox protected Veduta but not the connected service | **Confirmed, critical.** A plugin bound to Immich could call `DELETE /api/albums/{id}` | Authority is now `(slot, method, path glob)`. Manifests declare `routes`; the effective grant is `manifest ∩ lock ∩ connection policy`; `ErrRouteDenied` is a first-class typed failure (D13, §6) |
| R2 | Manifest permissions were requested, not approved; module hash pinning is circular because the same manifest supplies the hash | **Confirmed, critical** | `veduta.lock.yaml` records the **manifest** digest, module digest, approved capabilities, routes and limits. Unlocked ⇒ `disabled`; changed ⇒ refused with a printed diff until re-approved; approval is audited (D14, §6) |
| R3 | Rules referenced `disk.root.percent` but documents contain only presentation blocks — no stable data model | **Confirmed, the strongest finding.** My own example config queried a path that had no defined source | `signals`: a declared, typed, unit-carrying map. Rules and history read only signals; `signal()`/`state()` are the only functions in scope (D15) |
| R4 | Runtime output and core-owned execution state were conflated; a plugin could assert its own freshness | **Confirmed** | `CardState` envelope: `document` (integration-owned) vs `execution` (core-owned). `generatedAt` is always core-stamped (D16) |
| R5 | The declarative format mixed literal strings, quoted expressions, an `items.expr` hatch and a `{{ }}` body template | **Confirmed** — `'"Photos"'` was indefensible | Four explicit node kinds, no name-based inference, structured request bodies (D17) |
| R6 | Eight invalid documents/configs were accepted by the draft schemas | **All eight confirmed by test** | Fixed: mode-specific auth requirements, type-specific connection auth, closed notification channels, runtime-exclusive manifest fields, anchored semver, per-block-type item schemas, `aspect` ≥1:1, execution fields no longer expressible in a document |
| R7 | The single-flight key used one "connection" and would collide for multi-slot operations | **Confirmed** | Key is now `sha256(integration+digest, operation, sorted slot→(connection + revision) map, canonical params)`; the revision makes credential rotation invalidate sharing (§11) |
| R8 | Asset tokens claimed to reject materially changed connections but carried no fingerprint | **Confirmed** | Payload gains a connection fingerprint, the minting integration id, and canonical **separate** path and query; serving re-checks the token against currently approved `use: asset` routes (§7) |
| R9 | Secret-string scanning cannot be a security boundary | **Confirmed** — I had listed it as a control | Reclassified as defence in depth, with an 8-character floor to avoid false positives; the boundary is that plugins never receive secrets (§8) |
| R10 | "Only connections may reveal secrets" conflicted with auth and notifications | **Confirmed, factual error** | Three narrow consumer adapters — `connections`, `notify`, `auth` — with the import-boundary test enforcing that nothing else references `Reveal` (§2) |
| R11 | The example registered Immich as `builtin` while the repo presents it as the flagship declarative integration | **Confirmed, factual error** | Example now uses `source: path:./plugins/immich`, and the manifest is the worked reference |
| R12 | "Refresh only visible cards" needs a subscription protocol that was never designed | **Confirmed** | Dropped for 0.1. Replaced with something needing no protocol at all: scheduling pauses after 10 minutes with no connected SSE client and resumes with an immediate refresh of expired cards (D18) |
| R13 | S1/WASM was placed first on the demo critical path while described as only "informing" the broker | **Confirmed, internal contradiction** | Split: S1a (0.5 d ARM feasibility smoke, week one, blocks nothing) and S1b (full conformance PoC, immediately before Phase G). The demo path no longer touches WASM |
| R14 | ~40 ideal days ⇒ 8–12 calendar weeks for one developer | **Fair.** My phase estimates summed to 39.5 and I never stated a calendar figure | Both numbers now stated in the plan |

### Accepted with a deliberate deviation

- **R1's "treat mutating methods as distinct capabilities."** Adopted route-level method control, but
  **not** a separate `http.write` capability: many read APIs use POST (Immich's own
  `/api/search/metadata` is the example in this repo), so a verb-based capability would either break
  real integrations or train users to grant writes reflexively. The route *is* the permission. What
  the verb changes is the **approval prompt** — non-GET routes are listed first, highlighted, and
  need an explicit keystroke.
- **R3's signal placement.** The review put `signals` as a sibling of `document` in `CardState`.
  Both are integration-owned, so signals live *inside* the document and the envelope splits strictly
  along the ownership line — one integration artifact, one core artifact. Equivalent, simpler.
  Added beyond the review: signals must be **declared in the manifest**, so a rule naming a
  nonexistent signal is a config-load error rather than an alert that silently never fires.
- **R4's TTL.** `execution.ttlSeconds` is core-owned, but the integration keeps an advisory
  `hints.ttlSeconds` — it genuinely knows its upstream's cadence — which the core clamps. Also added
  `document.notices[]` for *degraded success* (a sub-request failed), which is a different thing
  from `execution.error` (the run failed) and was previously unrepresentable.
- **R6's "empty metric items".** `metrics`, `key-value`, `progress` and `status` now require ≥1
  item. `list` and the media grids may be **empty on purpose** and carry an `empty` string — "No
  active streams" is a legitimate render, not a bug.

### Found while verifying, not in the review

- **A declarative manifest could declare no capabilities and still run an HTTP pipeline.**
  `capabilities` is now required and explicit, with the loader cross-checking that routes imply
  `http` and asset routes imply `assets`.
- **A pipeline could reference a slot the manifest never declared.** Now a load-time error
  (`x-loader-checks` in the manifest schema enumerates the semantic checks JSON Schema cannot express).
- **The asset capability could have been used to escape the route restriction.** If `assets.ref` and
  `http.request` drew from one route list, an integration granted `GET /api/assets/*/thumbnail` for
  images could mint refs to any path its data grant forbids. Routes now carry `use: data|asset` and
  the two broker methods match disjoint sets.
- **The declarative runtime would otherwise have bypassed the whole control.** Route checks apply to
  it identically, at load time rather than at runtime, or "plugins are untrusted" would have meant
  "WASM plugins are untrusted".

### Sequencing

The review's proposed order — freeze contracts, visual prototype, one end-to-end declarative Immich
slice, then persistence/scheduling/SSE, then WASM — is the order now in the plan, as Part 0 followed
by the revised critical path.


---

## 8. Review round 3 — findings, verdicts, and what changed

All seven findings reproduced and confirmed; nine of nine adversarial cases were indeed accepted by
the round-2 schemas. The corpus is now 65 cases and the validator runs three layers.

| # | Finding | Verdict | Change |
| --- | --- | --- | --- |
| T1 | Idle scheduling disables alerts | **Confirmed, critical, and self-inflicted** — I introduced the pause in round 2 to replace viewport refresh, and it would have switched off "disk full" and "Jellyfin down" precisely when nobody was watching | Pausing removed. Every configured card always refreshes. The future refinement (rule-referenced always active, presentation-only backs off) is recorded but not built (D18) |
| T2 | Approval needs a two-step, digest-bound protocol with re-authentication | **Confirmed** — a single `POST` approving "whatever the manifest says now" is a TOCTOU window | `GET /approval` → review; `POST /approve` with `expectedManifestSha256` **and the exact grants**; `409` on drift. Re-auth via a 5-minute sudo window in password mode; CLI-only under forward-auth and `auth: none`, since a trusted-header deployment cannot re-prompt (D19) |
| T3 | Method + path do not fully describe authority for body-driven APIs | **Confirmed and important to state honestly** | Documented as a limit of the model in §6 and in the manifest schema itself. Approval flags **body-bearing** routes rather than merely non-GET verbs. Routes gain optional `queryKeys`, `contentType`, `maxBodyKB`. Body-schema constraints deferred to a later `apiVersion`. Header handling switched from denylist to **allowlist** — and the same rule now covers connection-owned **query** parameters, which the finding did not mention and which matter when `auth.type: query` |
| T4 | `CardState` accepted impossible state combinations | **Confirmed, all four** | Conditional invariants per state, with a table in §4. `disabledReason` added and required, separating `unapproved`/`permissions-changed` (waiting on a human) from `circuit-open` (transient) |
| T5 | The suite checked structure, not declared semantics; `format` was an annotation | **Confirmed** | Three-layer validator. Crucially, **not** by installing a format checker: `format` is an annotation in the Go validator's default mode too, so anything that must be enforced is now a `pattern`. `oneOf` on request bodies, lock limits bounded identically to manifest limits, signal type/unit/history coherence enforced (D22) |
| T6 | Route canonicalisation is security-critical and underspecified; the pattern accepted encoded traversal | **Confirmed** | Normative algorithm in §6, one routine with five call sites, its own milestone (D1b) with a fuzz target. Matching is **three independent checks**, not a computed glob intersection — the finding is right that intersecting arbitrary globs is not well defined. `**` removed from v1 entirely (D20) |
| T7 | The connection fingerprint could be a password oracle | **Confirmed** | The token now carries an opaque random 128-bit **revision** persisted in `connection_state`, rotated on material config or resolved-secret change. No hash of any configuration value is browser-visible (D21) |

### Found while verifying, not in the review

**The route pattern could not compile in Go.** Both the manifest and lock schemas used
`^/(?!.*\.\.)…` — negative lookahead, which Go's RE2 does not support. `santhosh-tekuri/jsonschema`
uses stdlib `regexp`, so the Go validator would have refused the schema at boot, and the fix under
time pressure would very likely have been to delete the constraint. Verified with `regexp.Compile`
on Go 1.27: `invalid or unsupported Perl syntax: (?!`. Replaced with a lookahead-free pattern that
rejects `%`, backslashes, `..` and `**` by construction, and the validator now has a portability
layer that fails on any lookahead or backreference in any schema.

**`**` was ambiguous and unnecessary.** `/a/**/b` has no unambiguous reading, and no first-party
integration needs multi-segment matching. Removed from v1 rather than specified.

**Query parameters were the header problem's twin.** With `auth.type: query`, the connection owns a
query key; a plugin supplying the same key was previously unhandled.

### Deviation from the review

Only one: T5 suggests installing a JSON Schema format checker. I did not, because it would create a
false sense of enforcement — the Go validator treats `format` as an annotation by default, so the
two validators would disagree. Every constraint that matters is a `pattern` instead, and the
validator asserts those patterns are portable.

### Status

The four contracts frozen in round 2 stand, joined by four more (D19–D22). With T1 fixed and the
approval transaction and state invariants hardened, the core architecture is ready to freeze and the
vertical slice can begin.


---

## 9. Review round 4 — findings, verdicts, and what changed

All seven findings reproduced and confirmed, plus every "smaller correction". The validator grew a
real Go-backed portability layer and a parser-based semantic layer; the corpus is 77 cases and every
semantic check is mutation-proven.

| # | Finding | Verdict | Change |
| --- | --- | --- | --- |
| F1 | Declarative integrations have an unbounded host-process DoS path | **Confirmed, and the sharpest finding of this round.** WASM was bounded; the runtime advertised as the *primary* extension mechanism was not. "Terminating" is not "resource-bounded", and a synchronous evaluator ignores a context deadline unless instrumented | Explicit budget in §5 and in the manifest: `inputMB`, `jsonDepth`, `jsonNodes` (enforced during streaming decode), `exprNodes` (enforced **at load**, so a bomb never ships), one shared `iterations` counter across `each`/`map`/`filter`/`sortBy` and template expansion, and **intermediate** value accounting. Evaluator interruptibility is now an acceptance criterion of Spike 3, and adversarial benchmarks ship with D3 (D23) |
| F2 | `circuit-open` was modelled as `disabled` but `disabled` forbids `nextRunAt` | **Confirmed, a self-contradiction** | `disabled` now means exactly "will not run without human intervention". An open circuit is `stale` (with a last-good document) or `error` (without), carrying `circuitOpenUntil` plus a half-open `nextRunAt`. `circuitOpenUntil` is forbidden in `ok`/`pending`/`disabled` (D24) |
| F3 | Route identity ignored `queryKeys`, `contentType`, `maxBodyKB` | **Confirmed** — a manifest route restricted to `queryKeys: [safe]`, JSON and 4 KiB compared equal to a lock route with none of them | Canonical identity is the full tuple with normalisation rules written into the contract: sorted unique query keys, lowercased type/subtype with charset kept and other parameters dropped, effective `maxBodyKB`. An **omitted** constraint is wider than an explicit one and shows in the diff as a widening (D25) |
| F4 | Semantic validation had blind spots and the docstring overstated it | **Confirmed, every item** — signals were collected globally across operations, rules were never parsed at all, required slots and slot kinds were unchecked, duplicates undetected, the digest never recomputed | All six implemented, and each mutation-tested. The example lock now carries the **real** canonical digest of the Immich manifest, and the digest is defined over the parsed manifest re-serialised with sorted keys, so reformatting or comment edits do not invalidate an approval |
| F5 | Random revisions need an observable rotation trigger | **Confirmed** — "whenever the resolved secret changes" was stronger than the implementation could observe | Public revision stays random; change detection is a **private, instance-keyed HMAC** over config ‖ resolved secrets, stored in `connection_state` and never exposed. `file:` secrets are watched directly; `env:` secrets are documented as restart-only and caught by the startup comparison; delete-and-recreate always rotates (D26) |
| F6 | Regex is not a parser | **Confirmed** — `2026-99-99T99:99:99+99:99` passed, IPv6 homelab URLs were rejected, port 99999 accepted | Patterns are now explicitly structural pre-filters; semantics are enforced with real parsers (`time.Parse(RFC3339Nano)`, `net/url`, semver) in the loader and mirrored in the validator's layer 3. The URL pattern accepts bracketed IPv6. The RE2 layer now shells out to a checked-in **Go** program (`internal/contracts`), mutation-tested to fail on a lookahead (D22 revised) |
| F7 | Forward-auth approval was described but not configurable | **Confirmed** | `auth.forward.privilegedOperations: cli-only \| admin-group` with `adminGroups`, conditionally required. And the claim is corrected: a group header is **authorisation, not proof of current human presence**, which is why `cli-only` is the default (D27) |

### Smaller corrections, all applied

- The plan's D2 still described a `**` suffix matcher, contradicting D20 — removed; D2 now delegates all path handling to D1b.
- "Five call sites" corrected to **six** (manifest load, lock load, `http.request`, asset mint, asset serve, connection `allowedPaths`), in both documents.
- A non-optional global body ceiling: `spec.limits.requestBodyKB`, default 64 KiB; a route's `maxBodyKB` may only narrow it, and omitting both is never unlimited.
- `Content-Type` normalisation specified exactly.
- Asset-serving step 2 no longer says "recompute its fingerprint"; it compares the persisted revision.

### No deviations this round

Every recommendation was adopted as given, including the two I would have been tempted to soften:
the declarative budget is a hard contract rather than a "should", and the RE2 check is a real Go
compiler rather than a heuristic. The one addition beyond the findings: the manifest digest is
defined over the *canonical* (sorted-key, re-serialised) manifest rather than raw file bytes, so
approvals survive reformatting — otherwise every comment edit would demand re-approval and users
would learn to approve reflexively, which is the failure mode the lock exists to prevent.

### Status

Twenty-seven decisions, thirteen frozen. The four items the review named as blocking — declarative
resource budgeting, circuit-breaker semantics, full route-constraint identity, and operation-specific
signal and rule checks — are resolved and mutation-tested. The architecture is ready to freeze; the
next commit should be milestone A1.


---

## 10. Review round 5 — findings, verdicts, and what changed

All seven findings and every refinement reproduced and confirmed. The suite now runs four layers
over 84 structural cases, 5 canonical-digest fixtures verified independently by Go and Python, and
**19 checked-in negative semantic fixtures** — so a deleted check fails the build rather than
quietly reducing coverage.

| # | Finding | Verdict | Change |
| --- | --- | --- | --- |
| G1 | The declarative budget starts too late; `exprNodes` is per expression | **Confirmed.** The budget guarded execution while parsing and validation ran first, and hundreds of maximum-sized expressions were unbounded in aggregate | Pre-parse **core** limits (manifest bytes, YAML depth/nodes/aliases and alias expansion, duplicate-key rejection, module file size) enforced before schema validation — core constants, because an untrusted document cannot declare its own ceiling. Aggregate ceilings at load: expression nodes per operation and per manifest, template nodes and depth, literal-string bytes. `sortBy` charges `n·log n` (D28, D29) |
| G2 | Lock limits omitted every new field, and the widening check treated an absent manifest limit as "anything" | **Confirmed, both** — a lock recording `iterations` was rejected as an unknown property, and `req is not None` let a lock record the schema maximum where the manifest relied on a default | Limits are defined **once** in `plugin-manifest#/$defs/limits` and `$ref`d by the lock. Every comparison uses value-or-default, and the lock records `effectiveLimits` = `min(core, manifest ?? default, approved ?? default)`, so a core upgrade that changes a default forces re-approval instead of silently moving authority (D30) |
| G3 | The semantic validator still overstated its coverage | **Confirmed, every item.** Rules were regex-scanned on raw text, the real `disk-full` rule was skipped and then reported as resolved, the Jellyfin lock entry counted as a passing skip, pipeline requests were never checked against routes, and duplicate ids went undetected | Rule parsing strips string literals and **fails closed**: every `signal()`/`state()` occurrence must match a literal-argument form or it is an error, so a constructed reference cannot slip through unchecked. `plugins/glances` and `plugins/jellyfin` are now real contract fixtures, and a lock entry without a manifest is an **error** — there are no skips left. Pipeline requests must be covered by a declared route; asset nodes must have a `use: asset` route; duplicate integration, card, rule, slot, operation, signal and route ids are all detected. Nineteen negative fixtures run through the same code as the real examples |
| G4 | The canonical digest was Python's `sort_keys` output, not a portable specification | **Confirmed** | **RFC 8785 (JCS)**, implemented in `internal/canonical` and `scripts/canonjson/main.go`, verified byte-for-byte on golden fixtures with Unicode, `<>&`, tabs, reordered keys and reordered set-like arrays. Two explicit decisions: manifests are **float-free** (so ECMAScript number formatting never arises), and set-like arrays (`capabilities`, `queryKeys`) are sorted and de-duplicated, so reordering is not a change of authority. Defaults are **not** folded in — the digest covers what the author wrote, and `effectiveLimits` covers the rest (D32) |
| G5 | `circuitOpenUntil` without `nextRunAt` was accepted | **Confirmed** | `dependentRequired` added, and the contract states they are the same event seen from the breaker and from the scheduler, so they must be equal |
| G6 | `baseUrl: http://user:pass@host` bypassed the secret model | **Confirmed, and worse than it looks** — it also makes `auth: none` appear secret-free | Connection base URLs reject userinfo, query and fragment, structurally and in the loader. Webhook URLs are the deliberate exception you identified: tokens in the path are normal there, so a channel URL is a `secrets.Value` logged only as scheme://host plus channel id (D33) |
| G7 | Forward-auth `admin-group` accepted an empty policy | **Confirmed** | `minLength: 1` on header names with the HTTP token grammar, `minItems: 1` and `uniqueItems` on `adminGroups` |

### Refinements, all applied

- `http` is required only for `use: data` routes and `assets` only for `use: asset` — an asset-only
  integration is no longer forced to request a capability it never uses.
- A shared `hostCalls` budget (default 256) across `HTTP`, cache, asset minting, logging and events;
  `cacheBytesKB` bounds storage that `cacheEntries` never did.
- `Content-Type` normalisation is `mime.ParseMediaType` semantics: lowercased type/subtype with
  **all** parameters retained, sorted, keys lowercased — dropping them would collapse
  `application/vnd.api+json;profile=…` into its base type.
- The asset-proxy text no longer implies pass-through avoids parsing hostile data: enforcing
  dimension caps requires `image.DecodeConfig`, which parses attacker-controlled headers. Stated
  plainly and fuzzed in L3.
- The connection material HMAC uses **length-prefixed** canonical serialisation, so
  `{user: "ab", pass: "c"}` cannot collide with `{user: "a", pass: "bc"}`.

### No deviations

Every finding was adopted as given. The one judgement call within G4 — whether to normalise defaults
and set-like arrays before hashing — is decided explicitly rather than left implicit: set-like arrays
yes, defaults no, with `effectiveLimits` carrying what defaults resolved to.

### Status

Thirty-three decisions, twenty frozen. The four items named as blocking — pre-parser and aggregate
manifest budgets, complete and defaulted lock limits, parser-based semantic validation with no
skipped fixtures, and a genuinely portable canonical digest — are resolved, and each is now guarded
by a checked-in fixture rather than by a manual mutation run.


---

## 11. Review round 6 — findings, verdicts, and what changed

All nine reproduced and confirmed; six were counterexamples I could run directly. Two of them found
defects in the *examples*, not just the checkers, which is the sign the suite had been grading its
own homework.

| # | Finding | Verdict | Change |
| --- | --- | --- | --- |
| H1 | `effectiveLimits` optional and partially checked | **Confirmed** — the checked-in lock omitted it entirely and passed, so a changed core default would not have forced re-approval | Required by schema, with **every** key required; the semantic layer checks the exact key set and each value. Three fixtures: missing map, partial map (via a new `__replace__` overlay so the fixture cannot be merged back to completeness), wrong value |
| H2 | Canonicalisation normalised by property **name**, anywhere | **Confirmed, and the most serious of this round.** `params` accepts arbitrary JSON Schema, so `params.default.capabilities: [b,a]` and `[a,b]` hashed identically — the lock stopped pinning the whole manifest | Normalisation is **path-aware** in both languages: only `/spec/capabilities` and `/spec/operations/*/routes/*/queryKeys`. Fixtures now assert both directions — `must_match` for real set-like fields, `must_differ` for arbitrary user data that merely reuses the name (D35) |
| H3 | Pipeline coverage ignored most of the route grant | **Confirmed** — a route restricted to `queryKeys: [safe]` accepted a pipeline sending `evil=x` | Coverage now checks literal query keys, body presence and kind, and the declared media type. Anything not statically decidable (body size, dynamic values) is explicitly classified as runtime-only. **This immediately found a real defect**: Immich's POST route sent a JSON body while declaring no `contentType` — a plugin that would have failed at runtime |
| H4 | Alert behaviour under stale or missing signals undefined | **Confirmed, and a genuine product gap** — the difference between a phantom 3am page from hour-old data and a silently suppressed outage | `signal()` is three-valued: `unknown` unless the current document carries a type-valid declared value. `unknown` is not true, so `for:` windows reset rather than accumulate on stale data; outages are `state()`'s job; resolution requires a fresh false evaluation; debounce state persists in a new `rule_state` table. A six-row table in §12 pins every case, and J2 gains one test per row (D36) |
| H5 | Circuit timestamp equality was descriptive and probably wrong | **Confirmed on both counts** — your reasoning is better than mine: jitter and scheduling pressure legitimately delay a probe | `nextRunAt >= circuitOpenUntil`, with `circuitOpenUntil` documented as the earliest permissible probe. Schema enforces the dependency, the suite enforces the ordering (D37) |
| H6 | Three conflicting `Content-Type` definitions | **Confirmed** | One definition: `mime.ParseMediaType`/`FormatMediaType`, **all** parameters retained. The architecture's contradictory claim that `application/json` equals `application/json; charset=utf-8` is removed — they are different routes. Python no longer splits on `;`; it delegates to the Go helper, so `profile="a;b"` and escaped quotes round-trip correctly (D38) |
| H7 | The "real SemVer parser" claim was not true | **Confirmed** — `1.0.0-01` and `1.0.0-alpha..1` passed both layers | Schema uses the official SemVer grammar (RE2-safe), and validity is delegated to `gocheck semver`, a strict SemVer 2.0.0 implementation. Both agree on all eight probe cases |
| H8 | The cross-language digest test excluded the real YAML manifest on the Go side | **Confirmed** — it proved two JSON canonicalisers agreed, not that the production path did | Go now digests the three real `manifest.yaml` files through a strict YAML decoder that rejects duplicate mapping keys and floats and preserves YAML scalar typing, and those digests are compared against Python's |
| H9 | The rule scanner rejected call-like text inside string literals | **Confirmed** | Replaced with a lexer that returns non-string spans and only searches those. A **positive** fixture now asserts `state("x") == "signal(ghost, y)"` produces no issues, and the module says plainly that it is intentionally incomplete until Spike 3 supplies an AST |

### What this round changed about the suite itself

`internal/contracts` replaces `scripts/canonjson` and is now the Go half of the contract checks:
strict-YAML + RFC 8785 digest, `mime.ParseMediaType`, and SemVer 2.0.0. The Python semantic layer
**fails closed** without it rather than falling back to approximations — the failure mode that
produced H6 and H7 in the first place.

### No deviations

Every finding adopted, including H5 where your invariant is the correct one and mine was wrong.

### Status

Thirty-eight decisions, twenty-five frozen. The four freeze blockers — complete `effectiveLimits`,
path-aware digest normalisation, full pipeline-route coverage, and stale/unknown rule semantics —
are resolved, each guarded by checked-in fixtures.


---

## 12. Review round 7 — findings, verdicts, and what changed

The go/no-go pass, resolved before the first commit rather than after. All nine reproduced.

| # | Finding | Verdict | Change |
| --- | --- | --- | --- |
| K1 | The authoritative broker Go types still described the discarded model | **Confirmed, and the most dangerous kind of stale text** — D2 would have been implemented from the concrete types, which lacked `queryKeys`/`contentType`/`maxBodyKB`, still advertised `**`, and defined `Grant.Routes` as an intersection, while a later section correctly forbade intersecting globs | `Route` carries every constraint; `Grant` carries `ManifestRoutes`, `ApprovedRoutes` and `ConnectionPolicy` **separately** with a `Grant.Authorize` method that checks a concrete request against all three; `QueryKeys` distinguishes nil (unconstrained) from empty (no query at all); every `**` and "effective route set" reference is gone (D39). D2's tests cover query keys, media type, body ceiling, and each policy independently |
| K2 | Auth optional while the default listener was public | **Confirmed** — `version: 1` alone validated, `:8099` binds all interfaces, and Phase H sat after the container image and the real-Immich demo | `auth` is a **required** config block; `listen` defaults to `127.0.0.1:8099`; the server refuses a non-loopback bind until H1 has landed and a mode other than `none` is configured; **H1 moves ahead of E3/A4** (D46) |
| K3 | Snapshot changes need cancellation and commit fencing | **Confirmed** — single-flight stops joining, not finishing | `ExecutionIdentity` (snapshot generation, card hash, manifest digest, approval revision, slot→connection revisions) is checked before every broker call and before every state commit; reload cancels superseded contexts; late results are dropped with an event. F2 tests a blocked upstream completing after reload and after revocation (D40) |
| K4 | Persisted state lacked definition identity | **Confirmed** | `card_state` gains `card_hash`, manifest digest, approval revision, slot revisions, and stores the **complete validated envelope** as JSON so no field can go missing; `rule_state` gains `rule_hash`. Both reset when the definition changes, so a reused id cannot resurrect the previous definition's data (D41) |
| K5 | `for:` rules need their own wake-up | **Confirmed, and a real outage-shaped bug** — an open circuit produces no card updates, which is exactly when the alert matters | Persisted timer queue: schedule at `since + for`, re-evaluate at the deadline against current state, cancel on false/unknown/edit, rebuild pending deadlines from `rule_state` on start (D42) |
| K6 | SSE replay needed an overflow and restart protocol | **Confirmed**; "exactly once" was the wrong promise | Explicit `event: reset` when the id is unknown, too old, or from a previous process lifetime; the client refetches `/cards`; every `card` event is a complete envelope so application is idempotent replacement; the stream id carries a process-lifetime nonce (D43) |
| K7 | The Go decoder was not strict about document boundaries | **Confirmed** — `{"a":1}{"b":2}` and a two-document YAML file both hashed as their first document | Exactly one document, trailing content rejected, alias expansion bounded (depth 32, 20 000 nodes, 100 aliases) in the checker as in the loader. Integer domain pinned to **JSON safe integers ±(2^53−1)** in both languages: JCS interoperability is defined over IEEE 754 doubles, and Go's silent `int64` boundary disagreed with Python |
| K8 | `http-json` is an authority-bearing escape hatch | **Confirmed, and worth stating rather than defending** | Documented and fenced: `GET`/`POST` only, no custom headers beyond the broker allowlist, JSON body or none, no redirects, still bound by the connection's `allowedPaths`, same expression and resource budgets — and **called out by name during imported-connection review**, since an imported `customapi` widget becomes exactly this on imported credentials |
| K9 | Persistence needed atomic dedupe and an upgrade policy | **Confirmed** | Partial unique index on `dedupe_key` for pending/sending rows, with **at-least-once delivery documented** rather than pretended away — a crash between remote acceptance and the success commit is unfixable locally. Plugin cache is namespaced by manifest digest, so an upgraded plugin starts cold instead of consuming the previous version's entries (D44, D45) |

### Cleanup, all applied

The 4.9 MB `scripts/gocheck/gocheck` ELF binary is deleted and build artefacts are ignored; F1 says
thirteen tables; the threat-model row no longer says a breaker "disables" a card; D2b's UI wording is
a human-readable **applicability report** produced by the three independent checks rather than a
computed intersection.

### Status

Forty-six decisions, thirty-three frozen. The gates the review named are recorded in the plan: K1 and
K3 before D2, K2 before anything beyond loopback, and K4/K5/K6 before F1/F2/F4/J2. Implementation
starts at A1.


---

## 13. Round 8 — the contract suite moves into Go

Not a review finding; a question worth recording, because the answer changed the repository
layout. Asked why the project needed Python at all, the honest answer was: it didn't any more.

**Why it existed.** The contract suite was written in Python during design review, when the
repository held nothing but documents and there was no Go module. Python plus `jsonschema` was the
fastest way to make the review's claims falsifiable rather than merely asserted. That justification
expired the moment `go.mod` landed.

**Why it had become actively wrong.** The stated purpose of the semantic layer is to mirror what the
Go loader will do. A Python mirror drifts — findings H6 (media-type normalisation) and H7 (SemVer
validity) *were* that drift, and the fix was to shell out to a Go helper. The suite had already
conceded that Python was the wrong host and was calling Go for everything that mattered, while
costing a third language, a third toolchain in CI, and 798 lines of shim.

**What was kept.** The two independent RFC 8785 implementations did real work: a single
implementation cannot detect its own ambiguity, and cross-checking Go against Python is what proved
the canonicalisation spec unambiguous. That value is banked permanently in
`testdata/canonical/expected-digests.json` — the golden digests were produced by both, and they
still pin the specification now that only one implementation remains.

**What the port is.** `scripts/*.py`, `scripts/gocheck` and `scripts/re2check` are gone. The four
layers are now Go tests in `internal/contracts`, and the checks themselves are ordinary code in
`internal/contracts/semantic.go` and `internal/canonical` — which is where milestones C1, D2b and D3
needed them anyway, so this was milestone work brought forward rather than rework. Every data file
is unchanged: the same 79-case corpus, the same 29 semantic fixtures, the same golden digests.

**Two bugs the port surfaced**, both in the Python original:

- `effectiveLimit` initialised its accumulator to the *default* and then took the minimum against
  everything, so an explicit manifest limit higher than the default was silently floored. Python and
  Go disagreed on `immich.timeoutMs` (5000 versus 3000), which is how it was caught. The formula is
  now written once and stated in a comment: `min(core maximum, manifest value-or-default, approved
  value-or-default)`.
- An invalid `contentType` was reported only when a pipeline step happened to exercise the route.
  Routes are now validated where they are declared.

The meta-tests still bite: deleting the `effectiveLimits` checks, disabling the query-key coverage
check, or widening the digest normalisation path list each turn the build red.
