# Docker

Two unrelated things share this page: **running Veduta as a container**, and **reading container
state from a Docker host** as a dashboard card.

The [README quick start](../README.md#quick-start) is the short version — three steps, and a
compose file you can copy out of it. This page explains what those steps chose and how to extend
them. [`compose.yaml`](../compose.yaml) in the repository root is that same deployment plus the
Docker socket proxy described at the bottom of this page.

## Running Veduta

Images are published to GitHub Container Registry for `linux/amd64`, `linux/arm64` and
`linux/arm/v7`. `latest` is deliberately not published before 1.0, so name the version:

```sh
docker pull ghcr.io/sergeyfarin/veduta:0.1.0
```

The image is [distroless](https://github.com/GoogleContainerTools/distroless): no shell, no
package manager, and the binary is the only executable in it. It runs as uid `65532` (`nonroot`).

### Two settings the container will not start without

**Bind the container's own interface.** The default listen address is `127.0.0.1:8099`, which
inside a container is reachable only from inside that container. Set it in the mounted config —
the config stays authoritative, so the image does not override it:

```yaml
server:
  listen: "0.0.0.0:8099"
  dataDir: /data
```

**Configure authentication.** Veduta *refuses to start* on a non-loopback address until it is, and
a container binding `0.0.0.0` is squarely that case. This is deliberate: from Phase D onward the
dashboard holds real service credentials, so a misconfiguration that would have exposed them fails
loudly instead of quietly working.

Worth being precise about what that check sees, because it surprises people running containers:
it inspects the address **Veduta binds**, not the one Docker publishes. It cannot see past the
container boundary, so it fires even when `ports:` publishes to `127.0.0.1` and nothing off-host
can reach the service. On a single-user homelab that is the check being conservative rather than
detecting a real exposure — but as soon as you publish the port to your LAN, it is describing a
genuine one.

`--i-know-what-im-doing` overrides the refusal. Behind a trusted reverse proxy that terminates
authentication, prefer `auth.mode: forward` over the override — see the
[security model](security.md).

### Producing the password hash

`auth.admin.passwordHash` is an Argon2id PHC string, never a password. `veduta auth hash` reads the
password from stdin — never from an argument, which would put it in shell history and in every
other user's `ps` — and writes the verifier to stdout and nothing else:

```sh
docker run --rm -i ghcr.io/sergeyfarin/veduta:0.1.0 auth hash
```

Terminal input is echoed. To keep the password off the screen, or to script it:

```sh
read -rs -p 'Password: ' pw && printf %s "$pw" | \
  docker run --rm -i ghcr.io/sergeyfarin/veduta:0.1.0 auth hash
```

The cost defaults to RFC 9106's second recommended configuration — 64 MiB, three passes, four
lanes — and `--memory`, `--iterations` and `--parallelism` adjust it within the range the login
path is willing to verify. Verification reads the cost from the hash itself, so raising it later
re-costs new passwords without invalidating the one you have.

The quick start pastes that string straight into `veduta.yaml`. It is a verifier, not a password:
someone who reads it cannot log in with it, only attack it offline at 64 MiB per guess. The file
still deserves the permissions you would give any config holding a hash.

### Keeping credentials out of the config file

Real service credentials are a different matter, and `${secret:NAME}` exists for them — an Immich
API key in plaintext *is* usable by anyone who reads the file. Veduta resolves `${secret:NAME}`
from `/run/secrets` first and the environment second, which is exactly where Compose mounts a
Docker secret:

```yaml
services:
  veduta:
    secrets:
      - IMMICH_KEY

secrets:
  IMMICH_KEY:
    file: ./secrets/immich-key
```

```yaml
connections:
  immich:
    kind: http
    baseUrl: http://immich:2283
    auth: { type: header, name: x-api-key, value: "${secret:IMMICH_KEY}" }
```

One entry per `${secret:NAME}` the configuration uses — each needs its own file and its own line
in both blocks, or the server refuses to load the configuration and names the reference it could
not resolve. Prefer a secret file to an environment variable: `docker inspect` reveals a
container's environment to anyone who can reach the daemon.

The admin hash can move here too, as `${secret:VEDUTA_ADMIN_HASH}`, once you are already
maintaining secret files for credentials.

### The two mounts

**`/data` must be writable by uid 65532.** The image ships a `/data` directory already owned by
that uid, so Docker seeds a named volume mounted there with the same ownership and nothing is
required of you — this is why the quick start uses a named volume. A bind mount takes the host
directory's ownership instead, and must be prepared:

```sh
mkdir -p ./data && sudo chown -R 65532:65532 ./data
```

(Without the directory in the image, a named volume would be created `root:root` and SQLite would
report the resulting failure as `unable to open database file (out of memory)` — which says nothing
about permissions. That is why it is in the image.)

**`/config` is writable too, and needs preparing only if you approve integrations from the
dashboard.** A first run with no integrations never touches it. Approving one writes
`veduta.lock.yaml` into that directory — from `veduta integration approve` and from the
dashboard's approve button alike — and the write is atomic, so it also creates a temporary file
beside the target. A bind mount keeps the host directory's ownership, which is yours, not the
container's. Grant the group rather than transferring the directory, so that you keep editing
`veduta.yaml` without `sudo`:

```sh
sudo chown -R :65532 ./config && sudo chmod -R g+w ./config
```

Without it the server starts and serves normally, and only approval fails, with
`creating temp file: permission denied` — a poor place to discover a mount option. Mount `/config`
`:ro` only if you approve integrations elsewhere and copy the resulting lock file in.

`veduta.lock.yaml` itself is written mode `0600` owned by uid 65532, so reading it back on the
host — to commit it beside `veduta.yaml`, as you should — takes `sudo`.

### Hardening, and exposure

The container runs with a read-only root filesystem, all capabilities dropped and
`no-new-privileges`, on top of the uid and distroless base the image already provides. Nothing
outside the two mounts is ever written — the frontend is embedded in the binary and no temporary
files are created — so none of that costs anything.

<a id="exposure"></a>
What it cannot decide for you is exposure. The published port is `127.0.0.1:8099`, the host's
loopback only: Veduta speaks plain HTTP, and its session cookie carries no `Secure` attribute, so
a LAN-visible port means session tokens crossing the network in the clear. Put a TLS-terminating
reverse proxy in front for anything beyond this host. Changing it to `8099:8099` for a throwaway
test on a trusted network is a deliberate downgrade, not a default.

### Health checks

The image declares a `HEALTHCHECK` that runs the binary against itself:

```sh
veduta health --addr http://127.0.0.1:8099
```

It exits non-zero when the instance is not serving, so `docker ps` reports `unhealthy` and
orchestrators restart it. There is no shell or `curl` in the image to do this instead — that is
why the probe lives in the binary, and why it cannot drift from the endpoint it checks. The same
command works from outside for an external probe.

### First-party integrations

The shipped manifests are staged into the image at `/usr/share/veduta/plugins` — the same set, from
the same distribution list, that a release archive carries — so a configuration can reference them
without mounting anything:

```yaml
integrations:
  - id: immich
    source: path:/usr/share/veduta/plugins/immich
```

Third-party plugins are mounted by you and approved in `veduta.lock.yaml` exactly as they are
outside a container — mount them somewhere other than `/usr/share/veduta/plugins`, which the image
replaces on every upgrade. The first-party manifests are approved the same way: shipping in the
image grants them nothing. **The plugin ABI is experimental and changes between releases** — a plugin
built against an older ABI is refused at load rather than run, so expect to rebuild and re-approve
on upgrade.

### Verifying a download

Release archives ship with `SHA256SUMS`:

```sh
sha256sum --check --ignore-missing SHA256SUMS
```

Images carry build provenance and an SBOM attestation, readable with
`docker buildx imagetools inspect ghcr.io/sergeyfarin/veduta:0.1.0`.

---

## Docker connection

Veduta reads container state through the Docker Engine HTTP API. Prefer a read-only socket proxy
over mounting `/var/run/docker.sock` into Veduta; direct access to that socket is effectively root
access to the host.

[`compose.yaml`](../compose.yaml) carries such a proxy behind a profile, so it starts only when you
ask for it:

```sh
docker compose --profile docker up -d
```

It is pinned, granted `CONTAINERS` and `INFO` and refused `POST`, and its port is not published to
the host — only the compose network reaches it. Veduta deliberately does not `depends_on` it: a
missing proxy degrades one card to a warning rather than holding up the dashboard.

Point the Veduta connection at the proxy and leave actions disabled:

```yaml
connections:
  docker-local:
    kind: docker
    endpoint: tcp://docker-socket-proxy:2375   # read-only proxy
    allowActions: false
```

The builtin integration uses a fixed GET-only Engine API allowlist. Docker connections are not
available through the declarative or WASM plugin broker. A proxy that denies container listing is
shown as a restricted warning card rather than breaking the dashboard refresh loop.
