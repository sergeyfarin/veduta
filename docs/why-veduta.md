# Why Veduta?

> Comparison reviewed 2026-10-09, including Homarr v2. Dashboard projects change quickly; follow
> the linked official documentation when making a deployment decision. The
> [product direction review](product-direction.md) weighs whether to continue Veduta or extend
> an established dashboard, with near-, mid-, and long-term options.

Veduta is not an attempt to replace every self-hosted dashboard. Mature projects already offer
large widget catalogues, polished configuration experiences, and active communities. They are the
more appropriate choice for a dashboard needed today: Veduta's documented current release,
`v0.2.2`, is a pre-1.0 pre-release with no stability promise, and it cannot yet execute an action.

Veduta is instead testing one product thesis:

> Can a home dashboard support visually rich, third-party integrations without asking the
> operator to give each integration unrestricted credentials, network access, or a way to inject
> its own frontend code?

Its answer is a deliberately constrained integration model. An integration requests named
capabilities and upstream routes, receives no service credential directly, and returns a typed
Widget Document from a fixed rendering vocabulary. The core applies credentials, budgets, route
checks, output validation, and asset proxying. More complicated integrations may run as sandboxed
WebAssembly guests, but they cross the same broker and rendering boundaries.

That model trades flexibility for inspectability. A template or frontend component can express
almost anything; a Veduta integration can express only what the broker and Widget Document allow.
The restriction is the point, but whether it remains useful in practice is still being tested.

A second hypothesis is being tested alongside the dashboard, not instead of it: that a short
record of what changed while nobody was looking, and of what still needs attention, makes current
status more useful. The first test is a built-in Activity card on the dashboard, used on a few
existing installs. The [private activity feed proposal](proposals/0001-private-activity-feed.md)
describes the plan, the risks, and how the result will be judged. It is an experiment, not shipped
functionality.

## Comparison with established dashboards

This is a comparison of architecture and product emphasis, not a security ranking. Each project
has different goals, and configuration mistakes or deployment choices can change its effective
security. “Veduta distinction” describes the design being evaluated, not a claim that Veduta is
more mature or generally better.

| Project | Configuration and extension model | Established strengths | Veduta's intended distinction |
| --- | --- | --- | --- |
| [Homepage](https://gethomepage.dev/) | YAML files or Docker/Kubernetes discovery; a broad catalogue of service widgets. Adding a new built-in widget involves registering frontend code in the project. | Extensive service coverage, familiar YAML workflow, container discovery, and proxied API requests. | Side-loaded integrations produce only typed blocks; the core owns credentials and enforces approved routes. |
| [Glance](https://github.com/glanceapp/glance) | YAML; built-in widgets, JSON-to-Go-template `custom-api` widgets, iframes, and HTTP extensions that may return HTML when explicitly allowed. | Small single binary, strong feed-oriented layout, flexible templates, and an active community-widget collection. | Integrations cannot supply HTML or DOM code; presentation stays in the fixed renderer while network access stays behind the broker. |
| [Homarr](https://homarr.dev/docs/) | Browser-managed, database-backed boards; v2 custom widgets combine named server requests with a restricted JSX runtime and separately stored credentials. | Visual editing, multiple boards, users and groups, interactive widgets, and Workshop distribution. | YAML and a digest-bound approval lock, with a smaller resolved block vocabulary. Credential mediation and constrained widgets also exist in Homarr. |
| [Dashy](https://dashy.to/docs/) | YAML with a browser configuration editor; many built-in widgets, extensive theming, and server-proxied requests with environment-variable secret substitution. | Wide customisation, multiple views, authentication options, and more than 50 documented widgets. | Server-produced typed documents and per-integration grants replace a general widget option/proxy contract. |

### What Veduta is not claiming

- It is not currently easier to install, more stable, or better supported than these projects.
- It does not currently match their widget catalogues or configuration interfaces.
- A capability model reduces authority only when its policies and implementation are correct; it
  does not make the project automatically secure.
- Rich media is an emphasis, not a claim that other dashboards cannot display images or video.
- The WebAssembly path is experimental and frozen: maintained, not extended, and removed at 1.0
  unless a real integration needs it ([decision 0005](decisions/0005-wasm-frozen.md)).

## When the trade-off may be worthwhile

Homarr v2 substantially overlaps with the original integration thesis: it also mediates service
requests and constrains widget rendering. Veduta's remaining distinctions are the specific
approval contract, file-oriented operation, deployment shape, and smaller presentation vocabulary,
not an exclusive claim to credential-safe custom widgets. Whether those differences justify an
independent app is unproven; the [direction review](product-direction.md) recommends a practical
Homarr v2 trial before broad new investment.

Veduta's model may fit an operator who wants configuration in version-controlled files, expects to
install integrations from authors they do not personally audit, and values predictable rendering
and narrowly scoped service access more than unrestricted widget code. It is a poor fit for
someone who wants a mature dashboard now, a large integration catalogue, a GUI-first workflow, or
arbitrary HTML and frontend customisation.

## Sources and maintenance notes

The comparison above uses project-owned documentation:

- Homepage: [project overview](https://github.com/gethomepage/homepage),
  [Docker discovery](https://gethomepage.dev/configs/docker/), and
  [widget authoring](https://gethomepage.dev/widgets/authoring/tutorial/).
- Glance: [project overview](https://github.com/glanceapp/glance),
  [configuration and widget types](https://github.com/glanceapp/glance/blob/main/docs/configuration.md),
  and [extension protocol](https://github.com/glanceapp/glance/blob/main/docs/extensions.md).
- Homarr: [boards](https://homarr.dev/docs/management/boards/),
  [integrations](https://homarr.dev/docs/management/integrations/), and
  [custom widgets](https://homarr.dev/docs/management/custom-widgets/), including
  [requests and security](https://homarr.dev/docs/management/custom-widgets/requests-and-security/)
  and the [restricted JSX runtime](https://homarr.dev/docs/management/custom-widgets/custom-jsx/).
- Dashy: [project documentation](https://dashy.to/docs/),
  [configuration](https://dashy.to/docs/configuring/), and
  [widgets](https://dashy.to/docs/widgets/).

Re-check the relevant source before changing a comparison. Avoid counts unless the upstream
project currently publishes them, describe architectural differences without assigning motives,
and keep Veduta's maturity warning alongside any competitive claim.
