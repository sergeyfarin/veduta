# Shipped integrations

Eleven integrations ship with Veduta. Two are compiled into the binary; the other nine are files in
[`plugins/`](../plugins/) that sit beside it — eight declarative manifests and one WebAssembly
module. All nine cross the same approval boundary a third-party integration does: they are
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
| [`prometheus`](../plugins/prometheus/manifest.yaml) | `stat`, `series`, `top`, `table` | a Prometheus server; PromQL on each card | declarative |

Every one of these declares its complete upstream request surface in its manifest, and every route
in the eight declarative manifests is a `GET`. None of them can act on the system it watches: an
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
this integration read-only is its route grant: it declares `GET /api/states` and `GET /api/states/*`
and nothing else, so neither `/api/services` nor `/api/template` is reachable through it.

`overview` reads the full `/api/states`, which is several megabytes on a large installation, so give
it a slow refresh. `sensor` reads only its own entity from `/api/states/<entityId>`, a few hundred
bytes, so sensor cards are cheap. An `entityId` that is not one plain path segment fails the card
rather than being sent.

A `sensor` card emits a `state` signal, and a numeric `value` signal only when the entity carries a
`unit_of_measurement` and is currently reporting. When the entity is unavailable, `value` is absent
rather than stale — which a rule reads as unknown, so it neither fires nor resolves on a number
nobody measured. When the entity no longer exists (renamed in Home Assistant, say), Home Assistant
answers 404 and the card shows that error and emits no signals at all.

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

The card's `environmentId` parameter chooses the environment and defaults to `0`, the local Docker
host; remote hosts and agents added to the same Arcane have their own ids, shown in Arcane's
environment list. The route is `GET /api/environments/*/containers`, so an approval grants that one
endpoint in every environment the API key can see, and nothing else.

```yaml
      - id: edge-containers
        integration: arcane
        operation: containers
        params: { environmentId: "3", limit: 6 }
        slots: { server: arcane }
```

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

## Prometheus

Prometheus keeps the history and does the aggregation; a card asks it a few PromQL questions
through its JSON query API and shows the answers. Use it for whatever already lands in Prometheus
(router and per-device traffic exporters, DNS exporters, anything with a `/metrics` endpoint)
rather than teaching Veduta each source. Hosts that run Beszel or Glances are better served by
those integrations directly.

```yaml
connections:
  prometheus:
    kind: http
    baseUrl: http://prometheus.lan:9090
    auth: { type: none }
```

The manifest declares two routes, `GET /api/v1/query` and `GET /api/v1/query_range`, and nothing
else, so the admin API, rule and target listings and remote write are unreachable through it. An
approval does grant reading **every series that Prometheus holds**, because the query is the
card's to choose: put Prometheus behind authentication of its own if some of its series should
not reach the dashboard.

Four operations:

- `stat` shows up to six instant queries as values. Each query should return one sample (wrap a
  wider result in `sum()`, since only the first sample is shown); one with no samples, or a `NaN`,
  shows a dash rather than failing the card. The first query's value is also the `value` signal,
  absent when there is nothing to read.
- `series` draws up to four range queries over a `window` of `1h`, `6h`, `24h` (the default) or
  `7d`. The step is chosen from the window so a line stays within the chart's 288 points: 15 s,
  90 s, 6 min and 42 min. Each query should return one series; only the first is drawn.
- `top` ranks the series one instant query returns, largest first, titling each row by the metric
  label named in `label` (and optionally subtitling it by `subtitle`). `limit` is 1-20, default 5.
- `table` sets up to four instant queries side by side, one row per series. Up to four `labels`
  name the rows and become the leading columns; the rows come from the first value's query,
  largest first, up to `limit` (1-20, default 10). Each later value is the sample of the series in
  its own query whose labels **all** match the row's, or a blank where none does - so every query
  must keep the labels you list (`sum by (mac, node) (...)` for both, not `by (mac)` for one).

`format` takes the dashboard's value formats: `number`, `count`, `bytes`, `bytes-rate`,
`percent`, `duration` or `temperature`. **`percent` expects a 0-1 fraction**, so divide a 0-100
metric by 100 in the query. Give a chart or a list `span: { rows: 2 }`; one row is too short for
either.

```yaml
      - id: internet
        title: Internet
        integration: prometheus
        operation: stat
        slots: { server: prometheus }
        params:
          queries:
            - { label: Today, query: 'sum(increase(wan_rx_bytes_total[1d]))', format: bytes }
            - { label: Now,   query: 'sum(rate(wan_rx_bytes_total[2m]))',     format: bytes-rate }

      - id: family-traffic
        title: Family, last 24 hours
        integration: prometheus
        operation: series
        slots: { server: prometheus }
        span: { rows: 2 }
        params:
          format: bytes-rate
          lines:
            - { label: Desktop, query: 'sum(rate(client_rx_bytes_total{client="desktop"}[5m]))' }
            - { label: Console, query: 'sum(rate(client_rx_bytes_total{client="console"}[5m]))' }

      - id: top-talkers
        title: Busiest devices today
        integration: prometheus
        operation: top
        slots: { server: prometheus }
        span: { rows: 2 }
        params:
          query: 'topk(5, sum by (client) (increase(client_rx_bytes_total[1d])))'
          label: client
          format: bytes

      - id: wifi-clients
        title: Wi-Fi clients
        integration: prometheus
        operation: table
        slots: { server: prometheus }
        span: { columns: 2, rows: 3 }
        params:
          labels:
            - { name: mac,  label: Device }
            - { name: node, label: Access point }
          values:
            - { label: Signal dBm, query: 'max by (mac, node) (wifi_station_signal_dbm)' }
            - { label: Down,   query: 'max by (mac, node) (wifi_station_receive_kilobits_per_second) * 125', format: bytes-rate }
            - { label: Up,     query: 'max by (mac, node) (wifi_station_transmit_kilobits_per_second) * 125', format: bytes-rate }
```

The Wi-Fi metrics are OpenWrt's `prometheus-node-exporter-lua-wifi_stations` collector's, with
`node` a label your scrape config gives each access point; the other names are placeholders. Use
whatever your exporters publish, which the
Prometheus web UI will list. A query Prometheus cannot parse fails the card with HTTP 400; try it
in that UI first, where the error says what is wrong.

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
