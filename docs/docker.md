# Docker

Two unrelated things share this page: **running Veduta as a container**, and **reading container
state from a Docker host** as a dashboard card.

The [README quick start](../README.md#quick-start) is the short version — one compose file, and
`docker compose up -d`. This page explains what it chose and how to extend it.
[`compose.yaml`](../compose.yaml) in the repository root is that same deployment plus the Docker
socket proxy described at the bottom of this page.

## Running Veduta

Images are published to GitHub Container Registry for `linux/amd64`, `linux/arm64` and
`linux/arm/v7`. `latest` is deliberately not published before 1.0, so name the version:

```sh
docker pull ghcr.io/sergeyfarin/veduta:0.1.0
```

The image is [distroless](https://github.com/GoogleContainerTools/distroless): no shell, no
package manager, and the binary is the only executable in it. It runs as uid `65532` (`nonroot`).

### What `veduta init` does, and why it is a separate service

`veduta init` writes `/config/veduta.yaml` if there is not one, with an administrator password it
generates and hashes, and prints that password once. It then does nothing on every subsequent run,
so it is safe to leave in the compose file — it will not overwrite a configuration you have since
edited. There is no shell in the image to do this instead: the binary carries the command, which
is also why it works identically outside a container.

It exists because two settings are mandatory and neither has a usable default in a container:

```yaml
server:
  listen: "0.0.0.0:8099"   # 127.0.0.1 inside a container is reachable only from inside it
  dataDir: /data
auth:
  mode: password           # Veduta refuses a non-loopback bind without authentication
  admin:
    username: admin
    passwordHash: "$argon2id$..."
```

Worth being precise about that refusal, because it surprises people running containers: it
inspects the address **Veduta binds**, not the one Docker publishes. It cannot see past the
container boundary, so it fires even when `ports:` publishes to `127.0.0.1` and nothing off-host
can reach the service. On a single-user homelab that is the check being conservative rather than
detecting a real exposure — but as soon as you publish the port to your LAN, it is describing a
genuine one.

`--i-know-what-im-doing` overrides the refusal. Behind a trusted reverse proxy that terminates
authentication, prefer `auth.mode: forward` over the override — see the
[security model](security.md).

### Why the container runs as your uid

Nothing in the deployment runs as root. The compose file sets `user:` on both services from
`VEDUTA_UID`/`VEDUTA_GID` in `.env`, so the container is you.

This is the part of Docker that has no clean built-in answer. Bind mounts carry host ownership
through untranslated — a file owned by uid 1000 on the host is owned by uid 1000 inside the
container, whatever the container calls that user — and the image's own uid is 65532, which owns
nothing on your host. Docker offers no remapping for this: there is no equivalent of podman's
`:U` mount flag or Kubernetes' `fsGroup`. The options are therefore only ever:

| | |
| --- | --- |
| **Match the uid** (what this deployment does) | No root, no `chown`, files stay yours. Needs to know your uid. |
| `chown -R :65532` the directories yourself | No root container, but a manual step, repeated for every new mount, and `sudo` to undo. |
| A privileged container that fixes ownership at startup | No setup for the operator, at the cost of a container running as root. Common in self-hosted images — Homepage's entrypoint starts as root and drops via `su-exec` — but it is the option Docker's own [security guidance](https://docs.docker.com/engine/security/) argues against, so it is not the default here. |
| A named volume | Inherits the image's ownership, so it needs nothing at all — but you cannot easily edit a configuration file inside one. |

Matching the uid is the only one of those that costs the operator nothing *and* keeps every
container unprivileged, which is why it is the default.

### When you cannot choose the uid

Some hosts do not let you: a NAS appliance UI that starts containers for you, or a `./config`
that is already owned by root because Docker created it when it did not exist. Two ways out, in
order of preference.

**Prepare the directories once, and drop `user:`.** The server then runs as the image's own uid,
which is what it does when nothing overrides it:

```sh
mkdir -p config data
sudo chown -R :65532 config && sudo chmod -R g+w config
sudo chown -R 65532:65532 data
```

Granting the group rather than transferring `config` is deliberate: you keep editing
`veduta.yaml` without `sudo`, and uid 65532 can still write `veduta.lock.yaml` beside it.

**Or let `init` do it, once, as root.** `veduta init --fix-permissions` grants uid 65532 the
group on the configuration directory and hands the file it writes back to the directory's owner.
It is the privileged option from the table above, so it is documented rather than default — use
it when the alternatives are closed to you:

```yaml
  veduta-init:
    image: ghcr.io/sergeyfarin/veduta:0.1.0
    user: "0:0"
    command: ["init", "--fix-permissions"]
    volumes:
      - ./config:/config
    restart: "no"
```

The server service keeps its own `user:`, `read_only`, `cap_drop` and `no-new-privileges`
regardless; only this one-shot container is privileged, and it exits before the server starts.

### Named volumes, if you prefer them

The image ships both `/config` and `/data` owned by uid 65532, so a named volume mounted at
either one inherits that ownership and needs no preparation and no `user:` line. `/data` is a
good candidate. `/config` is a worse one only because editing `veduta.yaml` inside a named volume
is awkward — which is the whole reason the default binds it.

### Choosing or changing the password

`init` generates one. To choose it instead, set `VEDUTA_ADMIN_PASSWORD` on the init service: it is
hashed on the first run and the plaintext is never written to disk. An environment variable is
visible to anyone who can reach the Docker daemon, so for anything long-lived prefer generating
one and replacing it afterwards.

To change the password later, replace `auth.admin.passwordHash` in `veduta.yaml` and let the
server reload. `auth.admin.passwordHash` is an Argon2id PHC string, never a password.
`veduta auth hash` reads the password from stdin — never from an argument, which would put it in
shell history and in every other user's `ps` — and writes the verifier to stdout and nothing else:

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

What lands in `veduta.yaml` is a verifier, not a password: someone who reads it cannot log in with
it, only attack it offline at 64 MiB per guess. `init` writes the file mode `0640` for that
reason, and service credentials — which *are* usable by whoever reads them — belong somewhere else
entirely.

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

Both must be writable by whatever uid the container runs as — your own, in the default
deployment, which is what makes them need no preparation beyond `mkdir`.

**`/data`** holds the SQLite database and the asset and icon caches. **`/config`** holds
`veduta.yaml`, `conf.d/` and `veduta.lock.yaml`. `/config` has to stay writable beyond the first
run: approving an integration writes `veduta.lock.yaml` into that directory, from
`veduta integration approve` and from the dashboard's approve button alike, and the write is
atomic, so it also creates a temporary file beside the target. If the directory is not writable
the server starts and serves normally and only approval fails, with
`creating temp file: permission denied` — a poor place to discover a mount option. Mount
`/config` `:ro` only if you approve integrations elsewhere and copy the resulting lock file in.

Both directories exist in the image owned by uid 65532, so a named volume mounted at either one
inherits that ownership and works with no `user:` line at all. That is the only reason they are
in the image: without `/data` there, Docker would create the volume `root:root` and SQLite would
report the failure as `unable to open database file (out of memory)` — which says nothing about
permissions.

`veduta.lock.yaml` is written mode `0600`, owned by the uid that wrote it. When that uid is not
yours — because you dropped `user:` and let the server run as 65532 — reading it back on the host
to commit it beside `veduta.yaml`, as you should, takes `sudo`.

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
