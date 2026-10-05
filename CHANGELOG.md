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

- Manifest expressions can convert between Unix seconds and RFC 3339: `fromUnix(n)` for a series
  point's timestamp and `unix(t)` for a request value such as a query window's start. Before
  this, a manifest could not chart an upstream that sends Unix timestamps.
  ([#18](https://github.com/sergeyfarin/veduta/issues/18))
- A first-party `prometheus` integration: values (`stat`), charts (`series`) and rankings (`top`)
  from PromQL a card supplies, through the two read-only query endpoints. Tested against responses
  captured from a real Prometheus 3.15.0. See [docs/integrations.md](docs/integrations.md#prometheus).

### Fixed

- Glances, Beszel and Proxmox showed their percentages 100 times too large: 25% CPU read
  "2500.0%" next to a correctly quarter-full bar. The renderer reads a percent-formatted value as
  a 0-1 fraction, and these three manifests passed the upstream's 0-100 number. Their signals are
  unchanged (still 0-100, so existing rules keep working), but the manifests are now `glances`
  0.2.1, `beszel` 0.1.1 and `proxmox` 0.1.1 and need re-approval (`veduta integration diff`, then
  `approve`). ([#19](https://github.com/sergeyfarin/veduta/issues/19))

### Changed

- Open gaps and planned work moved from `docs/03-backlog.md` to
  [GitHub issues](https://github.com/sergeyfarin/veduta/issues). The finished implementation plan
  and the resolved-gap record moved to [docs/archive/](docs/archive/README.md), and
  `CONTRIBUTING.md` now describes how work is tracked and how a release is cut.
- `docs/01-architecture.md` section 6 shows the optional `limits` field in the approval request
  and explains when it is needed.

## 0.2.2 — 2026-09-30

Upgrade instructions, a new light backdrop for the Veil preset, and the decision to freeze the
WebAssembly plugin path. Upgrading from 0.2.x is the tag change alone - nothing needs approving
again and there are no database migrations; the README's new Upgrading section has the command.

### Added

- Upgrade instructions, in the README and in full in `docs/docker.md`: the version is pinned in
  `compose.yaml`, so `docker compose pull` alone does not upgrade - the file has to name the new
  one.

### Changed

- The Veil preset's light backdrop is Luigi Querena's *Campo di San Giovanni e Paolo, Venice*,
  replacing the Canaletto, toned down the same way so text over it keeps its contrast. The dark
  backdrop is unchanged.

- **The WebAssembly plugin path is frozen** ([decision 0005](docs/decisions/0005-wasm-frozen.md)).
  It keeps working and keeps being secured, but gains no new host functions, ABI changes, SDKs or
  integrations, and its ABI stays experimental. Formats a manifest cannot read will be added as
  core decoders for declarative manifests instead. At 1.0 it is removed unless a real integration
  beyond Jellyfin depends on it. Nothing changes for an existing installation.

## 0.2.1 — 2026-09-30

A small release: clearer failures when the data directory is unusable, and a lock file you can
read back without `sudo`. Upgrading from 0.2.0 is the tag change alone - `0.2.1` in both services of
`compose.yaml`, then `docker compose pull` and `docker compose up -d`. Nothing needs approving
again, and there are no database migrations.

### Changed

- `veduta.lock.yaml` is written mode `0640` instead of `0600` - owner-writable and group-readable,
  like `veduta.yaml` - so an operator in the file's group can read and commit the record of what
  they approved when the server runs as another uid. It holds no secret, and only its owner can
  write it.

### Fixed

- A data directory that is a file, sits under a file, cannot be created, or is not writable is now
  reported before the database opens, naming the setting that chose it (`server.dataDir` or
  `--data-dir`) and the absolute path it resolved to. It used to surface as a SQLite error naming
  neither. `veduta --check-config` checks an explicitly set data directory too - without creating
  it, and as a warning, since a configuration is often checked away from the host it is for - and
  accepts `--data-dir`.

## 0.2.0 — 2026-09-29

Charts, an integrations page, and integrations that can be pointed at one thing. A `series` block
draws timestamped values - supplied by an integration, or the card's own retained history - and the
Glances card now shows CPU and memory over a day. The dashboard header links to a read-only page of
what each integration may reach. A card parameter can select an upstream object by path, which lets
Arcane watch any environment and a Home Assistant sensor read only its own entity.

**Upgrading from 0.1.x.** Back up `config/` and `data/`, change the image tag to `0.2.0` in
`compose.yaml` (both services), then `docker compose pull` and `docker compose up -d`. The
`glances`, `arcane` and `homeassistant` manifests changed version, so any of them you use is disabled
until approved again - the new Integrations page lists them with the commands; under compose:

```sh
docker compose run --rm veduta integration approve --config /config/veduta.yaml glances
```

A third-party declarative manifest whose pipeline `path` is an expression no longer loads; see
Changed below.

### Added

- A declarative pipeline path can name a card parameter as a whole segment:
  `path: "/api/states/{entityId}"`. The parameter must be a required or defaulted string or integer,
  a value must be one plain path segment (1–128 characters from `A-Z a-z 0-9 - . _ ~ : @`, not a dot
  segment) or the card fails, and the route must have `*` at that segment. Coverage is checked when
  the manifest loads.
- Connection `headers` accept `${secret:NAME}`, as a webhook's headers already did, so an upstream
  that authenticates with a custom header no longer forces its credential into the config file.
- A `series` block: up to four timestamped numeric lines, drawn by the dashboard itself as SVG with
  no charting library, where a `null` reading is a gap rather than a line drawn across it. Every
  chart carries a text summary of what each line reads now and the range it moved through. An
  integration either supplies the points or binds its own retained signals with `history`, in which
  case Veduta attaches what it has stored for the card (1 hour to 7 days) and the integration
  never reads stored history. The Glances card now shows CPU and memory over the last 24 hours.
- An **Integrations** page, linked from the dashboard header: each integration's status, the routes,
  capabilities and limits it is granted, and what it asks for beyond them - including a requested
  route an approval left out, which is listed rather than hidden - with the command that approves
  it. It is read-only: approval stays at the command line.
- Arcane cards take an `environmentId` parameter (default `"0"`, the local Docker host), so a remote
  host or agent can have a card.

### Changed

- **A pipeline path must be a literal string.** An expression there used to load and was checked
  only when a card ran; it is now refused when the manifest loads, with the placeholder syntax
  above as the replacement. Third-party manifests using one need rewriting (the plugin ABI is
  experimental, as noted at the top of this file).
- `glances` is now manifest version 0.2.0 (its chart), and needs re-approval like the two below.
- `arcane` and `homeassistant` are now manifest version 0.2.0 with changed routes, so both need
  re-approval (`veduta integration diff` then `approve`). Home Assistant's `sensor` reads only its
  own entity from `/api/states/<entityId>` instead of every entity; a missing entity now fails the
  card with HTTP 404 instead of showing "no such entity".
- A connection header written as `${secret:NAME}` is now resolved. Before, it was sent to the
  upstream as that literal text.
- The "authentication is disabled" banner used a colour token that does not exist, so it never
  showed its warning tint. It does now.

## 0.1.3 — 2026-09-29

A security release, and the first whose artefacts drop the `v` from their names. The GitHub
releases and container images for 0.1.0, 0.1.1 and 0.1.2 have been withdrawn because all three
carry the route authorisation flaw below; their tags and entries here are kept so the history stays
readable.
Upgrade to 0.1.3.

### Security

- A request path could carry a query or fragment past route authorisation. A value inside a route's
  `*` segment containing `?` or `#` was authorised as ordinary path text and then parsed as a URL,
  so it could send query keys the route's `queryKeys` did not allow, or reach a shorter path than
  the approved one, on the same connection and method. Paths containing a literal `?` or `#` are
  now refused everywhere a route is checked. Affects 0.1.0–0.1.2.

### Changed

- Release archives and `veduta version` now use the bare version: `veduta-0.1.3-linux-amd64.tar.gz`
  and `0.1.3`, where they used the tag whole (`veduta-v0.1.3-…`, `v0.1.3`). This matches the image
  tag and the changelog headings. The git tag keeps its `v`. Anything that downloads an archive by
  name needs the `v` removed.

### Added

- Issue templates for bug reports, asking for the version, how it was installed, and the init and
  server logs, and pointing security reports at the private advisory form.

## 0.1.2 — 2026-09-28

### Added

- `veduta init` writes a starter weather card, so a fresh install is not an empty page. It reads
  Open-Meteo through the built-in `http-json` integration, needs no account or key, and shows
  Berlin until `latitude` and `longitude` are changed. It is the one outbound request a default
  install makes, and the generated file says so.

### Fixed

- The `veil` backdrop covered only as much of the page as its content was tall, so a short or empty
  dashboard on a tall screen left a flat band below the image. The page is now at least the height
  of the viewport.

## 0.1.1 — 2026-09-28

A patch release that makes the quick start work. The README and `compose.yaml` described
`docker compose up -d` as the whole installation, but that flow landed after 0.1.0 was tagged, so
the published 0.1.0 image answered `unknown command "init"` and the first install failed. Nothing
in the server changed.

### Added

- `veduta init` writes a minimal `veduta.yaml` with a generated administrator password, already
  hashed, and prints the password once. It does nothing when a configuration exists, so it is safe
  to run on every start. `VEDUTA_ADMIN_PASSWORD` chooses the password instead; the plaintext is
  never written to disk. `--fix-permissions` is the escape hatch for hosts where the container's
  uid cannot be chosen.
- `compose.yaml` and the README quick start run a one-shot `veduta-init` service before the
  server, so the installation is `docker compose up -d`.

### Changed

- Every container in the quick start runs as the operator's uid and gid, from `VEDUTA_UID` and
  `VEDUTA_GID` in `.env`, rather than one of them running as root. `./config` and `./data` are bind
  mounts that stay owned by the operator, so no `chown` and no `sudo` are needed to edit or back
  them up. A uid mismatch now reports both uids and the `.env` line that fixes it, where the kernel
  reported only "permission denied".
- The quick start joins `docker compose up -d` and `docker compose logs veduta-init` with `;`
  rather than `&&`, so the logs, the only place a failing init explains itself, print either way.

## 0.1.0 — 2026-09-17

First public release, and an alpha in every sense but the version string: pre-1.0 means no
stability promise, the release is marked a pre-release on GitHub, and `latest` is not published at
all until there is a 1.0 to point it at — installing means naming the version you wanted.

Veduta is a self-hosted dashboard for a home or homelab that shows rich content — photos, posters,
container and host state — queries services through a credential boundary the integrations never
see past, and notifies when something needs attention. Action controls are present but remain
disabled; execution is not implemented in this release.

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
- **Proxmox VE** (`cluster-overview` — node availability, VM and LXC counts, and core-weighted
  cluster CPU and memory from one read-only `/api2/json/cluster/resources`), **Home Assistant**
  (`overview` — entity, light, switch, person and unavailable-entity counts; and `sensor` — one
  entity, with a numeric signal emitted only when the entity carries a unit and is actually
  reporting), **Arcane** (`containers` — status counts and a container page in a single request),
  and **Dockhand** (`overview` — container, stack and image totals summed across every environment
  it manages), bringing the shipped set to ten. Every route in all four is a `GET`, so none can act
  on the system it watches.
- Declarative Immich `memories`: date-filtered on-this-day images, one memory per year, item counts
  and optional browser links. No new WebAssembly module or service is needed.

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
  Homepage's `homeassistant` and `proxmox` widgets map onto real integrations rather than link-only
  cards. Home Assistant's Homepage key is its bearer token and carries across; a Proxmox token is
  two Homepage fields joined into one non-standard header, so the importer writes the card and the
  URL, deliberately leaves the connection with no auth rather than guessing, and prints the header
  to add.
- `veduta health` probes a running instance and exits non-zero if it is not serving — the
  container image is distroless, so the binary is the only thing available to run a health check.
- `veduta auth hash` produces the Argon2id verifier password mode requires, reading the password
  from stdin rather than from an argument and writing the PHC string alone to stdout. Generation
  and verification share one definition of an acceptable cost, so the command cannot emit
  parameters the server would then refuse at start-up.

**Release**
- A Pi-class budget gate for the WebAssembly plugin runtime, running on CI's native four-core ARM64
  runner. It measures cold compilation of the real committed Jellyfin module, compilation from a
  warm cache, warm invocation over 24 calls, and resident memory per loaded instance read from
  `/proc`, then asserts the kill criteria the sandbox spike set before any of it was built: 50 ms
  per warm invocation, 20 MB resident per instance. First measured on 2026-09-17: 244 ms cold,
  12 ms warm, a 1.765 ms warm-invocation median and 1.0 MiB resident per added instance — 20 to 28
  times inside the criteria. 32-bit `linux/arm/v7` stays unmeasured; GitHub has no native runner
  for it.
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

- `plugins/beszel/manifest.yaml` was never schema-validated by the contract suite, which named each
  manifest individually and had not been updated when Beszel was added. The suite now finds every
  `plugins/*/manifest.yaml`, and a new check asserts each one is also listed in
  `hack/stage-plugins.sh` — a manifest missing from that allowlist ships in no release archive and
  no container image, and nothing previously noticed.
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
  [docs/03-backlog.md](https://github.com/sergeyfarin/veduta/issues) rather than left implicit, and each one is named there
  with its priority; closed ones move to
  [docs/archive/03-backlog-resolved.md](docs/archive/03-backlog-resolved.md) with an account of where the fix
  landed. Nothing in that open list is a known defect in a shipped path: the two that were - a
  Widget Document check that existed but was never called, and a live-update slot that leaked when
  a slow stream was dropped - are fixed in this release.
