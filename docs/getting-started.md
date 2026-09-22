# Setup and evaluation

> [!WARNING]
> `v0.1.0` is a pre-release. Pre-1.0 carries no stability promise: configuration, storage,
> packaging, and extension interfaces may change between releases without a migration path. Action
> controls render and are permanently disabled. Do not rely on this as a production dashboard.

Veduta runs as one Go binary with an embedded web application, and ships three ways.

**Container image.** `latest` is deliberately not published before 1.0, so name the version:

```sh
docker pull ghcr.io/sergeyfarin/veduta:0.1.0
```

The [README quick start](../README.md#quick-start) is a compose file you can copy and
`docker compose up -d`, which writes a configuration and generates an administrator password on
the first run. [`compose.yaml`](../compose.yaml) in the repository is that deployment plus a
read-only Docker socket proxy; [docs/docker.md](docker.md) explains what both chose.

**Release archive.** `linux/amd64`, `linux/arm64`, `linux/arm/v7` and `darwin/arm64` are attached to
each [release](https://github.com/sergeyfarin/veduta/releases), with a `SHA256SUMS` to verify a
download against:

```sh
sha256sum --check --ignore-missing SHA256SUMS
```

**From source.** Go 1.27, Node 24, and pnpm 12 are pinned in [`mise.toml`](../mise.toml):

```sh
mise install
pnpm install
pnpm build
```

## Release archive layout

An archive unpacks to a single relocatable directory:

```
veduta-<version>-<target>/
├── veduta
├── plugins/          the first-party integrations
│   ├── arcane/manifest.yaml
│   ├── beszel/manifest.yaml
│   ├── dockhand/manifest.yaml
│   ├── glances/manifest.yaml
│   ├── homeassistant/manifest.yaml
│   ├── immich/manifest.yaml
│   ├── proxmox/manifest.yaml
│   └── jellyfin/{manifest.yaml,jellyfin.wasm}
└── LICENSE, LICENSING.md, LICENSE-PLUGIN-EXCEPTION.txt, THIRD-PARTY-NOTICES.md, README.md
```

The integrations are files beside the binary, not bytes inside it. That is deliberate: a shipped integration is loaded through the same path as one you write yourself, so it is listed, diffed and approved in `veduta.lock.yaml` on exactly the same terms rather than inheriting the binary's trust. Nothing is active until you approve it.

With `veduta.yaml` next to the binary, reference them by relative path — paths resolve from the configuration file's directory, never the working directory:

```yaml
integrations:
  - id: immich
    source: path:./plugins/immich
```

The system layout matches the container image:

| | |
| --- | --- |
| binary | `/usr/local/bin/veduta` |
| configuration | `/etc/veduta/veduta.yaml`, `conf.d/`, `veduta.lock.yaml` |
| first-party integrations | `/usr/share/veduta/plugins/` |
| your own integrations | `/var/lib/veduta/plugins/` |
| data | `/var/lib/veduta/` |

With the configuration in `/etc/veduta`, name the integrations absolutely:

```yaml
integrations:
  - id: immich
    source: path:/usr/share/veduta/plugins/immich
```

Keep your own integrations outside `/usr/share/veduta/plugins`: that directory belongs to the release and is replaced wholesale on upgrade. Replace the binary and that directory together and restart — a manifest from one release beside a binary from another will fail approval rather than run, which is safe but confusing to diagnose. See the [migration guide](migration.md) for the full upgrade sequence.

To inspect the dashboard without service credentials or configuration, start the checked-in showcase:

```sh
./veduta serve --fixtures
```

Open <http://127.0.0.1:8099>. The fixture server is for development and demonstrations.

For a local dashboard, create `veduta.yaml` with an explicit authentication policy:

```yaml
version: 1
server:
  listen: 127.0.0.1:8099
  dataDir: ./data
auth:
  mode: none
dashboard:
  title: Home
sections: []
```

Validate and start it:

```sh
./veduta --check-config --config veduta.yaml
./veduta serve --config veduta.yaml
```

`auth.mode: none` is restricted to loopback. Use password or forward authentication before exposing Veduta through a network or reverse proxy. Password mode requires an Argon2id PHC string in `auth.admin.passwordHash`, which may be written literally — it is a verifier, not a password, and cannot be used to log in:

```yaml
auth:
  mode: password
  admin:
    username: admin
    passwordHash: "$argon2id$v=19$m=65536,t=3,p=4$..."
```

Service credentials are the opposite case: an API key in plaintext is usable by whoever reads it, so keep those behind `${secret:NAME}`. The same reference works for the hash once you are maintaining secret files anyway:

```yaml
    passwordHash: ${secret:VEDUTA_PASSWORD_HASH}
```

Produce the hash with the binary. It reads the password from stdin — never from an argument, which would leave it in shell history and in every other user's `ps` — and writes the PHC string to stdout and nothing else, so it can be redirected straight into a secret file:

```sh
./veduta auth hash > /run/secrets/VEDUTA_PASSWORD_HASH
```

Terminal input is echoed; to keep the password off the screen, pipe it in instead:

```sh
read -rs -p 'Password: ' pw && printf %s "$pw" | ./veduta auth hash
```

The cost defaults to RFC 9106's second recommended configuration (64 MiB, three passes, four lanes); `--memory`, `--iterations` and `--parallelism` adjust it within the range the login path will verify. Because verification reads the cost from the hash itself, raising it later re-costs new passwords without invalidating existing ones.

Set `VEDUTA_PASSWORD_HASH` in the process environment or mount it at `/run/secrets/VEDUTA_PASSWORD_HASH`. A mounted secret file takes precedence. See the [security model](security.md) before exposing the service and the generated [configuration reference](configuration.md) for every field.

Start from [`examples/veduta.yaml`](../examples/veduta.yaml) to connect real services. Connections own base URLs and credentials. Cards bind an integration's abstract slot to a connection ID. External integrations remain inactive until their requested manifest digest, capabilities, routes, and limits are approved:

```sh
./veduta integration list --config veduta.yaml
./veduta integration diff --config veduta.yaml my-integration
./veduta integration approve --config veduta.yaml my-integration
```

Flags come before the integration id: argument parsing stops at the first non-flag word, so a
`--config` written after the id is read as a second positional argument and the command prints its
usage instead of running.

Card, list-item, and action icons accept literal glyphs, absolute HTTP(S) URLs, or `mdi:name`,
`si:name`, and `sh:name` identifiers. Veduta serves all remote icons through its own bounded disk
cache. Common first-party icons are embedded, and a failed fetch falls back to text, so rendering
the dashboard never depends on a browser reaching a CDN.

The server watches the primary file and sibling `conf.d/*.yaml` files. A changed configuration is validated and its complete runtime is activated before publication; a failed reload leaves the previous generation running and reports diagnostics through the configuration-status API.

For containers, persistence, and health checks, continue with the [Docker guide](docker.md). To bring an existing Homepage setup across, use the [migration guide](migration.md).
