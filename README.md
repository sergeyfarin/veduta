# Veduta

> *veduta* (n.) — a highly detailed, wide-angle painting of a place.

A lightweight, self-hosted dashboard for your home and homelab. It shows **rich content**
(photos, posters, camera frames, charts — not just numbers), securely queries your
services and machines, performs a small set of **explicitly approved actions**, and
**notifies** you when something needs attention.

Its distinguishing bet: **community integrations are sandboxed by design**. An integration
describes what it wants; the core decides whether it is allowed, and holds every credential.

## Status

Design complete and frozen. Implementation under way: Phase A (repository bootstrap, embedded
SPA), B1–B5 (design tokens and card shell; the Widget Document / CardState envelope with
Go/TypeScript types generated from one schema; renderers for all nine block types - status,
metrics, key-value, progress, list, text/markdown, image, image-grid, poster-grid, table,
actions; and a fixture dashboard with a Playwright visual regression baseline), C1 (the config
loader: `veduta.yaml` + `conf.d/*.yaml` merged, schema-validated and semantically checked, every
error carrying `file:line:col`), C2 (secrets: `env:`/`file:` providers, a redacting `Value`
type, a log scrubber active by default), C3 (atomic live reload with status), D1 (the
connection registry and HTTP client: auth injection, an IP-pinning dialer against DNS rebinding,
redirect/size/rate limits), D1b (the one route-canonicalisation and glob-matching routine every
authority check shares, fuzz-tested), D2 (the capability broker: three independent route
policies, never a computed intersection; header/query allowlisting; shared per-invocation
budgets), and D2b (the integration lock and approval flow: canonical manifest digest, permission
diff, `veduta integration list|diff|approve`, and the matching
`GET/POST /api/v1/integrations...` endpoints - a sudo-window gate and audit trail are deferred to
H2), D3 (the bounded declarative runtime), D4 (the generic
HTTP/JSON card - a card's `view:` block synthesises a manifest reused wholesale through D3, not a
second execution engine), and D5 (connection health and admin endpoints: `GET /api/v1/connections`,
`POST /api/v1/connections/{id}/test`, DNS/TCP/TLS/auth/HTTP-status classified separately) are done
and green in CI, closing out Phase D; spikes S2 (upstream API reality check), S3 (expr vs cel
bake-off), S4 (visual prototype), Phases E–G, and Phase H (password and forward authentication,
privileged-operation gating, and persistent audit) and Phase I (Docker and host metrics) are done.
Phase J is done: declared signal history and events feed durable, manifest-checked rules, and a
credential-safe ntfy/webhook outbox with bounded retries and flood controls. Phase K is underway:
K1 parses Homepage YAML and Docker labels into a format-neutral import model. See
[docs/02-implementation-plan.md](docs/02-implementation-plan.md) for the full
milestone table and what's marked **DONE**, and [docs/03-backlog.md](docs/03-backlog.md) for
open gaps found along the way. K1 is unblocked; coherent config/runtime publication is required
before K2, and atomic rule-transition persistence is required before release hardening.

The contract suite in `internal/contracts` is the CI gate from the first commit —
`mise run contracts`.

Toolchain is pinned in [mise.toml](mise.toml): Go 1.27, Node 24, pnpm 11.

```bash
mise install && pnpm install
pnpm dev        # showcase API on 127.0.0.1:8099 and Vite on :5173, /api proxied to the API
pnpm run dev:config # real config + live reload (requires the example's secrets in the environment)
pnpm dev -- --host  # same, plus reachable from another device on your LAN (see web/README.md)
pnpm check      # go vet, go test (contract suite included), svelte-check
pnpm build      # SPA into web/build, then the binary
pnpm run update # every dependency, npm and Go, to latest

./veduta serve --fixtures         # serve the checked-in showcase dashboard, no config needed
pnpm --filter veduta-web test:e2e # visual regression baseline against it (Playwright, pinned Chromium)

./veduta --check-config --config examples/veduta.yaml  # validate a config, file:line:col on error
```

Frontend dependencies are pinned to exact versions — `.npmrc` sets `save-exact`, so `pnpm add`
and `pnpm update` write `1.2.3`, never `^1.2.3`. A dependency change should be a reviewable
commit, not something a fresh install decides.

Password mode now permits a non-loopback listener with Argon2id-backed sessions. `auth: none` and
the not-yet-implemented forward mode remain loopback-only unless the explicit override is passed.

This repository currently contains:

| Document | Contents |
| --- | --- |
| [docs/00-review-and-prior-art.md](docs/00-review-and-prior-art.md) | Competitive research, reuse decisions, and eight rounds of adversarial review with verdicts |
| [docs/01-architecture.md](docs/01-architecture.md) | Architecture, every contract (schemas, Go interfaces, REST, SQLite), threat model, decision log |
| [docs/02-implementation-plan.md](docs/02-implementation-plan.md) | Spikes, dependency-ordered milestones, and an executable issue backlog |
| [docs/03-backlog.md](docs/03-backlog.md) | Gaps, issues and improvement opportunities found along the way, not yet their own milestone |
| [docs/spikes/](docs/spikes/) | S2 (upstream API findings) and S4 (visual prototype, HTML+CSS) write-ups |
| [docs/dev-environment.md](docs/dev-environment.md) | Headless-VM notes: no system browser, how UI changes actually get verified |
| [hack/capture-upstream-fixtures.sh](hack/capture-upstream-fixtures.sh) | Captures real Immich/Jellyfin responses as reviewed fixtures |
| [schemas/](schemas/) | JSON Schemas: Widget Document, card-state envelope, plugin manifest, integration lock, configuration |
| [examples/veduta.yaml](examples/veduta.yaml) | Target configuration file (validates against the config schema) |
| [plugins/](plugins/) | Four integrations: Immich, Glances and Beszel (declarative), plus Jellyfin (WASM) |
| [examples/veduta.lock.yaml](examples/veduta.lock.yaml) | Approved capabilities and routes per integration |
| [testdata/widgets/](testdata/widgets/) | Golden Widget Document and card-state fixtures |
| [testdata/schema-cases.json](testdata/schema-cases.json) | 79-case adversarial schema corpus |
| [testdata/semantic-cases/](testdata/semantic-cases/) | 29 fixtures guarding the cross-document checks (a deleted check fails the build) |
| [testdata/canonical/](testdata/canonical/) | Golden RFC 8785 digests with collision invariants (checked by Go; the suite is Go-only, see below) |
| [internal/contracts/](internal/contracts/) | The contract suite: structural, RE2 portability, canonical digest and semantic layers |
| [internal/canonical/](internal/canonical/) | Strict decoder and RFC 8785 canonical manifest digest |
| [mise.toml](mise.toml) | Pinned toolchain and task runner |
| [web/](web/) | Svelte 5 + Vite + TypeScript SPA skeleton (pnpm workspace) |
| [internal/api/](internal/api/) | HTTP foundation: health, build identity, embedded SPA, the loopback gate |
| [internal/widgets/](internal/widgets/) | The Widget Document: typed Go structs, discriminated block union, `Validate` |
| [internal/state/](internal/state/) | The CardState envelope; five constructors are the only way to build one |
| [web/scripts/gen-types.mjs](web/scripts/gen-types.mjs) | Generates `web/src/lib/types/*.ts` from the schemas — `pnpm gen-types` |
| [web/src/lib/blocks/](web/src/lib/blocks/) | All nine Widget Document block renderers, a registry, and the unknown-type placeholder |
| [web/src/lib/format.ts](web/src/lib/format.ts) | The one place a `Scalar` value is turned into display text, per its `Format` hint |
| [web/src/lib/markdown.ts](web/src/lib/markdown.ts) | Restricted-subset markdown parser — never produces an HTML string, only an AST |
| [web/src/lib/assets.ts](web/src/lib/assets.ts) | The one place an `Image.ref` becomes a fetchable URL |
| [internal/fixtures/](internal/fixtures/) | The checked-in showcase dashboard `--fixtures` serves: layout, every CardState, local images |
| [web/playwright.config.ts](web/playwright.config.ts) / [web/tests/visual.spec.ts](web/tests/visual.spec.ts) | Visual regression baseline against `--fixtures`: frozen clock, one pinned browser |
| [internal/config/](internal/config/) | `veduta.yaml` + `conf.d/*.yaml` loader: merge, schema and semantic validation, `file:line:col` on every error |
| [internal/secrets/](internal/secrets/) | `${secret:NAME}` resolution: `env:`/`file:` providers, a redacting `Value` type, the log scrubber |
| [internal/connections/](internal/connections/) | The credential boundary: per-connection HTTP client, auth injection, IP-pinning dialer, rate/redirect/size limits, health classified by stage (DNS/TCP/TLS/auth/HTTP-status) with credential scrubbing on the error path |
| [internal/connections/routepath/](internal/connections/routepath/) | The one route-canonicalisation and glob-matching routine every authority check shares — fuzz-tested |
| [internal/capabilities/](internal/capabilities/) | The capability broker: three independent route policies, header/query allowlisting, per-invocation budgets, typed denials |
| [internal/integrations/](internal/integrations/) | The lock file: canonical manifest digest, permission diff, two-step digest-bound approval — `veduta integration list\|diff\|approve` and `GET/POST /api/v1/integrations...` both call into it; also the frozen `Runtime`/`Instance` contract every runtime implements |
| [docs/docker.md](docs/docker.md) | Read-only Docker socket-proxy deployment and connection guidance |
| [internal/integrations/manifestload/](internal/integrations/manifestload/) | Parses and statically validates a declarative manifest: pre-parse byte/depth/node/alias limits, schema validation, the four-node template grammar, load-time slot/capability/route/signal checks |
| [internal/integrations/declarative/](internal/integrations/declarative/) | The declarative runtime: an `expr` environment where native collection builtins keep their syntax but are call-boundary charged against a shared budget, the pipeline executor, output-document assembly through `widgets.Validate` |
| [internal/integrations/httpjson/](internal/integrations/httpjson/) | The generic HTTP/JSON card (`integration: http-json`): synthesises a manifest and a self-approving lock entry from one card's `params`/`view:`, then runs it through the unmodified declarative runtime |
| [CONTRIBUTING.md](CONTRIBUTING.md) | DCO sign-off, the licence split, what a change needs |
| [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md) | Every dependency and its licence, enforced by a test |
| [LICENSING.md](LICENSING.md) | Multi-license layout, plugin exception, AGPL §13 obligations |

## Product principles

- **Beautiful by default.** No hours of CSS to look good.
- **Rich content is first-class.** Images, posters and frames are not hacks.
- **Config as code.** Everything important is expressible in a readable file.
- **Plugins are untrusted.** Integrations get capabilities, never machine privileges.
- **Simple integrations should be simple.** Reading a number from an API must not require code.
- **One application, one binary.**
- **Observe, and perform small approved actions.** Not Grafana, not Ansible, not Home Assistant.
- **Local-first.** No cloud dependency, ever.
- **Migration should be easy.** Import what you already have from Homepage.

## License

AGPL-3.0-or-later for the core; Apache-2.0 for `sdk/`, `schemas/` and first-party integrations,
with an explicit plugin exception so community integrations stay under whatever license their
authors choose. See [LICENSING.md](LICENSING.md).

## Non-goals

Time-series database, log aggregation, workflow builder, config-driven RBAC for large
organisations, arbitrary remote shell, browser automation, plugin marketplace.
