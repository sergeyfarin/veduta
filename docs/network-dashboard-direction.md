# After trying Homarr: a smaller Veduta for network and DNS

Updated **2026-10-10**. This supplements the [product direction review](product-direction.md)
and [Homarr v2 deep dive](homarr-v2-deep-dive.md), incorporating the owner's actual trial:
Homarr's appearance, UI editing and Jellyfin recently-added widget work well; Immich memories
and meaningful Prometheus metrics remain gaps. This is a proposed experiment, not an accepted
ADR or authorization to remove integrations. Homarr source observations use the same pinned
v2.3.0 checkout as the deep dive; live documentation was checked again.

## Recommendation

**Use Homarr as the home page and media dashboard. Narrow active Veduta work to a useful network
and DNS overview, initially using the existing Prometheus installation and optionally direct
AdGuard statistics.** Preserve the other integrations while testing this direction; reducing the
active scope does not require deleting working code or creating a second repository.

The proposed promise is: **“Is my home network healthy, and what explains today's change?”**
This has a more specific audience and workflow than a general home-lab widget catalogue. It is
still a hypothesis: fewer widgets alone do not differentiate Veduta. The overview must make
common questions easier to answer than a Homarr custom widget or a small Grafana dashboard.

## What Homarr extensions can safely do

There are two materially different extension models:

- **Custom/community widget definitions** run in Homarr's restricted JSX interpreter, rather
  than receiving arbitrary JavaScript execution. Supported layouts, charts and inputs are
  flexible, but imports, hooks, arbitrary functions and direct browser request code are excluded.
  See the [runtime contract](https://homarr.dev/docs/management/custom-widgets/custom-jsx/).
- **Native widgets and integrations**, including changes in a fork or upstream contribution,
  are application code. The custom interpreter is not their security boundary. Review these as
  trusted changes to the Homarr server and frontend.

For custom definitions, network calls are named server requests. Homarr injects encrypted
credentials, enforces source origins and network scope, checks permissions and applies request,
response, concurrency and time limits. Authenticated requests do not follow redirects. Actions
have separate authorization and do not run automatically on board load. These are meaningful
protections against arbitrary execution and accidental authority expansion. The
[request contract](https://homarr.dev/docs/management/custom-widgets/requests-and-security/)
explains the exact limits.

The balance is sensible for a homelab, but it is not a trustless marketplace. The administrator
still chooses which service a definition may reach and should inspect its routes, methods and
actions. A read can expose private data, and an upstream API can make a nominal read change
state. Homarr therefore requires **full integration access even for arbitrary GET requests**
through a saved integration; bounded native operations can offer a narrower contract. This
protects credentials and authority, but limits sharing a custom integration widget with
less-privileged viewers. The
[integration request implementation](https://github.com/homarr-labs/homarr/blob/v2.3.0/packages/api/src/router/integration/integration-http.ts)
is useful evidence beyond the documentation.

Do not assume every displayed resource goes through the credential broker: the custom image
renderer accepts ordinary HTTP(S) image URLs. That is distinct from server-authenticated API
requests; it neither injects an API-key header nor guarantees server-proxied images. See the
[pinned renderer](https://github.com/homarr-labs/homarr/blob/v2.3.0/packages/custom-widgets/src/runtime/data.tsx).
Never solve authenticated thumbnails by placing a service key in a browser-visible image URL.
These observations describe boundaries, not a comprehensive security audit.

## The two missing widgets call for different solutions

**Prometheus is a promising custom-widget experiment.** The native integration deliberately
counts scrape targets through `/api/v1/targets`; it does not run arbitrary PromQL.
That is a native-widget limitation, not proof that Homarr cannot display metrics. Its generic
requests could call `/api/v1/query` or `/api/v1/query_range`, and the restricted renderer has
charts. Response shape conversion, time windows, label selection, empty results and request
budgets still need testing. Start with one fixed, read-only query and one modest range chart,
not a complete Grafana replacement. See [Homarr's Prometheus documentation](https://homarr.dev/docs/integrations/prometheus/)
and the [Prometheus HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/).

**Immich memories are a better candidate for a focused native contribution.** A custom request
can plausibly retrieve memory metadata, but that does not automatically produce authenticated
thumbnail URLs. Homarr's native Immich integration already creates proxied image links with
server-held credentials; its helper is private and the generic custom response path does not
expose equivalent binary image brokering. A native memories mode could reuse that mechanism,
subject to upstream acceptance and a live API test. This is an architectural inference, not a
claim that a prototype was run or no community solution exists. See the
[pinned Immich integration](https://github.com/homarr-labs/homarr/blob/v2.3.0/packages/integrations/src/immich/immich-integration.ts)
and [custom response parser](https://github.com/homarr-labs/homarr/blob/v2.3.0/packages/custom-widgets/src/server/response.ts).
Veduta's existing memories implementation also needs its documented live validation before it
should count as a proven alternative.

## Keep Prometheus, or poll sources directly?

**Keep Prometheus for the first experiment because it is already collecting the data.** Current
Veduta main already offers Prometheus stat, series, top and table operations. That avoids building
collection and presentation simultaneously. Its range API supplies historical data even across
Veduta downtime. Use a restricted read-only endpoint; query cost still needs bounding.

Direct AdGuard access is reasonable: its API exposes aggregated statistics through
`GET /control/stats`, including a bounded lookback. An overview need not fetch individual DNS
query logs or enable configuration actions. However, a route restricted in Veduta is not the same
as an upstream account with read-only privileges. Credentials and upstream permissions still
matter. See the [official API specification](https://github.com/AdguardTeam/AdGuardHome/blob/master/openapi/openapi.yaml).
Avoid collecting the same DNS data by two routes without a concrete reason.

Direct OpenWrt access is more involved. You must discover available interfaces and Wi-Fi data,
handle firmware/package differences, reconnect sessions, distinguish cumulative counters from
rates, and handle resets, outages and missing samples. Veduta has bounded signal history already;
it would not need a database from scratch. But correct counter conversion and useful aggregation
are additional responsibilities that existing exporters and Prometheus currently carry.
The [official Wi-Fi station exporter](https://github.com/openwrt/packages/blob/master/utils/prometheus-node-exporter-lua/files/usr/lib/lua/prometheus-collectors/wifi_stations.lua)
illustrates that collection layer.

There is a security wrinkle: OpenWrt's HTTP ubus RPC dispatches an object and method from the
request body. Allowing a POST route alone cannot distinguish observation from administration.
Use upstream read-only RPC ACLs and a constrained adapter; do not rely on a path-only grant.
See [uhttpd's session access checks](https://github.com/openwrt/uhttpd/blob/master/ubus.c)
and Veduta's documented route-grant limitations in [the architecture](01-architecture.md).

Direct polling becomes attractive if removing the monitoring stack is itself the goal: one
router, one DNS service, modest history and no other Prometheus consumers. For the current
installation, removing Prometheus would probably increase development work before it decreases
operating complexity. A future optional direct adapter can coexist with a Prometheus adapter
behind the same presentation model; do not build both now.

## Alternatives worth keeping open

| Path | Main benefit | Main cost / reason to reject |
| --- | --- | --- |
| Homarr plus custom PromQL widgets | One dashboard and existing UI editor; least new application maintenance | Author and maintain response transformations; narrower custom runtime and sharing permissions |
| Homarr plus Grafana | Mature metric queries and history; fastest route to dependable charts | Another interface; a graph collection may not explain household network health |
| Homarr plus focused Veduta | Existing Go app and query operations; coherent network/DNS workflow | Must earn the cost of a second dashboard through better interpretation and usability |
| Homarr plus Veduta with direct sources | Potentially fewer services for a tiny installation | Veduta takes over collection, rates, history and source-specific maintenance |
| Existing OpenWrt/AdGuard interfaces | Almost no development; full source-specific diagnostics | Fragmented overview and navigation; weak cross-source explanation |
| Focused Homarr contributions, pause Veduta | Improvements benefit existing users; preserves one primary dashboard | Upstream design/review timing; contributing a widget does not grant control of the roadmap |

Home Assistant remains useful when the goal is household devices and automation. Routing all
network statistics through it merely for this dashboard would add a dependency without an
established benefit. A separate collector plus another UI is also premature: introduce a shared
collector only when multiple real consumers need it.

## A small experiment and decision gates

For the next two weeks of actual use, ship one readable network board. First resolve the existing
configuration-to-renderer issue (#24), so accepted settings visibly work. Then use the current
Prometheus integration and add only the missing DNS input. Favor a working configured example,
clear source/label requirements and visible stale/error states over integration count.

Aim for six to eight panels answering concrete questions:

1. Is WAN traffic unusually high? Current throughput and a day of context.
2. Are clients connected and Wi-Fi conditions reasonable? Counts and signal where exported.
3. Which devices account for traffic? Only if the available metrics support that attribution.
4. Is DNS healthy? Query volume, blocked share, upstream failures and response time.
5. Can I trust this display? Last successful collection and explicit missing/stale data.

A blocked-query percentage is not a safety score. Do not guess device identities or manufacture
per-device traffic from metrics that do not contain it. Keep media in Homarr and link to Grafana,
OpenWrt and AdGuard for investigation.

Compare one representative throughput chart in all three plausible homes: Homarr custom widget,
Grafana and Veduta. Record setup time, readability on a phone, freshness behavior and whether you
actually open the result. If Homarr or Grafana answers the questions adequately, stop expanding
Veduta. If Veduta becomes the useful daily overview, the mid-term work is saved presets, clear
units, stable device naming and comparisons with normal behavior. Consider a small incident
record only after users demonstrate a need to explain past changes.

Longer term, an optional direct-source edition could serve small installations without
Prometheus. A curated network diagnostic view or exportable dashboard pack could be a better
product than a universal dashboard platform. Do not commit to actions, an extension marketplace,
AI explanations or another media catalogue until the narrow workflow proves useful.
