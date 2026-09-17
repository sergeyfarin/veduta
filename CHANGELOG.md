# Changelog

All notable changes to Veduta are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) — with one explicit carve-out while the
project is pre-1.0:

> **The plugin ABI is experimental.** The WebAssembly guest ABI, the host functions in
> [`sdk/rust`](sdk/rust), and the integration manifest schema are **not** covered by semantic
> versioning yet and will break between minor releases. Manifest digests and module hashes are
> pinned in `veduta.lock.yaml`, so an incompatible plugin fails closed rather than misbehaving;
> expect to rebuild and re-approve third-party plugins when upgrading.

## Unreleased

### Added

- Declarative Immich `memories`: date-filtered on-this-day images, one memory per
  year, item counts and optional browser links. No new WASM module or service is
  needed. The Immich manifest is now 0.3.0 and requires renewed approval.

- Four first-party declarative integrations, bringing the shipped set to ten:
  **Proxmox VE** (`cluster-overview` — node availability, VM and LXC counts, and core-weighted
  cluster CPU and memory from one read-only `/api2/json/cluster/resources`), **Home Assistant**
  (`overview` — entity, light, switch, person and unavailable-entity counts; and `sensor` — one
  entity, with a numeric signal emitted only when the entity carries a unit and is actually
  reporting), **Arcane** (`containers` — status counts and a container page in a single request),
  and **Dockhand** (`overview` — container, stack and image totals summed across every environment
  it manages). Every route in all four is a `GET`, so none can act on the system it watches.
  Documented in the new [docs/integrations.md](docs/integrations.md).
- A Pi-class budget gate for the WASM plugin runtime, running on CI's native four-core ARM64
  runner beside the existing 50-card load gate. It measures cold compilation of the real committed
  Jellyfin module, compilation from a warm cache, warm invocation over 24 calls, and resident
  memory per loaded instance read from `/proc`, then asserts the kill criteria the sandbox spike
  set before any of it was built: 50 ms per warm invocation, 20 MB resident per instance. These
  figures were the one piece of the WASM runtime's acceptance that cross-compilation could not
  supply. First measured on 2026-09-17: 244 ms to compile the module from cold, 12 ms from a warm
  cache, a 1.765 ms warm-invocation median and 1.0 MiB resident per added instance — 20 to 28 times
  inside the criteria, so a Pi-class arm64 host runs an integration for about two milliseconds and
  a megabyte. 32-bit `linux/arm/v7` stays unmeasured; GitHub has no native runner for it.

- `veduta import homepage` now maps Homepage's `homeassistant` and `proxmox` widgets onto real
  integrations instead of importing them as link-only cards. Home Assistant's Homepage key is its
  bearer token and carries across; a Proxmox token is two Homepage fields joined into one
  non-standard header, so the importer writes the card and the URL, deliberately leaves the
  connection with no auth rather than guessing, and prints the header to add.

### Fixed

- `plugins/beszel/manifest.yaml` was never schema-validated by the contract suite, which named
  each manifest individually and had not been updated when Beszel was added. The suite now finds
  every `plugins/*/manifest.yaml`, and a new check asserts each one is also listed in
  `hack/stage-plugins.sh` — a manifest missing from that allowlist ships in no release archive and
  no container image, and nothing previously noticed.

## 0.1.0-alpha.1 — planned

Planned first public alpha, and deliberately a prerelease: the tag carries an `-alpha.1`
identifier, so the GitHub release is marked pre-release and the container image publishes only as
`ghcr.io/sergeyfarin/veduta:0.1.0-alpha.1` — `latest` and `0.1` are left untouched for a release
that earns them. Veduta is a self-hosted dashboard for a home or homelab that shows rich
content — photos, posters, container and host state — queries services through a
credential boundary the integrations never see past, and notifies when something needs attention.
Action controls are present but remain disabled; execution is not implemented in this alpha.

### Added

**Dashboard and rendering**
- A Svelte 5 single-page app embedded in the binary, with renderers for every block type: status,
  metrics, key–value, progress, list, text/markdown, image, image grid, poster grid, table and
  actions.
- Two visual presets: **Clean** (the opaque default) and **Veil** (translucent, blurred cards over
  a painted backdrop). `dashboard.appearance` sets the instance default and each viewer can pick
  either for themselves, remembered in their own browser — two independent three-valued controls,
  one per axis, because light/dark and clean/veil are orthogonal and `auto` is meaningful on both.
  Nothing is stored server-side. `dashboard.theme`, which never had a reader, is gone.
- Veil's backdrop is a public-domain veduta per colour scheme — Canaletto's *Molo, Venice, from the
  Bacino di San Marco* by day, Vernet's *Entrance to the Port of Palermo by Moonlight* by night —
  each shipped desaturated and contrast-compressed so it sits behind the cards rather than
  competing with them, with a generated gradient beneath as the fallback if an image does not load.
  Cards sit at 55% opacity over it with a 22px backdrop blur, so the preset reads as glass.
- The scrim that guarantees contrast over a backdrop is **two** values, not one. The bundled
  paintings are files in this repository, so their floors are proven against the pixels they
  actually contain and they need a scrim of only 0.18; a configured `dashboard.background` is an
  arbitrary file, can only be defended against pure black and pure white, and gets 0.70. A test
  holds the invariant that the unknown case never gets the cheaper defence.
- `dashboard.background` replaces the bundled painting with a local image, absolute or relative to
  the config file. Veduta reads and serves it, so no viewer's browser fetches from a third-party
  host; content is sniffed rather than trusted from the extension.
- The Widget Document and CardState envelopes, with Go and TypeScript types generated from one
  JSON Schema so the two halves cannot disagree.
- Live updates over SSE with heartbeats, an event replay ring, `Last-Event-ID` resumption and
  per-session stream caps; stale-while-revalidate rendering that shows last-known-good data,
  visibly marked, after a restart or a failed refresh.
- A local-first icon proxy resolving `mdi:`, `si:`, `sh:` and URL specs through a bounded disk
  cache, with an embedded offline pack so rendering never depends on reaching a CDN.

**Configuration**
- `veduta.yaml` plus `conf.d/*.yaml`, schema-validated and semantically checked, with every
  diagnostic carrying `file:line:col`. `veduta --check-config` exits non-zero on error.
- Secrets as `env:` and `file:` references with a redacting value type and a log scrubber active
  from the first log line, so a credential cannot reach a log by omission. A card whose document
  repeats a configured secret verbatim is rejected rather than served: the scheduler checks every
  document a run produces, and every one restored from storage, against the same set of values.
- Atomic live reload: a changed configuration is validated and its complete runtime generation
  built before publication, and a failed reload leaves the previous generation serving.

**The trust model**
- Integrations declare the capabilities and the exact upstream routes they want; the core decides
  whether they are allowed and holds every credential. Connections own base URLs, authentication,
  TLS, timeouts and rate limits, and integrations never see any of it.
- `veduta.lock.yaml` records an approval bound to a canonical manifest digest.
  `veduta integration list | diff | approve` shows an administrator exactly what they are granting,
  and approving a subset of what a manifest requests is the normal case.
- A WebAssembly sandbox with no filesystem, environment, sockets or native HTTP; memory ceilings,
  deadline interruption, output caps and sha256-pinned modules, with a conformance suite in CI.
- A declarative runtime for integrations that are pure data — a YAML manifest, no compiled code —
  covering the common case without a sandbox at all.
- An HTTP client that pins resolved IPs against DNS rebinding and enforces redirect, size and rate
  limits, with redirects re-checked against the approved route set.

**Shipped integrations**
- Immich and Glances and Beszel (declarative), Jellyfin (WebAssembly), Docker via a read-only
  socket proxy, and a generic HTTP/JSON card for anything without a manifest.

**Operations**
- SQLite persistence with forward-only migrations for card state, scheduling, signal history,
  events, sessions and caches.
- A scheduler with jitter, single-flight de-duplication, exponential backoff and a circuit breaker
  that surfaces as `stale`/`error` and never silently as `disabled`.
- Rules over declared signals and execution state, with debounce windows, and a credential-safe
  ntfy and webhook outbox with bounded retries and flood control. Rule transitions and their event
  and outbox effects commit in one transaction.
- Password and forward authentication, a sudo window gating privileged operations, and a
  persistent audit trail. A non-loopback bind is refused outright until authentication is
  configured.
- `veduta import homepage` parses an existing Homepage configuration, maps services and bookmarks,
  creates connections and integrations disabled for review, and applies a comment-preserving edit.
- `veduta health` probes a running instance and exits non-zero if it is not serving — the
  container image is distroless, so the binary is the only thing available to run a health check.
- `veduta auth hash` produces the Argon2id verifier password mode requires, reading the password
  from stdin rather than from an argument and writing the PHC string alone to stdout. Generation
  and verification share one definition of an acceptable cost, so the command cannot emit
  parameters the server would then refuse at start-up.

**Release**
- Multi-architecture container images on GHCR for `linux/amd64`, `linux/arm64` and `linux/arm/v7`,
  built on a distroless base with no shell and running as a non-root user.
- `compose.yaml`: a complete deployment rather than a fragment to adapt. The password hash arrives
  as a Docker secret at `/run/secrets`, which is where secret resolution looks first and which
  `docker inspect` does not reveal; the container runs with a read-only root filesystem, no
  capabilities and `no-new-privileges`; the port publishes to loopback, since Veduta speaks plain
  HTTP and belongs behind a TLS-terminating proxy; and the read-only Docker socket proxy sits
  behind a profile, pinned and unpublished.
- Signed-off, checksummed release archives for `linux/amd64`, `linux/arm64`, `linux/arm/v7` and
  `darwin/arm64`, each carrying the licences and the generated attribution alongside the binary.
- `THIRD-PARTY-NOTICES.md`, generated from what is actually shipped — Rollup's module graph for
  the frontend bundle and the resolved dependency closure for the WebAssembly plugins — served by
  the running instance at `/api/v1/notices` and linked from the dashboard footer.
- An AGPL section 13 source link in the footer and in `GET /api/v1/version`, pointing at the exact
  commit the binary was built from.

### Fixed

- A panic anywhere under a card refresh no longer ends the process. Integration code runs behind a
  barrier that logs the panic and its stack against the card that caused it and fails that card
  with an `internal` error, leaving the rest of the dashboard serving; the scheduler releases the
  card's single-flight entry however a run leaves, so a panicking card can no longer wedge every
  later refresh of itself.
- A card that declared no `params:` block crashed the server: its nil parameter map marshalled to
  the JSON literal `null`, which the declarative runtime decoded over the map it was about to
  write the manifest's defaults into. Defaults now apply identically whether a card omits
  `params:` or gives an empty one.

### Known limitations

- The plugin ABI is experimental; see the note above.
- The Go guest SDK is deferred — the official Go PDK requires WASI and misses the guest size and
  cold-validation targets, so weakening the sandbox to accommodate it was refused. Rust is the
  supported plugin language.
- Open gaps found during implementation are tracked honestly in
  [docs/03-backlog.md](docs/03-backlog.md) rather than left implicit, and each one is named there
  with its priority; closed ones move to
  [docs/03-backlog-resolved.md](docs/03-backlog-resolved.md) with an account of where the fix
  landed. Nothing in that open list is a known defect in a shipped path: the two that were - a
  Widget Document check that existed but was never called, and a live-update slot that leaked when
  a slow stream was dropped - are fixed in this release.
