# Veduta

> *veduta* (n.) — a highly detailed, wide-angle painting of a place.

Veduta is an experimental, self-hosted dashboard for homes and homelabs. It is being built to
present service and machine data—including photos, posters, and other rich content—through a
small, consistent interface.

Its central design idea is that integrations should not need unrestricted access to the host or
to service credentials. Integrations declare the operations they need; Veduta's core owns the
credentials and applies the approved capability and route limits.

> [!WARNING]
> **Veduta is early software, and `v0.1.0` is a pre-release.** Pre-1.0 carries no stability
> promise: configuration, storage, APIs, packaging, and the plugin ABI may change between releases
> without a migration path. Action controls render and are permanently disabled — executing them is
> not implemented. Treat it as something to evaluate, not as a dashboard to depend on.

![A development snapshot of Veduta's fixture dashboard in the Clean preset](web/tests/visual.spec.ts-snapshots/dashboard-light-chromium-linux.png)

*A test fixture, not a released product. The image is maintained by the visual-regression suite.*

## Why Veduta?

Existing self-hosted dashboards already do an excellent job of providing links, service widgets,
monitoring views, and visual customisation. They are also established products, while Veduta is
not. Veduta exists to explore a narrower extension contract: an integration returns typed content
from a fixed rendering vocabulary, cannot render arbitrary code in the dashboard, and reaches an
upstream service only through capabilities and routes approved by the operator.

That trade-off aims to make rich, third-party integrations easier to reason about, at the cost of
less freedom than arbitrary templates or frontend components. See
[Why Veduta, and how it differs](docs/why-veduta.md) for the rationale, a comparison with Homepage,
Glance, Homarr, and Dashy, and the important maturity caveat.

## What is here today

The repository contains a working development implementation of the dashboard, configuration
loader, integration runtimes, capability broker, authentication, persistence, notifications, and
packaging automation. These components have automated tests, but their presence does not imply
release readiness or interface stability.

Action blocks are currently display-only and remain disabled. Executing approved actions needs a
separate design and implementation; it is tracked in the
[backlog](docs/03-backlog.md#action-execution-is-not-implemented).

Detailed engineering progress and remaining work live outside this README:

- [Implementation plan and alpha preparation](docs/02-implementation-plan.md)
- [Open gaps and decisions](docs/03-backlog.md)
- [Architecture and security boundaries](docs/01-architecture.md)
- [Design research and product scope](docs/00-review-and-prior-art.md)
- [Product rationale and dashboard comparison](docs/why-veduta.md)
- [Theming and visual-customisation decision](docs/decisions/0002-theming-and-visual-customisation.md)
- [Why cards are not extensible with JS, CSS, Mermaid or Adaptive Cards](docs/decisions/0004-card-expressiveness-and-the-presentation-contract.md)

## Installing and evaluating

`v0.1.0` is published as a pre-release. `latest` is deliberately not published and will not be
before 1.0, so an install names the version it wants:

```sh
docker pull ghcr.io/sergeyfarin/veduta:0.1.0
```

Archives for `linux/amd64`, `linux/arm64`, `linux/arm/v7` and `darwin/arm64`, each carrying the
first-party integrations and the licences beside the binary, are attached to the
[release](https://github.com/sergeyfarin/veduta/releases/tag/v0.1.0) with a `SHA256SUMS` to check
them against.

The [setup and evaluation guide](docs/getting-started.md) covers configuration, the archive layout
and the fixture dashboard; the toolchain and common development tasks, for building from source
instead, are in [docs/dev-environment.md](docs/dev-environment.md).

Useful references:

| Document | Purpose |
| --- | --- |
| [Configuration reference](docs/configuration.md) | Generated reference for the current, unstable configuration schema |
| [Integrations](docs/integrations.md) | Current first-party integration coverage and requirements |
| [Integration authoring](docs/integration-authoring.md) | Experimental declarative and Rust/WASM extension interfaces |
| [Security model](docs/security.md) | Trust boundaries, deployment assumptions, and operator checklist |
| [Docker notes](docs/docker.md) | Development-stage container layout and security guidance |
| [Migration notes](docs/migration.md) | Current import and upgrade design; not a compatibility guarantee |
| [Contributing](CONTRIBUTING.md) | Contribution and verification requirements |

## License

The core is licensed under AGPL-3.0-or-later. `sdk/`, `schemas/`, and first-party integrations are
licensed under Apache-2.0, with a plugin exception that allows community integrations to use other
licences. See [LICENSING.md](LICENSING.md) and
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).
