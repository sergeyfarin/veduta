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
  from the first log line, so a credential cannot reach a log by omission.
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

**Release**
- Multi-architecture container images on GHCR for `linux/amd64`, `linux/arm64` and `linux/arm/v7`,
  built on a distroless base with no shell and running as a non-root user.
- Signed-off, checksummed release archives for `linux/amd64`, `linux/arm64`, `linux/arm/v7` and
  `darwin/arm64`, each carrying the licences and the generated attribution alongside the binary.
- `THIRD-PARTY-NOTICES.md`, generated from what is actually shipped — Rollup's module graph for
  the frontend bundle and the resolved dependency closure for the WebAssembly plugins — served by
  the running instance at `/api/v1/notices` and linked from the dashboard footer.
- An AGPL section 13 source link in the footer and in `GET /api/v1/version`, pointing at the exact
  commit the binary was built from.

### Known limitations

- The plugin ABI is experimental; see the note above.
- The Go guest SDK is deferred — the official Go PDK requires WASI and misses the guest size and
  cold-validation targets, so weakening the sandbox to accommodate it was refused. Rust is the
  supported plugin language.
- Open gaps found during implementation are tracked honestly in
  [docs/03-backlog.md](docs/03-backlog.md) rather than left implicit.

[Unreleased]: https://github.com/sergeyfarin/veduta/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/sergeyfarin/veduta/releases/tag/v0.1.0
