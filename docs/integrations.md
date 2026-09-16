# Shipped integrations

Ten integrations ship with Veduta. Two are compiled into the binary; the other eight are files in
[`plugins/`](../plugins/) that sit beside it — seven declarative manifests and one WebAssembly
module. All eight cross the same approval boundary a third-party integration does: they are
listed, diffed and approved in `veduta.lock.yaml` rather than inheriting the binary's trust.
Nothing here is active until you approve it.

| Integration | Operations | Needs | Runtime |
| --- | --- | --- | --- |
| `docker` | container state | a Docker socket or TCP endpoint | builtin |
| `http-json` | whatever the card's `view:` declares | an HTTP connection | builtin |
| [`immich`](../plugins/immich/manifest.yaml) | `recent-assets`, `memories`, `server-statistics` | an API key | declarative |
| [`jellyfin`](../plugins/jellyfin/manifest.yaml) | `recently-added` | an API key | WebAssembly |
| [`glances`](../plugins/glances/manifest.yaml) | `overview` | a Glances web server | declarative |
| [`beszel`](../plugins/beszel/manifest.yaml) | `overview` | a Beszel Hub token + `systemId` | declarative |
| [`proxmox`](../plugins/proxmox/manifest.yaml) | `cluster-overview` | a PVEAuditor API token | declarative |
| [`homeassistant`](../plugins/homeassistant/manifest.yaml) | `overview`, `sensor` | a long-lived access token | declarative |
| [`arcane`](../plugins/arcane/manifest.yaml) | `containers` | an API key | declarative |
| [`dockhand`](../plugins/dockhand/manifest.yaml) | `overview` | an API token | declarative |

Every one of these declares its complete upstream request surface in its manifest, and every route
in the seven declarative manifests is a `GET`. None of them can act on the system it watches: an
integration that could stop a container or call a Home Assistant service would need a route
declaring that method and path, and none does.

Each entry below gives the connection the integration expects. Declare the plugin, review its
permissions with `veduta integration diff <id>`, approve it, then enable the connection:

```bash
./veduta integration diff --config veduta.yaml proxmox
```

```bash
./veduta integration approve --config veduta.yaml proxmox
```

A rule's `signal()` names a **card**, not an integration — the examples below assume a card whose
id matches the integration's, which is the common case but not automatic.

## Immich memories

The declarative `memories` operation shows one memory per year, newest year first,
with broker-owned thumbnails and item counts. See the
[Immich example and selection policy](../plugins/immich/README.md).

## Proxmox VE

One read-only `GET /api2/json/cluster/resources` describes the whole cluster: every node, guest and
storage entry. The card reports node availability, VM and container counts, and core-weighted CPU
and memory across the online nodes.

Proxmox does not use a bearer token. Its API token goes into one `Authorization` header in Proxmox's
own scheme, assembled from the token's full id and its secret:

```yaml
connections:
  proxmox:
    kind: http
    baseUrl: https://pve.lan:8006
    auth:
      type: header
      name: Authorization
      value: 'PVEAPIToken=veduta@pve!dashboard=${secret:PROXMOX_TOKEN}'
    tls:
      caFile: /etc/veduta/pve-root-ca.pem
```

Create the token under a user with only `PVEAuditor` on `/`, so Proxmox refuses anything the
manifest already refuses. A default Proxmox install presents a self-signed certificate: copy
`/etc/pve/pve-root-ca.pem` from the node and point `tls.caFile` at it rather than reaching for
`insecureSkipVerify`.

Signals: `nodes.online`, `nodes.total`, `vms.running`, `vms.total`, `lxc.running`, `lxc.total`,
`cpu.percent`, `mem.percent`.

```yaml
rules:
  - id: proxmox-node-down
    when: 'signal("proxmox", "nodes.online") < signal("proxmox", "nodes.total")'
    for: 2m
    severity: critical
    notify: [phone]
    resolve: true
```

## Home Assistant

Two operations. `overview` counts entities, lights and switches that are on, people at home, and
entities Home Assistant cannot currently reach. `sensor` renders one entity, named by the card's
`entityId` parameter.

```yaml
connections:
  homeassistant:
    kind: http
    baseUrl: http://homeassistant.lan:8123
    auth: { type: bearer, value: "${secret:HOMEASSISTANT_TOKEN}" }
```

Home Assistant has no read-only token scope — a long-lived access token is exactly as privileged as
the user that created it. Create a dedicated non-administrator user for Veduta. What actually keeps
this integration read-only is its route grant: it declares `GET /api/states` and nothing else, so
neither `/api/services` nor `/api/template` is reachable through it.

Both operations read the full `/api/states`, which is several megabytes on a large installation.
That is a real cost for `sensor`, which uses one entity out of it: Home Assistant does serve a
single entity at `/api/states/<entity_id>`, but a v1 manifest's pipeline path must be a literal
(see [docs/03-backlog.md](03-backlog.md)), so a per-entity path is not expressible yet. Prefer one
overview card and a few sensor cards at a slow refresh over a wall of sensor cards.

A `sensor` card emits a `state` signal always, and a numeric `value` signal only when the entity
carries a `unit_of_measurement` and is currently reporting. When the entity is missing or
unavailable, `value` is absent rather than stale — which a rule reads as unknown, so it neither
fires nor resolves on a number nobody measured.

```yaml
sections:
  - title: House
    cards:
      - id: freezer
        integration: homeassistant
        operation: sensor
        params: { entityId: sensor.freezer_temperature }
        slots: { server: homeassistant }
        refresh: 5m

rules:
  - id: freezer-warming
    when: 'signal("freezer", "value") > -15'
    for: 30m
    severity: critical
    notify: [phone]
```

## Arcane

Container status counts and a page of containers from an Arcane environment, in one request —
Arcane's list endpoint returns both the page and the environment's aggregate counts.

```yaml
connections:
  arcane:
    kind: http
    baseUrl: http://arcane.lan:3552
    auth: { type: header, name: X-Api-Key, value: "${secret:ARCANE_API_KEY}" }
```

**Local environment only.** Arcane scopes its API by environment id as a path segment, and a v1
manifest cannot build a path from a card parameter, so this integration declares
`GET /api/environments/0/containers` — environment `0` being the local Docker host. Remote hosts and
agents added to the same Arcane cannot be given a card yet; the gap is recorded in
[docs/03-backlog.md](03-backlog.md).

Signals: `containers.running`, `containers.stopped`, `containers.total`.

## Dockhand

Dockhand already aggregates every environment it manages — local sockets, TLS hosts and Hawser
agents — into one dashboard payload, so this integration reads that one endpoint and sums across
environments.

```yaml
connections:
  dockhand:
    kind: http
    baseUrl: http://dockhand.lan:3000
    auth: { type: bearer, value: "${secret:DOCKHAND_TOKEN}" }
```

Create the token in Dockhand's settings and give its role only `environments:view`, which is the
permission `/api/dashboard/stats` checks.

Signals: `environments.online`, `environments.total`, `containers.running`, `containers.total`,
`containers.unhealthy`, `containers.pending_updates`, `stacks.running`, `images.total`.

Dockhand collects disk usage and per-container metrics to answer this request, so it is slow by
nature. The operation's default refresh is two minutes and its timeout is fifteen seconds.

```yaml
rules:
  - id: containers-unhealthy
    when: 'signal("dockhand", "containers.unhealthy") > 0'
    for: 5m
    severity: warning
    notify: [phone]
    resolve: true
```

## Host metrics

Glances and Beszel have their own page: [docs/host-metrics.md](host-metrics.md).

## Importing from Homepage

`veduta import homepage` maps Homepage's `glances`, `immich`, `jellyfin`, `homeassistant` and
`proxmox` widgets onto these integrations; other widget types import as link-only cards with a
warning. Home Assistant's Homepage key is its bearer token, so that one carries across completely.
Proxmox's does not — Homepage splits a Proxmox token across `username` and `password`, and Proxmox
wants them joined into one non-standard header — so the importer writes the card and the URL, omits
the auth block rather than guessing at it, and says so in a warning. See
[docs/migration.md](migration.md).
