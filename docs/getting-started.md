# Getting started

Veduta runs as one Go binary with an embedded web application. Go 1.27, Node 24, and pnpm 11 are pinned in [`mise.toml`](../mise.toml). This source checkout is the current installation path while release artifacts are being prepared.

Install the toolchain and build the binary:

```sh
mise install
pnpm install
pnpm build
```

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

`auth.mode: none` is restricted to loopback. Use password or forward authentication before exposing Veduta through a network or reverse proxy. Password mode requires an Argon2id PHC string in `auth.admin.passwordHash`; store it as a secret reference rather than plaintext:

```yaml
auth:
  mode: password
  admin:
    username: admin
    passwordHash: ${secret:VEDUTA_PASSWORD_HASH}
```

Set `VEDUTA_PASSWORD_HASH` in the process environment or mount it at `/run/secrets/VEDUTA_PASSWORD_HASH`. A mounted secret file takes precedence. See the [security model](security.md) before exposing the service and the generated [configuration reference](configuration.md) for every field.

Start from [`examples/veduta.yaml`](../examples/veduta.yaml) to connect real services. Connections own base URLs and credentials. Cards bind an integration's abstract slot to a connection ID. External integrations remain inactive until their requested manifest digest, capabilities, routes, and limits are approved:

```sh
./veduta integration list --config veduta.yaml
./veduta integration diff my-integration --config veduta.yaml
./veduta integration approve my-integration --config veduta.yaml
```

The server watches the primary file and sibling `conf.d/*.yaml` files. A changed configuration is validated and its complete runtime is activated before publication; a failed reload leaves the previous generation running and reports diagnostics through the configuration-status API.

For containers, persistence, and health checks, continue with the [Docker guide](docker.md). To bring an existing Homepage setup across, use the [migration guide](migration.md).
