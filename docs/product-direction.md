# Product direction review: Veduta and the home lab stack

Reviewed **2026-10-09**, against Veduta `main` at `ba0a344`, the current docs, all five ADRs,
both proposals, the spike and archived planning records, and the live GitHub backlog. External
comparisons use project-owned documentation and release notes linked below. This is advice and
a proposed sequence of experiments, **not an accepted ADR or a commitment to implement them**.

## Recommendation

**Try current Homarr v2 against the actual desired home page before investing in another broad
Veduta development phase.** Homarr v2 overlaps substantially with both Veduta's visible features
and its original constrained-integration pitch. If its experience now works for you, custom
widgets and focused upstream contributions are likely the shortest route to useful results.

If Veduta continues independently, its most promising role is **a calm, compact overview of home
and lab, with a selective record of changes and clear links to the tools that own the detail**.
Make the existing dashboard dependable and easy to configure first; test the Activity card next.
Keep Grafana for investigation and diagrams, Home Assistant for devices and automations, and
existing monitors for uptime. An independent app should earn its maintenance cost through a
daily workflow or a deployment preference people actually care about.

The proposed user promise is: **“In thirty seconds, see what needs attention, what changed, and
where to go next.”** That is a hypothesis to test. An attractive grid, another widget catalogue,
or a capability broker by itself does not establish demand. A small personal project can also be
worth maintaining simply because its owner enjoys it; that is a different success criterion from
building an alternative for other people.

## What Veduta actually has

The engineering foundation is stronger than the onboarding and everyday workflow. The
[README](../README.md), [architecture](01-architecture.md), [integration guide](integrations.md),
and [changelog](../CHANGELOG.md) establish the following baseline:

| Area | Present in the reviewed code | Product implication |
| --- | --- | --- |
| Deployment | Go binary with embedded Svelte UI, SQLite, containers and ARM builds; init and reload | A viable small deployment, without needing a new platform |
| Presentation | Typed metrics, lists, tables, media, native series charts; Clean and Veil presets | Enough vocabulary to test useful dashboards now |
| Service access | Core-owned credentials, slots, bounded requests and asset proxy, reviewed lock grants | Valuable inspectability; approval and reapproval add operator work |
| Data | Scheduled polling, persisted card state and declared numeric history | Status and short trends already have a substrate |
| Notifications | Rules, persisted debounce, transition events, ntfy/webhook outbox | Usable ingredients, without a consolidated attention experience |
| Integrations | Docker, HTTP JSON, Immich, Jellyfin, Glances, Beszel, Proxmox, Home Assistant, Arcane, Dockhand; Prometheus on `main` | Concentrate on complete workflows rather than an integration-count target |
| Administration | Read-only Integrations page, CLI review/approval, Homepage importer, YAML validation | Reviewable, but reaching a working setup still needs several steps |

**Released and implemented are different.** The documented current release is `v0.2.2`.
Prometheus `stat`, `series`, `top`, and `table`, Unix timestamp helpers, and the percentage fixes
are under **Unreleased**. They must not be advertised as available in the `0.2.2` image. The
Activity card and executable actions are proposals, not functionality. The existing action
buttons remain disabled; the Home Assistant integration polls state and cannot control devices.

A concrete near-term gap emerged in the review: `dashboard.title`, layout columns and gap,
tag grouping, card tags, and section collapse are accepted configuration but do not reach the
current presentation path. The dashboard API omits them, the frontend type lacks them, and
`App.svelte` uses a literal “Home” heading and an unconfigured `Grid`. This is now
[issue #24](https://github.com/sergeyfarin/veduta/issues/24). Card spans and appearance do work;
they are separate settings. Silent configuration promises deserve attention before more knobs.

Historical documents need to be read in order. ADR 0004's original chart gap was closed by 0.2;
ADR 0003's WASM proof-case discussion was narrowed by ADR 0005; archived phases are completion
records, not a current roadmap. The September comparison in [Why Veduta](why-veduta.md) predates
Homarr v2. This review supplements those records rather than rewriting their historical evidence.

## What the surrounding tools already do well

These are fit assessments inferred from documented behavior, not comparative performance tests
or security audits. None of the products was newly deployed as part of this review.

| Tool | Established role and evidence | Consequence for Veduta |
| --- | --- | --- |
| Homepage | YAML service groups, links, multiple widgets per service and a large service-widget catalogue ([services](https://gethomepage.dev/configs/services/), [widgets](https://gethomepage.dev/widgets/index.html)) | A links-and-counters replacement has limited differentiation. Preserve importing and useful recipes; avoid catalogue parity. |
| Glance | A feed-oriented dashboard with YAML, custom API templates and HTTP extensions; its documented fetching is page-load/cache driven ([overview](https://github.com/glanceapp/glance), [extensions](https://github.com/glanceapp/glance/blob/main/docs/extensions.md)) | A pleasant information page is already served. Veduta's persisted background observations could matter for incidents, but a generic RSS page is a weak new direction. |
| Homarr v2 | Browser-managed boards, responsive layouts and permissions; custom widgets with separately stored credentials and restricted templates ([boards](https://homarr.dev/docs/management/boards/), [custom widgets](https://homarr.dev/docs/management/custom-widgets/)) | The strongest direct alternative. Test extending it before duplicating its editor, groups, controls and distribution. |
| Dashy | Configurable sections and dynamic widgets, with a browser configuration workflow ([widgets](https://dashy.to/docs/widgets/), [configuration](https://dashy.to/docs/configuring/)) | Extreme customisation is occupied territory. Veduta's curated appearance can be a preference, but needs usability evidence. |
| Grafana with Prometheus | Querying, exploration, alerting, dashboard variables and time ranges; Canvas supports arranged elements and data-bound connections ([overview](https://grafana.com/docs/grafana/latest/introduction/), [Canvas](https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/visualizations/canvas/)) | Use Veduta for a few selected values and trends. Let Grafana own historical investigation and the current house/network-diagram trial. |
| Home Assistant | Auto-generated and visually editable dashboards, device controls and automations; Activity records household changes ([dashboards](https://www.home-assistant.io/dashboards/), [automation](https://www.home-assistant.io/docs/automation/), [Activity](https://www.home-assistant.io/integrations/logbook/)) | A second smart-home engine is unlikely to help. Surface selected outcomes and link to HA; test whether an HA dashboard alone satisfies the household view. |
| Uptime Kuma / Gatus | Dedicated monitoring and status pages; Kuma documents protocol checks and notifications, Gatus describes status, alerting and incident support ([Kuma](https://github.com/louislam/uptime-kuma), [Gatus](https://github.com/TwiN/gatus)) | Consume their conclusions where useful. A failed Veduta API refresh is not proof that a service is down for users. |
| Beszel / Glances | Existing host-metric sources; Beszel includes historical container statistics and alerts ([Beszel](https://beszel.dev/guide/what-is-beszel), [Veduta host integration](host-metrics.md)) | Keep their agents and detail views. Do not add a Veduta host agent just to collect the same data. |
| ntfy | Straightforward HTTP publishing and phone notifications ([docs](https://docs.ntfy.sh/)) | Keep urgent delivery in a notification tool. The potential added value is reviewing context and recovery later. |

## Homarr v2 changes the calculation

Homarr [2.0.0 shipped on 2026-10-02](https://github.com/homarr-labs/homarr/releases/tag/v2.0.0);
its [release list](https://github.com/homarr-labs/homarr/releases) identifies 2.3.0 on October 7.
The v2 release introduced Custom Widgets and Workshop, rebuilt board editing, and expanded
integrations. Disliking the v1 experience is a reason to test v2 carefully, rather than a reason
to assume either that it still fails or that the rewrite fixed the particular problem you had.

The architectural overlap is real. Homarr's
[request documentation](https://homarr.dev/docs/management/custom-widgets/requests-and-security/)
describes server-side credential injection, fixed source origins, public/private/loopback scopes,
DNS and redirect checks, permissions, and bounded requests. Its
[JSX runtime](https://homarr.dev/docs/management/custom-widgets/custom-jsx/) interprets a restricted
expression language with evaluation and rendering budgets, safe components, and blocked arbitrary
functions, imports and browser requests. Its
[workbench](https://homarr.dev/docs/management/custom-widgets/authoring/) includes request testing,
preview data and a redacted journal. “Custom widgets without handing them every API key” is
therefore not a persuasive exclusive Veduta claim.

### Differentiation that remains

| Candidate distinction | What remains meaningful | Strength and limitation |
| --- | --- | --- |
| YAML plus a reviewed lock as authority | Veduta's manifest requests, independent operator approval, digest binding and fail-closed upgrades are explicit contracts | Real architectural difference. The inspected Homarr docs do not establish an equivalent independent digest-bound lock workflow; that is not proof of its absence or of inferior security. Test whether anyone wants the extra review work. |
| Smaller presentation contract | Veduta documents are resolved typed values rendered by the host; Homarr widgets have a richer interpreted template and interaction surface | Real tradeoff: simpler consistency and author expectations versus less flexibility. A stricter contract is useful only while it expresses the desired content. |
| File-based operations and small deployment | Veduta's YAML, secret files and relocatable binary fit operators who want diffable config and minimal runtime machinery | A defensible niche, not unique across all dashboards: Glance also offers a Go binary and YAML. Measure memory and setup cost on the intended hardware before claiming a performance advantage. |
| Curated visual experience | Predictable density, media treatment, readable stale states and a restrained appearance | Can justify personal use. Easily copied; Homarr's new layout may already be satisfactory. |
| Selective durable changes across services | One coherent incident/recovery/reminder experience could reduce checking several tools | Most interesting product hypothesis, but unshipped. HA Activity, monitoring histories and notifications already cover parts of it. A custom Homarr widget might cover the desired view too. |

What does **not** distinguish Veduta convincingly today: self-hosting, rich cards, secrets kept on
the server, extensible service widgets, “safe” templates in general, charts, or an eventual AI
widget generator. WASM is frozen and has no demonstrated additional workload; it cannot carry
the product pitch. An enabled restart button would close a gap, not create a new product category.

The key security distinction is a specific authority model, not a ranking. Homarr also separates
board and integration permissions; its generic saved-integration requests require full integration
access, even for GET. Veduta offers reviewed route grants but lacks household viewer permissions.
Route grants also do not constrain every meaning of query parameters or request bodies: the
current Prometheus integration may query all accessible series. Neither design guarantees safe
deployment or that all service data is appropriate for every viewer.

### What “extend Homarr” should mean

Start at the cheapest layer that meets the need:

| Layer | Good first work | Advantages | Costs / boundary |
| --- | --- | --- | --- |
| Board configuration and custom widgets | Selected HA sensors, Prometheus overview, media, backup freshness and links to Grafana | Existing auth, layout and administration; no application fork | Learn Homarr's definitions and verify real error, empty and narrow-screen states |
| A focused upstream contribution | A missing native integration, a layout defect, a compact summary or attention feature | Shared maintenance and more users can benefit | Maintainer agreement and upstream conventions; acceptance and timing are unknown |
| Independent service plus a Homarr widget | A durable private journal, if actual users need information existing sources do not retain | Separates a potentially distinctive data product from dashboard rendering; other clients can consume it | Adds a deployment and authenticated read API. Use the simplest existing source first |
| Long-lived Homarr fork | Only a fundamental, repeatedly demonstrated mismatch | Complete control over behavior | Ongoing merge, security, release and migration obligations; poorest default |

Veduta's manifests are not Homarr widget definitions. Port the few useful workflows, with new
tests and permissions, rather than expecting a drop-in adapter. Avoid moving upstream credentials
into templates or exported definitions. Do not expose private aggregate journal content on a
public board merely because its individual sources were previously private.

A widget showing the latest response is not automatically a durable journal. For overnight
incidents, some backend must keep observing and record transitions while every browser is closed.
Homarr has [scheduled integration/widget jobs](https://homarr.dev/docs/management/tasks/); this
review did not verify a generic durable custom-widget event lifecycle. Test that behavior, or use
an existing monitor's history, before proposing a new collector. Retaining the entire Veduta
server as a sidecar just to return a handful of current values duplicates work without clear gain.

## Different pathways

The effort estimates below are relative judgments, not measured project plans. Each pathway can
begin with a bounded trial, and none requires deleting the repository or migrating immediately.

| Path | Near term | Mid term | Longer term | Pros | Cons / choose it when |
| --- | --- | --- | --- | --- | --- |
| **A. Extend Homarr** | Rebuild the desired overview with existing integrations and 1–2 custom widgets | Share useful widgets; contribute specific missing behavior | Maintain a small set of extensions | Fastest likely practical value; shared platform maintenance | Depends on Homarr's UX and contracts. Preferred if a real v2 trial satisfies your daily tasks |
| **B. Keep Veduta as a focused overview** | Fix accepted settings, setup, clipping and real-install gaps | Compact card groups and carefully chosen recipes; links to specialist tools | Stable small app with deliberate scope | Preserves ownership, aesthetic and file workflow; most direct continuation | Large overlap and continued maintenance. Choose if its simplicity or presentation wins the trial |
| **C. Make selective activity the main experiment** | Existing proposal's small Activity card | Add history/filtering only if people use it; one high-value new source | A private journal that can be viewed in Veduta, Homarr or another client | Best opportunity for a recurring workflow beyond metrics | Noise, lifecycle and privacy can dominate; no proven demand. Choose only after the pilot earns it |
| **D. Household / ambient view** | Compare an HA dashboard with a curated Veduta page | Calendar agenda, useful sensors, memories and a tablet-friendly view | Separate audiences and enforced source visibility | Can be useful during healthy weeks; combines practical and enjoyable content | HA and Glance are strong alternatives; calendar and viewer permissions add work. Choose for a specific household routine |
| **E. Operational controls** | Link to HA, Proxmox and container managers | Implement the action proposal for a very small set of repeated tasks | More approved controls only where needed | Reduces context switching | Highest consequential failure cost; upstream tools already execute these actions. Current priority should remain usability |
| **F. Integration / capability platform** | Keep the current contracts and tests maintained | Validate a real third-party need for independently reviewed grants | A supported broker or integration ecosystem | Technically distinctive and reusable in principle | Weak demonstrated demand, distribution and support burden. Least convincing product direction now |

My ordering is **A as the immediate alternative to test; B if Veduta's experience remains a
preference; C as the strongest additional hypothesis**. D is attractive for a household-specific
need. E is a later convenience. F should wait for a real consumer. Maintaining Veduta as a
small personal app is also an honest outcome even if A is better for a broad audience.

## Near term: make a decision and a usable daily view

Think in the next **two to four weeks of focused work**, including a comparison trial. The
durations are provisional planning caps for one developer, not release dates.

### 1. Run a small comparison before new platform work

Cap initial Homarr setup at **two to three developer-days**, then use the chosen page for a week.
Use a separate current-v2 instance rather than migrating an old database as part of the test.
Homarr's [upgrade notes](https://homarr.dev/docs/getting-started/installation/docker/) explain
that a v2 database cannot simply be opened again by v1. No deployment or migration is performed
by this documentation change.

Build the same six jobs in Homarr and Veduta, with an HA/Grafana comparison where appropriate:

1. Open a common service quickly.
2. See host or cluster health without scanning dozens of values.
3. Read a useful network overview and open the relevant Grafana dashboard.
4. Read two or three selected household sensors, including an unavailable one.
5. See an Immich memory or recent Jellyfin content.
6. Check whether the last backup succeeded recently, using a real source if available.

For each, record setup effort, maintenance effort, phone readability, how failures appear, and
which previous v1 annoyance persists. Use the same service permissions where practical. Try
disconnect/reconnect and stale source data, not just attractive healthy screenshots. Record
unavailable workflows as unavailable rather than filling gaps with bespoke development.

**Decision rule:** if Homarr meets these tasks and its editing experience is comfortable, use it
and extend only the remaining concrete gap. If Veduta clearly wins on config workflow, deployment
or readability, continue B. If neither materially helps over an HA dashboard plus Grafana and
bookmarks, stop broad dashboard expansion. Choosing the tool that works now preserves time for
the genuinely interesting experiment.

### 2. If Veduta continues, complete what configuration already promises

Start with title, desktop column count and spacing from [#24](https://github.com/sergeyfarin/veduta/issues/24).
Define which accepted grouping/collapse settings are implemented next or explicitly deferred.
Keep card state, error and freshness visible in any compact form. Address chart/title overflow
in [#20](https://github.com/sergeyfarin/veduta/issues/20); requiring users to discover the correct
span in a reference document is not a sufficient everyday experience.

Verify against a **real configured server**, not only fixture screenshots: desktop and phone,
both presets and schemes, configuration reload, charts, long lists and healthy/error/stale/empty
states. The outcome is a dashboard whose documented settings visibly work and whose important
content stays readable at a practical size. Split these changes into small releasable slices.

### 3. Shorten the path from installation to the first useful card

Offer three concise recipes: **small lab**, **home and media**, and **Prometheus overview**. Each
should name required upstreams, credential provisioning, connection versus browser-facing URL,
manifest source, diff/approval, card size, and expected failure states. Use only sources the
operator already has; a small lab recipe must not require a Prometheus deployment.

Start a guided configuration experience as an **exportable YAML snippet with preview and
validation**. Keep YAML as the source of truth and secrets as references. The existing
`config.Apply` preserves comments for additive importer edits; it is not already a complete
concurrent editor. A later save flow needs conflict detection, full merged-config validation and
an explicit write policy. A second database configuration store would undermine the current model.

Keep CLI approval in this slice. A browser approval interface reverses a deliberate recent UX
decision and needs its own review; [#7](https://github.com/sergeyfarin/veduta/issues/7) also records
a concurrent CLI/API lock-write race. Improve instructions and diagnostics before changing who
can grant authority.

### 4. Turn current Prometheus support into one finished workflow

Validate and release the existing unreleased work through the normal process. Provide an
overview showing WAN traffic, a few busy devices, and an appropriate Wi-Fi summary, using the
actual deployed exporters' labels. Document that instant/range cards choose the first returned
sample/series, and that `percent` values are fractions. Do not make absent metrics look like zero.

Use configured links to Grafana dashboards. Grafana supports
[time ranges in dashboard URLs](https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/manage-dashboard-links/).
Static card links work now; entity/time-aware links are a possible later contract improvement.
Do not assume the current card schema already binds a selected table row into a URL.

Continue the Grafana trial in [#23](https://github.com/sergeyfarin/veduta/issues/23). Floors, rooms,
stable device identity and joining names to traffic are data problems as much as drawing problems.
For [NetAlertX #22](https://github.com/sergeyfarin/veduta/issues/22), first verify the actual API or
exported metrics; add a recipe if Prometheus already has the data, otherwise one narrow read-only
manifest. No new diagram or cross-card query engine is needed for counts and a short list.

### Near-term acceptance

Use a few independent installs to check these goals; the numerical targets are proposed learning
thresholds, not external benchmarks:

- A tester gets three useful cards working in about thirty minutes **after** deployment and
  upstream credentials are ready. Count assistance and abandonment, and record credential/setup
  time separately rather than hiding it.
- A phone user can identify an unhealthy or unknown source and reach the owning tool quickly.
- Editing supported presentation settings changes the UI; invalid edits keep the old generation
  and provide a clear diagnostic.
- Disconnects, restarts and a representative upgrade/restore do not create unexplained healthy
  states or strand an approved integration without understandable instructions.
- The page is voluntarily used for a week and replaces at least one named checking routine.

If these fail, spend the next slice on the observed problem. Extra integrations do not compensate
for activation or trust failures.

## Mid term: make the selected path more useful and interesting

Over the following **one to three months**, spend effort on one recurring workflow at a time.
There are two related tracks: clearer current status, and a small test of useful historical change.
Do not start every pathway in the comparison table.

### Compact groups and progressive detail

If continuing Veduta, [#14](https://github.com/sergeyfarin/veduta/issues/14) is a more practical
presentation investment than new chart types: several related services can read as compact rows,
while each retains its own refresh, authority and execution state. A “Lab” group might show a NAS,
Proxmox node and container service without allocating three large tiles. Homarr's existing
Containers should be tested for the same job before implementing the standalone version.

First group **explicitly configured cards**. Discovery-generated cards bring identity, deletion,
config review and later action-target questions; they are separate work. A collapsed group must
still reveal problems. A compact mode must not silently hide stale data or truncate the only
useful explanation. Follow a short summary with a clear link into the owning tool rather than
trying to fit its administration interface inside a tile.

### Test attention and recovery using proposal 0001

The [Activity proposal](proposals/0001-private-activity-feed.md) already has the right first
experiment: a built-in card, existing status/list blocks, persistent incidents through the rule
manager, no new tables, screen or frozen-contract changes, roughly three developer-days capped
at five. Carry out [#13](https://github.com/sergeyfarin/veduta/issues/13) after usability slices,
then observe ordinary use for four weeks. Start on the developer's install before recruiting the
other two to five testers. Keep its existing stopping rules.

Show what needs attention now, followed by recently resolved incidents. For example: “Immich
could not be reached by Veduta overnight; it recovered after 28 minutes.” That wording avoids
claiming the service itself was unavailable to every client. A card resetting to `pending` after
a configuration change is not a recovery. Rules without `resolve: true` do not have a recorded
historical end; the proposal already describes how to present that limit honestly.

**Do not turn every refresh or metric fluctuation into an entry.** Debounce, exclude sleeping
devices, group related occurrences, retain provenance, and distinguish an old fact from current
status. Read inclusion and push-notification policy should be separate. A quiet period should
have an honest empty state rather than manufactured “insights.” Record actual incident counts so
an incident-free week is not confused with failed demand or failed ingestion.

If the Home Assistant Activity view, existing monitor history, or a Homarr widget over those
records answers the same questions adequately, use that instead. A feed becomes distinctive only
when its selection and lifecycle make useful discoveries across sources easier.

### Prioritise outcomes over raw totals

The next integration should answer a question someone already asks. Suggested order, subject to
the real installation and permissions available:

| Workflow | Useful answer | Smallest initial implementation | Limitation to respect |
| --- | --- | --- | --- |
| Backup freshness | “Did the backup succeed within its expected interval?” | A read-only status/timestamp from the existing backup tool or an existing local status endpoint | A responding backup process is not a successful or restorable backup; a restore drill remains separate |
| Lab / network attention | “Any unreachable expected devices, or new devices I should inspect?” | Existing Prometheus queries or the narrow NetAlertX work | API fetch success does not establish target freshness; expected sleeping devices need exclusions |
| Household exception | “Is the freezer warming, or did a device become unavailable?” | Selected HA sensor cards and a small number of rules | Slow polling misses short transitions; upstream HA already owns device semantics |
| Pleasant daily content | “A photo memory worth seeing, or something recently added to the library” | Improve existing Immich/Jellyfin presentation and sizing | Do not promise format support or thumbnail transforms that remain open in #5 |
| Agenda | “What is coming up today?” | A selected existing HA/calendar source; a core ICS decoder only if needed | Recurrence, cancellation, time zones and privacy are real scope, not just parsing |

The current Prometheus JSON query integration already consumes aggregate/range results; it does
not require a Prometheus text decoder. Prefer established data aggregation upstream over building
joins between arbitrary card outputs. The
[Prometheus API](https://prometheus.io/docs/prometheus/latest/querying/api/) is the integration
boundary; arbitrary Grafana dashboards are not a data extraction contract.

For time-sensitive household events such as “washing machine finished,” do not describe occasional
state polling as a complete event record. Home Assistant offers
[WebSocket event subscriptions](https://developers.home-assistant.io/docs/api/websocket/), but
Veduta does not implement them. Start with an outcome or timestamp retained by the upstream, or
keep the notification in HA. A streaming connector needs reconciliation, reconnect behavior and
its own measured value before it becomes core work.

Calendar/RSS work follows the core-decoder direction in ADR 0005, and
[#12](https://github.com/sergeyfarin/veduta/issues/12) depends on the internet/LAN trust design in
[#11](https://github.com/sergeyfarin/veduta/issues/11). No proposed calendar card should quietly
skip that dependency. The existing weather starter uses the trusted HTTP JSON builtin; it does
not settle the authority model for untrusted mixed-source integrations.

### Gate expansion on evidence

At the end of the mid-term period, record:

- Which page people actually open without being prompted.
- Specific discoveries that saved an extra check, including during ordinary healthy weeks.
- Which updates were noise and whether anyone muted or abandoned the experience.
- Time spent keeping integrations and configuration working.
- Any unexplained missed incident, duplicate, false recovery or privacy failure.

**Keep Activity as a card** if that is enough. Add an Activity screen only when users repeatedly
want history, filters or detail. Continue the larger feed proposal only if its gates pass.
If people prefer Homarr for the page but still want the journal, investigate a small authenticated
read service and Homarr widget then. The journal is the product being tested; renderer independence
is an implementation option that should follow a real second consumer.

## Longer term: choose one earned direction

Use **three to twelve months and beyond** as an exploration horizon, not a promised schedule.

### A stable, intentionally small Veduta

If file-oriented setup and the visual experience attract repeat use, a compact standalone 1.0
could be worthwhile. Concentrate on upgrade/restore behavior, clear supported integrations,
stable config and manifest contracts, understandable grants, and operations on modest hardware.
Document what compatibility actually promises. Execute ADR 0005's 1.0 checkpoint: if no real
additional workload reopens WASM, move Jellyfin to declarative form under ADR 0003's validation
conditions and remove the runtime/SDK rather than stabilising an unused interface.

This can remain a modest personal/community app. A larger audience, cloud service or commercial
business is not required to justify a well-maintained useful tool.

### A private journal of home and lab outcomes

If the Activity pilot succeeds, selected backup results, household completions, calendar reminders
and development results could provide value during healthy weeks. Expand one source at a time
under the proposal's stage gates. Durable identity, occurrence versus receipt time, resolution,
restart/replay behavior, retention, cancellation and historical media lifetimes matter more than
the number of sources.

An optional inbound publisher could accept results from existing automation tools after a separate
authority design. It needs source-bound credentials, revocation, payload/rate limits and duplicate
handling. Keep it private initially; exposing a callback on the internet is a separate deployment
choice. Avoid an automation engine inside Veduta. Begin with deterministic facts and links;
optional summaries must retain their evidence and cannot grant themselves action authority.

This journal might be a Veduta feature, a contribution to another dashboard, or an independent
service with several clients. Do not build all three. A new frontend does not create the missing
data semantics, and moving the same shallow counter cards into a feed does not create a journal.

### A household view with distinct audiences

If family/tablet use becomes the actual pull, compare HA's existing dashboard and Activity again.
Veduta's current shared dashboard lacks per-source household viewer permissions. Calendar,
presence, location, private repository updates and lab administration do not automatically belong
on the same shared screen. Enforce source visibility in API and stream responses before separate
audiences; hiding a card in the browser is insufficient. This would be a new architecture decision,
not an appearance setting. A native mobile app should wait for a clear need beyond a good web UI.

### A few approved actions, when they save repeated work

Pick up [proposal 0002](proposals/0002-action-execution.md) only if users repeatedly leave the
dashboard for a small, recoverable task. Its plan begins with Docker to prove execution, then
manifest actions, with pinned targets and core-owned labels/confirmation. Keep approval binding,
authentication/CSRF, single-flight/cooldown, unknown outcomes and write-ahead audit intact.
Reordering toward HA scenes would require an explicit change to that plan, not silently wiring
the current display-only buttons.

HA scenes/scripts, container lifecycle and a library scan can be conveniences. An SSH terminal,
arbitrary commands, destructive operations or autonomous repair would greatly expand what this
dashboard must be trusted to do. If controls become the dominant need, Homarr, HA and existing
administration tools are likely better foundations than expanding Veduta's scope.

## How this fits the ADRs

| Record | Judgment for this direction |
| --- | --- |
| [0001: Go core](decisions/0001-backend-language.md) | Keep. A rewrite does not address activation or daily usefulness. |
| [0002: curated appearance](decisions/0002-theming-and-visual-customisation.md) | Keep for the standalone path. Improve density and legibility before new appearance controls. |
| [0003: Jellyfin proof case](decisions/0003-jellyfin-wasm-proof-case.md) | Read together with 0005. No near-term rewrite just to simplify a diagram of the stack. |
| [0004: closed Widget Document](decisions/0004-card-expressiveness-and-the-presentation-contract.md) | Keep unless a real unmet presentation need triggers a new decision. Links to Grafana do not require embedding its panels or Mermaid. |
| [0005: WASM frozen](decisions/0005-wasm-frozen.md) | Keep the freeze and eventual checkpoint. Decoders arrive for concrete needs, not as a speculative catalogue. |
| [Activity proposal](proposals/0001-private-activity-feed.md) | Best low-cost distinctive experiment; preserve its cap, exclusions and stopping rules. |
| [Action proposal](proposals/0002-action-execution.md) | Keep deprioritised. Useful later convenience; not the answer to the Homarr overlap. |

Choosing to extend Homarr is a project-direction choice, not permission to change Veduta's
contracts in place. A separate journal or publisher, richer feed contract, config editor, new
audience permissions, or removal of the standalone product needs an explicit follow-up decision.

## Proposed order of work

| Order | Concrete deliverable | Condition / related work |
| --- | --- | --- |
| 1 | Homarr v2 comparison notes covering the six real tasks and the original v1 frustrations | Before broad new investment; keep the existing Veduta instance |
| 2A | A working Homarr board and 1–2 useful custom widgets | If Homarr meets the tasks; upstream contribution only for a demonstrated gap |
| 2B | Veduta title/layout support, honest config contract, readable small charts | If continuing Veduta; #24 and #20 |
| 3B | Three concise setup recipes and one real Prometheus overview; normal release validation | Current unreleased work; verify exporters and #22 rather than assuming APIs |
| 4B | Compact explicit card groups | #14, only if current density is a real pain |
| 5 | The capped Activity test, or an equivalent view over existing source history | #13/proposal 0001; observe four weeks before expansion |
| 6 | A recorded decision: keep card, expand selected activity, use another dashboard, or maintain Veduta for personal preference | Evidence from activation, repeat use, discoveries, noise and upkeep |

Keep network diagrams deferred while Grafana is being tried. Do not spend the next phase on a
marketplace, new WASM SDK, broad chart language, mandatory AI summaries, full config editor, remote
agent or Homepage/Homarr feature parity. Allocate each small development slice to either a known
usability problem or one falsifiable workflow hypothesis.

## Evidence limits and maintenance

The recommendation has **high confidence about current overlap and the cost of duplicated
scope**, and **lower confidence about what people will use repeatedly**. Current project docs
describe intent and behavior; they do not substitute for trying Homarr v2 with your particular
services or for auditing its implementation. Likewise, Veduta's extensive tests do not prove a
ready product, a comparative security advantage, or demand for the grant model.

The specific sources are linked alongside the claims so the comparison can be refreshed. In
addition to the live sources, the review used [getting started](getting-started.md),
[configuration](configuration.md), [Docker deployment](docker.md), [security](security.md),
[migration](migration.md), [integration authoring](integration-authoring.md),
[prior-art review](00-review-and-prior-art.md), [upstream reality check](spikes/s2-upstream-reality-check.md),
[visual prototype](spikes/s4-visual-prototype.md), and the
[archived completion records](archive/README.md). The backlog references are a snapshot of
October 9; issue status and upstream functionality can change.

Refresh this recommendation after the comparison and Activity tests, with raw counts and
concrete examples. Do not treat a proposed feature's existence in this document as evidence that
it should be built.
