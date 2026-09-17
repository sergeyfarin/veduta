# Veduta

> *veduta* (n.) — a highly detailed, wide-angle painting of a place.

Veduta is an experimental, self-hosted dashboard for homes and homelabs. It is being built to
present service and machine data—including photos, posters, and other rich content—through a
small, consistent interface.

Its central design idea is that integrations should not need unrestricted access to the host or
to service credentials. Integrations declare the operations they need; Veduta's core owns the
credentials and applies the approved capability and route limits.

> [!WARNING]
> **Veduta is pre-alpha and is not ready to install or operate as a real dashboard.** There is no
> published release yet. The first public alpha is being prepared, and configuration, storage,
> APIs, packaging, and the plugin ABI may change without a migration path. Please treat the
> repository as development code, not as a supported deployment.

![A development snapshot of Veduta's fixture dashboard in the Clean preset](web/tests/visual.spec.ts-snapshots/dashboard-light-chromium-linux.png)

*A test fixture, not a released product. The image is maintained by the visual-regression suite.*

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
- [Theming and visual-customisation decision](docs/decisions/0002-theming-and-visual-customisation.md)

## Development preview

There is no installation path for users yet. Contributors and evaluators who accept the pre-alpha
limitations can build from source and run the fixture dashboard by following the
[pre-release evaluation guide](docs/getting-started.md). The toolchain and common development tasks
are documented in [docs/dev-environment.md](docs/dev-environment.md).

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
