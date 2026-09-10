# Docker

Two unrelated things share this page: **running Veduta as a container**, and **reading container
state from a Docker host** as a dashboard card.

## Running Veduta

Images are published to GitHub Container Registry for `linux/amd64`, `linux/arm64` and
`linux/arm/v7`:

```sh
docker pull ghcr.io/sergeyfarin/veduta:0.1.0
```

The image is [distroless](https://github.com/GoogleContainerTools/distroless): no shell, no
package manager, and the binary is the only executable in it. It runs as uid `65532` (`nonroot`).

```yaml
services:
  veduta:
    image: ghcr.io/sergeyfarin/veduta:0.1.0
    restart: unless-stopped
    ports:
      - "8099:8099"
    volumes:
      - ./config:/config:ro     # veduta.yaml, conf.d/, veduta.lock.yaml
      - veduta-data:/data       # SQLite database, asset and icon caches
    environment:
      VEDUTA_ADMIN_HASH: ${VEDUTA_ADMIN_HASH:?set this}

volumes:
  veduta-data:
```

### Two settings the container will not start without

**Bind the container's own interface.** The default listen address is `127.0.0.1:8099`, which
inside a container is reachable only from inside that container. Set it in the mounted config —
the config stays authoritative, so the image does not override it:

```yaml
server:
  listen: "0.0.0.0:8099"
  dataDir: /data
```

**Configure authentication.** Veduta *refuses to start* on a non-loopback address until it is,
and a container binding `0.0.0.0` is squarely that case. This is deliberate: from Phase D onward
the dashboard holds real service credentials, so a misconfiguration that would have exposed them
fails loudly instead of quietly working.

```yaml
auth:
  mode: password
  admin:
    username: admin
    passwordHash: ${secret:VEDUTA_ADMIN_HASH}
```

`--i-know-what-im-doing` overrides the refusal. Behind a trusted reverse proxy that terminates
authentication, prefer `auth.mode: forward` over the override — see the
[security model](security.md).

### Permissions on /data

`/data` must be writable by uid `65532`. A named volume (as above) is created with the right
ownership automatically. A bind mount is not:

```sh
mkdir -p ./data && sudo chown -R 65532:65532 ./data
```

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

The shipped manifests are baked in at `/usr/share/veduta/plugins`, so a configuration can
reference them without mounting anything:

```yaml
integrations:
  - id: immich
    source: path:/usr/share/veduta/plugins/immich
```

Third-party plugins are mounted by you and approved in `veduta.lock.yaml` exactly as they are
outside a container. **The plugin ABI is experimental and changes between releases** — a plugin
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

```yaml
services:
  docker-socket-proxy:
    image: tecnativa/docker-socket-proxy:latest
    environment:
      CONTAINERS: 1
      INFO: 1
      POST: 0
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  veduta:
    depends_on: [docker-socket-proxy]
```

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
