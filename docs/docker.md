# Docker connection

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
    endpoint: tcp://docker-socket-proxy:2375
    allowActions: false
```

The builtin integration uses a fixed GET-only Engine API allowlist. Docker connections are not
available through the declarative or WASM plugin broker. A proxy that denies container listing is
shown as a restricted warning card rather than breaking the dashboard refresh loop.
