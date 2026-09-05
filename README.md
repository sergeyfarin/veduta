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
SPA), B1 (design tokens, card shell, all four execution states), B2 (Widget Document, signals,
the CardState envelope, and Go/TypeScript types generated from one schema) and B3 (block
renderers for status/metrics/key-value/progress/list/text, with a Vitest harness) are done and
green in CI; spikes S2 (upstream API reality check) and S4 (visual prototype) are done. Next
up: B4 (media block renderers). See
[docs/02-implementation-plan.md](docs/02-implementation-plan.md) for the full milestone table and
what's marked **DONE**.

The contract suite in `internal/contracts` is the CI gate from the first commit —
`mise run contracts`.

Toolchain is pinned in [mise.toml](mise.toml): Go 1.27, Node 24, pnpm 11.

```bash
mise install && pnpm install
pnpm dev        # Go API on 127.0.0.1:8099 and Vite on :5173, /api proxied to the API
pnpm dev -- --host  # same, plus reachable from another device on your LAN (see web/README.md)
pnpm check      # go vet, go test (contract suite included), svelte-check
pnpm build      # SPA into web/build, then the binary
pnpm run update # every dependency, npm and Go, to latest
```

Frontend dependencies are pinned to exact versions — `.npmrc` sets `save-exact`, so `pnpm add`
and `pnpm update` write `1.2.3`, never `^1.2.3`. A dependency change should be a reviewable
commit, not something a fresh install decides.

Until authentication lands (milestone H1) the server refuses to bind anything but loopback: it
holds service credentials from Phase D onward.

This repository currently contains:

| Document | Contents |
| --- | --- |
| [docs/00-review-and-prior-art.md](docs/00-review-and-prior-art.md) | Competitive research, reuse decisions, and eight rounds of adversarial review with verdicts |
| [docs/01-architecture.md](docs/01-architecture.md) | Architecture, every contract (schemas, Go interfaces, REST, SQLite), threat model, decision log |
| [docs/02-implementation-plan.md](docs/02-implementation-plan.md) | Spikes, dependency-ordered milestones, and an executable issue backlog |
| [docs/spikes/](docs/spikes/) | S2 (upstream API findings) and S4 (visual prototype, HTML+CSS) write-ups |
| [docs/dev-environment.md](docs/dev-environment.md) | Headless-VM notes: no system browser, how UI changes actually get verified |
| [hack/capture-upstream-fixtures.sh](hack/capture-upstream-fixtures.sh) | Captures real Immich/Jellyfin responses as reviewed fixtures |
| [schemas/](schemas/) | JSON Schemas: Widget Document, card-state envelope, plugin manifest, integration lock, configuration |
| [examples/veduta.yaml](examples/veduta.yaml) | Target configuration file (validates against the config schema) |
| [plugins/](plugins/) | Three manifests: Immich and Glances (declarative), Jellyfin (WASM) — no code, no rebuild for the first two |
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
| [web/src/lib/blocks/](web/src/lib/blocks/) | Six Widget Document block renderers, a registry, and the unknown-type placeholder |
| [web/src/lib/format.ts](web/src/lib/format.ts) | The one place a `Scalar` value is turned into display text, per its `Format` hint |
| [web/src/lib/markdown.ts](web/src/lib/markdown.ts) | Restricted-subset markdown parser — never produces an HTML string, only an AST |
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
