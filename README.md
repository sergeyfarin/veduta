# Veduta

> *veduta* (n.) — a highly detailed, wide-angle painting of a place.

A lightweight, self-hosted dashboard for your home and homelab. It shows **rich content**
(photos, posters, camera frames, charts — not just numbers), securely queries your
services and machines, performs a small set of **explicitly approved actions**, and
**notifies** you when something needs attention.

> **Actions are not executable in 0.1.** A card's declared action buttons render, and they are
> permanently disabled. Executing one needs authentication, an authorisation check and an audit
> trail on the core side, and until all three exist an enabled button that did nothing on click
> would be worse than a visibly disabled one. Everything else on this page is implemented.

Its distinguishing bet: **community integrations are sandboxed by design**. An integration
describes what it wants; the core decides whether it is allowed, and holds every credential.

![The Veduta dashboard in the Clean preset, light: media posters, host metrics, container state,
notes and disk usage, with cards in ok, stale, error, loading and disabled
states](web/tests/visual.spec.ts-snapshots/dashboard-light-chromium-linux.png)

*Clean, light — the default.*

![The same dashboard in the Veil preset, dark: translucent blurred cards over a dimmed moonlit
harbour painting](web/tests/visual.spec.ts-snapshots/dashboard-veil-dark-chromium-linux.png)

*Veil, dark — translucent cards over Vernet's moonlit Palermo, set with `dashboard.appearance: veil`.*

Neither image is a marketing shot taken by hand — both are visual-regression baselines the test
suite compares against on every push ([`web/tests/visual.spec.ts`](web/tests/visual.spec.ts)),
rendered from the checked-in showcase fixtures with a frozen clock. If the interface changes, CI
fails until the baselines are regenerated, so the screenshots cannot drift away from the product.

Light and dark follow your browser, and the preset follows the instance's `dashboard.appearance` —
but both are yours to override, from two controls in the header, remembered in your own browser and
stored nowhere on the server. Veil's backdrop is a public-domain veduta chosen for the colour
scheme, shipped dimmed and desaturated so it stays a backdrop; a mandatory scrim over it is what
lets the text contrast be proven in CI against the worst image anyone could supply, rather than
eyeballed (`TestVeilImageContrast`).

> **Experimental: the plugin ABI will change.** Veduta is pre-1.0. The WebAssembly guest ABI, the
> host functions in [`sdk/rust`](sdk/rust), and the integration manifest schema are **not** stable
> yet and will break between releases. Manifest digests and module hashes are pinned in
> `veduta.lock.yaml` precisely so an incompatible plugin fails closed rather than misbehaving —
> expect to rebuild and re-approve third-party plugins when you upgrade.

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
credential-safe ntfy/webhook outbox with bounded retries and flood controls. Phase K is done:
`veduta import homepage` parses Homepage YAML, maps services and bookmarks, creates disabled
connections and integrations for review, and atomically applies a comment-preserving config edit.
Config reloads activate a complete runtime generation before publishing it. Phase L is done: release
documentation, local-first icon handling, hardening, and L4's packaging — a distroless multi-arch
container image, checksummed release archives, a generated and UI-served `THIRD-PARTY-NOTICES.md`,
a changelog and a trademark policy. The live S2 Immich/Jellyfin validation cleared on 2026-09-09:
Immich is confirmed and E3's "six photos under 2 s cold" AC is met (~90 ms on a LAN Immich); the
Jellyfin plugin was rewritten to a single sorted `GET /Items` (a Jellyfin API key has no associated
user, so the earlier `/Users/Me` step could not work) and its Wasm module rebuilt and re-approved.
**0.1.0 is built and verified but not yet tagged** — the release workflow is triggered by pushing a
`v0.1.0` tag. See
[docs/02-implementation-plan.md](docs/02-implementation-plan.md) for the full
milestone table and what's marked **DONE**, and [docs/03-backlog.md](docs/03-backlog.md) for
open gaps found along the way. Rule transitions and their event/outbox effects now persist in one
transaction, clearing the final pre-L3 implementation blocker.

New users can follow the focused [getting-started guide](docs/getting-started.md). The generated
[configuration reference](docs/configuration.md), [integration authoring guide](docs/integration-authoring.md),
[security model](docs/security.md), and [migration guide](docs/migration.md) cover deployment and
extension beyond the showcase.

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

Password and forward authentication permit a non-loopback listener. `auth: none` remains
loopback-only unless the explicit override is passed.

This repository currently contains:

| Document | Contents |
| --- | --- |
| [docs/getting-started.md](docs/getting-started.md) | Build, first launch, authentication, secrets, and integration approval |
| [docs/configuration.md](docs/configuration.md) | Configuration field reference generated from the authoritative JSON Schema |
| [docs/integration-authoring.md](docs/integration-authoring.md) | Declarative and Rust/WASM integration authoring and validation |
| [docs/security.md](docs/security.md) | Operator security model and deployment checklist |
| [docs/migration.md](docs/migration.md) | Homepage import, review, rollback, and Veduta upgrade guidance |
| [docs/00-review-and-prior-art.md](docs/00-review-and-prior-art.md) | Competitive research, reuse decisions, and eight rounds of adversarial review with verdicts |
| [docs/01-architecture.md](docs/01-architecture.md) | Architecture, every contract (schemas, Go interfaces, REST, SQLite), threat model, decision log |
| [docs/02-implementation-plan.md](docs/02-implementation-plan.md) | Spikes, dependency-ordered milestones, and an executable issue backlog |
| [docs/03-backlog.md](docs/03-backlog.md) | Open gaps and decisions found along the way, not yet their own milestone |
| [docs/03-backlog-resolved.md](docs/03-backlog-resolved.md) | The closed half: what each gap was and where the fix landed |
| [docs/spikes/](docs/spikes/) | S2 (upstream API findings) and S4 (visual prototype, HTML+CSS) write-ups |
| [docs/dev-environment.md](docs/dev-environment.md) | Headless-VM notes: no system browser, how UI changes actually get verified |
| [hack/capture-upstream-fixtures.sh](hack/capture-upstream-fixtures.sh) | Captures real Immich/Jellyfin responses as reviewed fixtures |
| [hack/gen-third-party-notices.go](hack/gen-third-party-notices.go) | Generates THIRD-PARTY-NOTICES.md from the shipped bundle and the resolved crate graph |
| [Dockerfile](Dockerfile) | Distroless multi-arch image; cross-compiles rather than emulating the target |
| [compose.yaml](compose.yaml) | A complete deployment: Docker secret for the password hash, read-only root filesystem, socket proxy behind a profile |
| [.github/workflows/release.yml](.github/workflows/release.yml) | Tag-triggered release: archives, checksums, GHCR image, GitHub release (with a dry-run mode) |
| [CHANGELOG.md](CHANGELOG.md) | Release history, and the pre-1.0 plugin-ABI carve-out |
| [TRADEMARK.md](TRADEMARK.md) | Use of the name; permissive, and short |
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

[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) lists everything Veduta redistributes, generated
from what is actually shipped rather than from what the manifests declare; a running instance
serves it at `/api/v1/notices` and links it from the footer, next to the AGPL section 13 source
link. [TRADEMARK.md](TRADEMARK.md) covers use of the name — permissive, and short.

Release history is in [CHANGELOG.md](CHANGELOG.md).

## Non-goals

Time-series database, log aggregation, workflow builder, config-driven RBAC for large
organisations, arbitrary remote shell, browser automation, plugin marketplace.
