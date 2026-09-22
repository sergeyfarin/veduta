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

## Quick start

`latest` is deliberately not published before 1.0, so a deployment names the version it wants.
Save this as `compose.yaml`:

```yaml
name: veduta
services:
  veduta-init:
    image: ghcr.io/sergeyfarin/veduta:0.1.0
    user: "${VEDUTA_UID:-1000}:${VEDUTA_GID:-1000}"
    command: ["init"]
    volumes:
      - ./config:/config
    restart: "no"
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]

  veduta:
    image: ghcr.io/sergeyfarin/veduta:0.1.0
    user: "${VEDUTA_UID:-1000}:${VEDUTA_GID:-1000}"
    restart: unless-stopped
    depends_on:
      veduta-init:
        condition: service_completed_successfully
    ports:
      - "127.0.0.1:8099:8099"
    volumes:
      - ./config:/config
      - ./data:/data
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
```

Then:

```sh
mkdir -p config data
printf 'VEDUTA_UID=%s\nVEDUTA_GID=%s\n' "$(id -u)" "$(id -g)" > .env
docker compose up -d && docker compose logs veduta-init
```

```
  Veduta is configured. Sign in as "admin" with this password:

      7fpmq853u4fe5a6gqq4d

  It is shown once and stored only as a hash. Save it now.
```

Veduta is on <http://127.0.0.1:8099>. That is the whole installation. Nothing in it runs as root,
and no `chown` is needed: the `.env` file makes the container run as you, so the bind-mounted
`config/` and `data/` stay yours to read, edit and back up.

The `veduta-init` service writes `config/veduta.yaml` on the first run and does nothing on every
run after it, so your edits are never overwritten. To choose the password instead of having one
generated, set `VEDUTA_ADMIN_PASSWORD` on that service — it is hashed on the first run and the
plaintext is never written to disk.

The dashboard is empty until you add connections and cards. Veduta reloads `config/veduta.yaml`
when it changes, so edit it in place; [`examples/veduta.yaml`](examples/veduta.yaml) is a worked
configuration to borrow from, and the [setup guide](docs/getting-started.md) walks through
connecting a real service.

Two things the quick start decided for you, both covered in the [Docker notes](docs/docker.md):

- **The port is published to loopback only.** Veduta speaks plain HTTP and its session cookie
  carries no `Secure` attribute, so reaching it from elsewhere on your network means putting a
  TLS-terminating reverse proxy in front. Changing it to `8099:8099` for a throwaway test on a
  trusted LAN is a deliberate downgrade, not a default.
- **The container runs as your uid rather than the image's.** Docker passes bind-mount ownership
  through untranslated, so a container writing to `./config` as some other uid cannot, and the
  usual fixes are a `chown` you have to remember or a privileged container that does it for you.
  Matching the uid avoids both. If you cannot choose the uid on your host,
  [docs/docker.md](docs/docker.md#when-you-cannot-choose-the-uid) has the alternatives.

To see the dashboard before configuring anything, the checked-in fixture showcase needs no
configuration file at all. It holds no credentials, so the override below is safe here and
nowhere else — the container has to bind `0.0.0.0` to be reachable from the host, and that is
exactly the bind Veduta refuses without authentication:

```sh
docker run --rm -p 127.0.0.1:8099:8099 ghcr.io/sergeyfarin/veduta:0.1.0 \
  serve --fixtures --listen 0.0.0.0:8099 --i-know-what-im-doing
```

### Other ways to install

[`compose.yaml`](compose.yaml) in this repository is the quick start's file plus a read-only Docker
socket proxy, behind a profile, for the Docker card.

Archives for `linux/amd64`, `linux/arm64`, `linux/arm/v7` and `darwin/arm64`, each carrying the
first-party integrations and the licences beside the binary, are attached to the
[release](https://github.com/sergeyfarin/veduta/releases/tag/v0.1.0) with a `SHA256SUMS` to check
them against. The toolchain for building from source is in
[docs/dev-environment.md](docs/dev-environment.md).

Useful references:

| Document | Purpose |
| --- | --- |
| [Setup and evaluation](docs/getting-started.md) | Configuration, archive layout and the fixture dashboard |
| [Configuration reference](docs/configuration.md) | Generated reference for the current, unstable configuration schema |
| [Integrations](docs/integrations.md) | Current first-party integration coverage and requirements |
| [Integration authoring](docs/integration-authoring.md) | Experimental declarative and Rust/WASM extension interfaces |
| [Security model](docs/security.md) | Trust boundaries, deployment assumptions, and operator checklist |
| [Docker notes](docs/docker.md) | Container layout, hardening and the Docker connection |
| [Migration notes](docs/migration.md) | Current import and upgrade design; not a compatibility guarantee |
| [Contributing](CONTRIBUTING.md) | Contribution and verification requirements |

## License

The core is licensed under AGPL-3.0-or-later. `sdk/`, `schemas/`, and first-party integrations are
licensed under Apache-2.0, with a plugin exception that allows community integrations to use other
licences. See [LICENSING.md](LICENSING.md) and
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).
