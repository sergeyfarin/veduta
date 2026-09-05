# Veduta

> *veduta* (n.) — a highly detailed, wide-angle painting of a place.

A lightweight, self-hosted dashboard for your home and homelab. It shows **rich content**
(photos, posters, camera frames, charts — not just numbers), securely queries your
services and machines, performs a small set of **explicitly approved actions**, and
**notifies** you when something needs attention.

Its distinguishing bet: **community integrations are sandboxed by design**. An integration
describes what it wants; the core decides whether it is allowed, and holds every credential.

## Status

Design complete and frozen; implementation starting at milestone A1. The contract suite in
`internal/contracts` is the CI gate from the first commit — `mise run contracts`.

Toolchain is pinned in [mise.toml](mise.toml): Go 1.27, Node 24, pnpm 11.

```bash
mise install && pnpm install
pnpm dev        # Go API on 127.0.0.1:8099 and Vite on :5173, /api proxied to the API
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
| [docs/00-review-and-prior-art.md](docs/00-review-and-prior-art.md) | Competitive research, what not to reinvent, and 14 challenges to the original design |
| [docs/01-architecture.md](docs/01-architecture.md) | Architecture, every contract (schemas, Go interfaces, REST, SQLite), threat model, decision log |
| [docs/02-implementation-plan.md](docs/02-implementation-plan.md) | Spikes, dependency-ordered milestones, and an executable issue backlog |
| [schemas/](schemas/) | JSON Schemas: Widget Document, card-state envelope, plugin manifest, integration lock, configuration |
| [examples/veduta.yaml](examples/veduta.yaml) | Target configuration file (validates against the config schema) |
| [plugins/immich/manifest.yaml](plugins/immich/manifest.yaml) | A complete declarative integration — no code, no rebuild |
| [examples/veduta.lock.yaml](examples/veduta.lock.yaml) | Approved capabilities and routes per integration |
| [testdata/widgets/](testdata/widgets/) | Golden Widget Document and card-state fixtures |
| [testdata/schema-cases.json](testdata/schema-cases.json) | 79-case adversarial schema corpus |
| [testdata/semantic-cases/](testdata/semantic-cases/) | 29 fixtures guarding the cross-document checks (a deleted check fails the build) |
| [testdata/canonical/](testdata/canonical/) | Golden RFC 8785 digests with collision invariants, reproduced by Go and Python |
| [internal/contracts/](internal/contracts/) | The contract suite: structural, RE2 portability, canonical digest and semantic layers |
| [internal/canonical/](internal/canonical/) | Strict decoder and RFC 8785 canonical manifest digest |
| [mise.toml](mise.toml) | Pinned toolchain and task runner |
| [web/](web/) | Svelte 5 + Vite + TypeScript SPA skeleton (pnpm workspace) |
| [internal/api/](internal/api/) | HTTP foundation: health, build identity, the loopback gate |
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
