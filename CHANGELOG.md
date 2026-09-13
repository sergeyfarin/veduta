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

## [Unreleased]

Nothing yet.

## [0.1.0] — unreleased

First public release. Veduta is a self-hosted dashboard for a home or homelab that shows rich
content — photos, posters, charts, container and host state — queries services through a
credential boundary the integrations never see past, performs a small set of explicitly approved
actions, and notifies when something needs attention.

### Added

**Dashboard and rendering**
- A Svelte 5 single-page app embedded in the binary, with renderers for every block type: status,
  metrics, key–value, progress, list, text/markdown, image, image grid, poster grid, table and
  actions.
- Two visual presets, chosen per instance with `dashboard.appearance`: **Clean** (the opaque
  default) and **Veil** (translucent, blurred cards over a generated dusk gradient). Light and dark
  remain a per-viewer browser preference and are deliberately not configured — `dashboard.theme`,
  which never had a reader, is gone. Veil's contrast is proven in CI by compositing its surface
  over both gradient stops rather than being checked by eye.
- `dashboard.background` points Veil at a local image, absolute or relative to the config file.
  Veduta reads and serves it, so no viewer's browser fetches from a third-party host; content is
  sniffed rather than trusted from the extension, and a mandatory scrim keeps every text token
  above its contrast floor against any image, proven against pure black and pure white.
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

[Unreleased]: https://github.com/sergeyfarin/veduta/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/sergeyfarin/veduta/releases/tag/v0.1.0
