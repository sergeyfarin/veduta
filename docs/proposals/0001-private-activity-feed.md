# Proposal: a private activity feed for home and services

Status: proposed, 2026-09-30. This is a product experiment, not an accepted architecture decision,
a release commitment, or a description of shipped functionality. The stages below are candidates
for scheduling after review; they do not supersede the current implementation plan.

## Product hypothesis

> Veduta gives you a beautiful, private place to catch up on meaningful changes across your home
> and services, and inspect their current state when you need it.

The current dashboard asks people to scan cards and interpret values. The proposed experience
answers three questions: what changed, what needs attention, and what is coming up? Applications,
devices, automations, and scheduled tasks supply updates. Selecting a source opens its current
status and recent history.

The social-feed analogy is useful for source identities, following, posts, and source pages. The
first experiment needs no public profiles, followers between people, reactions, or social graph.
It must provide value to one person with a few connected services. A household experience is a
possible later direction, conditional on visibility controls.

The hypothesis is that selective updates plus current status provide more recurring value than a
grid alone. Whether people return, whether setup is tolerable, and whether the updates replace an
existing checking habit are unproven. A feed is not automatically useful because it looks familiar.

## Benefits and alternatives

| Potential benefit | What would demonstrate it |
| --- | --- |
| Less manual checking | Users discover useful changes without opening several apps or scanning every card. |
| Better context than isolated notifications | An incident retains its history and recovery; users can inspect the source's current state. |
| A reason to return beyond service failures | Upcoming reminders, completed work, and optional media updates are useful during healthy operation. |
| Consistent, attractive integrations | Different sources use host-designed layouts and the same typography, spacing, media rules, and states. |
| A clearer product distinction | Users choose Veduta for catching up across services, rather than expecting another widget catalogue. |
| Reuse of existing engineering | The trial builds on the broker, typed rendering, scheduler, rules, events, and storage. |

Veduta's current distinction is a constrained extension contract: integrations supply typed
content and request approved upstream operations; the host owns rendering and credentials. It is
flexible for separately installed integrations but deliberately less expressive than arbitrary
frontend components. It is not evidence that Veduta is more secure than every established
dashboard. See [the current rationale](../why-veduta.md) and
[presentation decision 0004](../decisions/0004-card-expressiveness-and-the-presentation-contract.md).

Adjacent products already cover substantial parts of this idea:

| Alternative | Existing approach | What the experiment must add |
| --- | --- | --- |
| [Homepage](https://gethomepage.dev/) and [Glance](https://github.com/glanceapp/glance) | Service dashboards; Glance also presents content feeds. | Selected changes, persistent context, and current status in one experience. |
| [Home Assistant Activity](https://www.home-assistant.io/integrations/logbook/) | A chronological history of household changes. | Useful selection across home, development, and personal sources. |
| [ntfy](https://docs.ntfy.sh/) | Notifications published by scripts and services. | A richer destination for reviewing updates and inspecting their sources. |
| [Huginn](https://github.com/huginn/huginn) and [n8n](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.webhook/) | Event processing and automation. | A polished destination for their output, with restrained publishing policies. |
| [Tapestry](https://usetapestry.com/) and [Reeder](https://reeder.app/) | Unified timelines across content sources. | Private service events, reminders, and current operational state. |

These project-owned sources were reviewed on 2026-09-30. The gap described here is an inference,
not a claim that no competitor offers it. Existing products establish that the ingredients are
familiar; they do not establish demand for this combination. The practical alternative is also
the user's existing notifications, calendar, and occasional dashboard visits.

The first audience should be homelab owners, Home Assistant users, and developers who already
maintain several services or automations. Broader personal-life adoption would need easier
onboarding, more service coverage, and stronger privacy controls. Open-source interest and
willingness to pay are separate hypotheses; this pilot does not establish a commercial market.

## Proposed experience

Three connected views serve different purposes:

- **Today:** unresolved attention items and upcoming deadlines, with a finite catch-up summary.
- **Feed:** chronological updates, grouped by related occurrence, with source and type filters.
- **Apps:** each source's current status, freshness, available links, and recent updates. Existing
  dashboard cards can supply status; a source can also explicitly report that status is unavailable.

Unresolved attention stays visible in Today even when newer posts arrive. Reading or dismissing
an item does not resolve the underlying incident. A quiet feed must distinguish healthy sources
from sources that have stopped reporting.

| Source | Example update | Current-status detail |
| --- | --- | --- |
| Homelab | Last night's backup failed; last successful backup was Monday. | Backup freshness, storage, service health. |
| GitHub | A PR has passed checks and received approval. | Selected PRs, checks, and releases. |
| Home Assistant | The washing machine finished. | Selected entities and their freshness. |
| Calendar | Anna's birthday is next week; check-in opens tomorrow. | Upcoming dates and travel reminders. |
| Scheduled AI task | A weekend itinerary is ready to review. | Last run, task status, and result link. |
| Optional media or fitness source | A photo memory or a completed activity. | Recent content and relevant totals. |

The examples describe intended semantics, not integrations already implemented. Travel reminders
can initially come from calendar entries; automatic booking discovery and trip planning are later
possibilities. Strava and other fitness sources are outside the initial required set.

### Beauty and consistency

Beauty is an acceptance requirement. The current visual baseline demonstrates consistency but
does not yet establish a distinctive feed aesthetic. Use realistic, licensed fixture media and
realistic content lengths to assess it, rather than relying on placeholder posters.

The host should own a shared post header and a small family of layouts: incident, reminder,
milestone, media, and report. Reuse the existing block vocabulary, with a feed-specific shell,
readable column width, clear hierarchy, bounded previews, and expandable detail. Sources supply
content and semantic hints; they do not supply CSS, HTML, or scripts. Review mobile and desktop,
light and dark themes, keyboard access, contrast, and empty, stale, error, and long-content states.

### Publishing policy

An update represents a meaningful occurrence, not every poll or changed value. Start with
deterministic rules and templates; AI summarisation is optional later work.

- Group repeated failures into one incident and attach recovery to it.
- Debounce noisy state changes and define a stable identity for each occurrence.
- Default routine successes to summaries; show failures promptly.
- Keep optional enjoyable updates separate from urgent attention; allow them to be muted independently.
- Schedule reminders using an explicit timezone; cancel or replace them when the source changes.
- Expire obsolete updates and allow users to mute a source or event type.
- Record the origin and whether content is machine-generated; preserve links to supporting data.

For example, thirty restarts become one incident with detail. A daily backup success need not make
a daily post. A reminder is produced by time passing even if no source value changes.

## Risks and limits

| Risk | Consequence | Mitigation or experiment |
| --- | --- | --- |
| Notification overload | Users mute the feed or stop checking it. | Selective defaults, grouping, cooldowns, summaries, and usefulness/noise feedback. |
| Too little recurring value | A beautiful feed becomes a one-time configuration exercise. | Test voluntary return visits and discoveries during ordinary healthy weeks. |
| Integration upkeep | API changes, OAuth, and upstream restrictions dominate development. | A few supported native sources plus a constrained publisher for existing automation tools. |
| Setup friction | The interested audience never reaches useful updates. | Guided examples, setup timing, source-health feedback, and recorded abandonment reasons. |
| Misleading silence or old information | Missing events or stale status look healthy; old posts appear current. | Separate occurrence time, observation time, and source freshness; visibly report disconnected sources. |
| Delivery failures and duplicates | Important updates disappear or recur. | Durable ingestion, idempotency, checkpoints, reconciliation, and restart/replay tests. |
| Sensitive aggregation | A shared screen exposes plans, routines, private repos, or location. | Pilot with one authorised viewer per instance; shared use waits for enforced source visibility. |
| New inbound authority | A publisher spoofs another source, floods storage, or embeds hostile content. | Per-source write credentials, fixed validation, payload/rate limits, trusted source attribution, and denial tests. |
| Historical media and data retention | Expired asset tokens break old posts; history grows or outlives consent. | Explicit retention and media lifetime policies, deletion, and honest unavailable-media states. |
| Unreliable AI interpretation | An invented explanation or urgency undermines trust. | Deterministic facts first; later summaries retain evidence and cannot execute service actions. |
| Scope expansion | Building a universal life assistant prevents testing the core hypothesis. | Feature gate, a limited source set, explicit exclusions, and a pilot decision before expansion. |
| Weak differentiation or willingness to pay | Existing tools satisfy the need more cheaply. | Ask which routine it replaces and evaluate payment demand separately after retention is demonstrated. |

Cloud webhook producers need a reachable callback. For a private homelab, begin with local
publishers or outbound polling; any public ingress or relay needs an explicit deployment design.
Use upstream signatures when ingesting provider webhooks, rather than assuming a browser session
authenticates their origin. GitHub documents that failed deliveries are not automatically retried;
reconciliation matters ([delivery guidance](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries)).

Official OpenAI documentation says supported scheduled tasks can use connected tools and plugins
([scheduled tasks](https://learn.chatgpt.com/docs/automations)). A Veduta publishing tool is a
candidate to test with actual account availability, connectivity, and action permissions. A
general export stream for all ChatGPT task results has not been verified. OpenAI API completion
webhooks support a separate path for API jobs that the operator runs
([API webhooks](https://developers.openai.com/api/docs/guides/webhooks)). The pilot must not depend
on universal native ChatGPT task export.

Strava supports [webhooks](https://developers.strava.com/docs/webhooks/), but application capacity
and [rate limits](https://developers.strava.com/docs/rate-limits/) affect distribution. A working
personal integration would not by itself establish easy onboarding for a wider audience.

## Current foundations and required changes

The repository has foundations, not a finished feed:

| Existing implementation | Required experiment change |
| --- | --- |
| [Widget Document](../../internal/widgets/document.go), closed renderer, asset proxy | A host-owned feed shell using the same typed content; historical content and media policy. |
| [Scheduler](../../internal/scheduler/scheduler.go) keyed by cards | Sources that can produce updates without a visible dashboard card; reuse approved invocation identities. |
| [Rule manager](../../internal/rules/manager.go) with fired/resolved transitions | Curated feed projection, grouping, stable occurrence identity, and time-driven reminders. |
| [Events store](../../internal/storage/events.go) and [recent-events endpoint](../../internal/api/events.go) | A feed read model with source filters, cursor pagination, lifecycle, and bounded history. |
| [Notification outbox](../../internal/storage/notifications.go) with cooldown/rate controls | Separate feed inclusion from push policy; preserve transaction and retry guarantees. |
| YAML configuration and SQLite runtime state | Optional experiment/source policy configuration; viewer state in SQLite, not configuration files. |
| Authentication, grants, and credential-owning broker | Source-bound publisher authority and viewer authorisation on reads and preference writes. |

Proposed flow: **approved polling or authenticated publishing → validated occurrence → selection
and grouping → durable feed item → Today, Feed, and Apps**. Push notification is a separate policy
decision after persistence. Operational and audit records remain available independently; exposing
the existing events table directly as a feed would mix system diagnostics with user-facing updates.

Use Go and SQLite for the trial. Introduce a small feed service and additive storage migrations,
not a distributed event platform. Prototype source independence with an explicit mapping to
existing approved operations; avoid a broad scheduler rewrite until a real case requires it.

The contract design must distinguish provider identity, core-validated source identity, occurrence
ID, occurrence/receipt times, type, grouping key, lifecycle, due/expiry times, and typed content.
Retain the content observed when an update was created; reading an old post must not substitute
today's card document. Changes such as recovery are explicit revisions attached to the same item.
The host owns provenance, permission checks, and freshness. A machine publisher receives only
authority to write its approved source, not to read the aggregated feed or credentials.

This introduces event metadata, not a second presentation language. Review the source/output
contract against decision 0004 before implementation. Any required change to a frozen contract
needs a separately accepted decision, schema, and adversarial fixtures. Action blocks remain
display-only under the existing [0.2 decision](../03-backlog.md#action-execution-is-not-implemented).

## Staged plan to test the idea

Each stage is proposed work. Estimates are planning ranges for one developer, excluding waiting
for participants or service access. Split implementation stages into the repository's usual
0.5–2 day slices. Stop or revise at a gate before spending on the next stage.

| Stage | Dependencies and changes | Acceptance and evidence | Estimate |
| --- | --- | --- | --- |
| **F0: validate the experience** | No backend dependency. Build an interactive fixture prototype of Today, Feed, and an app detail view. Include an incident and recovery, reminder, media item, report, quiet day, and disconnected source. Interview five target users about their current checking habits. | Users can explain the difference between history and current status, find an unresolved issue and upcoming reminder, and identify useful examples. Record visual feedback on desktop/mobile and both themes. Revise or stop if the experience offers no benefit over existing tools. | 2–3 d |
| **F1: define the experiment contract** | F0. Write source, occurrence, feed-item, publisher, and viewer-state contracts; settle status mapping, transaction boundaries, retention, media lifetime, timezone behaviour, and authorisation. Specify an opt-in feature gate and pilot configuration examples. | Contract review against broker/presentation decisions; schemas and negative fixtures for required boundaries. Every field has an owner, and source removal/revocation behaviour is explicit. | 1–2 d |
| **F2: durable feed foundation** | F1. Add storage/read model, migrations, idempotent ingestion, incident revisions, cursor queries, retention/deletion, and an authenticated read API. Connect selected existing rule transitions. | Restarts and duplicate replay preserve one occurrence; recovery updates its incident; denied reads expose nothing; pagination is stable with concurrent arrivals; expiry and deletion are bounded. | 3–4 d |
| **F3: useful publishing policies** | F2. Add source mapping, deterministic change selection, debounce/grouping, source-health reporting, and a reminder scheduler. Begin with Home Assistant sensor transitions and a homelab/backup publisher recipe. | Ordinary polls do not create posts; a burst groups correctly; unknown/stale state is not a recovery; rescheduled/cancelled reminders and daylight-saving transitions behave correctly across restart. | 2–3 d |
| **F4: production experiment UI** | F2 and F3, informed by F0. Implement Today, Feed, Apps detail, filters, mute, read/acknowledge state, expandable content, and reconnection recovery. Keep the dashboard available and the experiment opt-in. | Visual and accessibility review meets the requirements above; recovery does not hide history; reading does not resolve incidents; source health stays visible; disabling the gate restores the existing experience. | 3–4 d |
| **F5: connect pilot sources** | F1–F4. Add a source-bound publisher with credential provisioning/revocation, limits, and automation recipes. Supply GitHub updates through a small outbound-polling adapter or explicit webhook bridge; calendar reminders through a documented local publisher. Attempt one real scheduled-task publishing workflow. | GitHub, selected Home Assistant events, a homelab/backup source, calendar reminders, and a generic publisher work with real inputs. Spoofed sources, revoked credentials, oversized payloads, replay, and floods are denied or bounded. Record the actual AI-task integration result and any limitation. | 3–4 d |
| **F6: limited pilot and decision** | F5. Recruit 10–15 target users, record setup and baseline habits, observe several ordinary weeks, collect useful/noisy feedback, and write a decision report. | Evaluate the predeclared measures below. Decide whether to expand, narrow, revise once, or stop. No automatic promotion to the default experience. | 1–2 d setup/reporting + 3–4 weeks observation |

Indicative effort: 15–22 developer-days plus pilot observation. This is an uncertainty range, not
a delivery date; API access and contract changes can move it. F0 is the first investment decision,
not a justification to implement every later stage.

Before the pilot, use meaningful tests for lifecycle, delivery, permission, and scheduling
failures; exercise real upstream payloads as well as fixtures. Run the normal repository checks
and relevant frontend/visual suites. Verify feed retention and media behaviour over an accelerated
history window, and check a noisy source does not exhaust memory or disk. Visual acceptance needs
human review because the [current pixel threshold has known gaps](../03-backlog.md#the-visual-baselines-per-pixel-threshold-hides-whole-area-changes).

### Scope boundaries for the trial

The pilot covers one authorised viewer per instance. It does not require public sharing,
household permissions, autonomous service actions, a general workflow editor, arbitrary frontend
plugins, automatic travel-booking extraction, Strava, native mobile apps, AI ranking, or mandatory
AI-generated summaries. Existing automation tools can supply selected facts and reports. An AI
publishing limitation must be reported rather than replaced by an undocumented scraping path.

## Validation and decision criteria

Agree the measures before recruitment. The following are proposed directional thresholds for a
small pilot, not statistically validated market benchmarks:

- **Activation:** at least 70% of recruits obtain useful updates from three sources within a
  week; record assisted setup separately and include abandoned installs in the denominator.
- **Return use:** at least 60% of activated participants voluntarily return in the final week,
  outside research sessions and developer prompts. Record visit frequency without demanding daily
  use from sources that update weekly.
- **Utility:** at least half of activated participants identify two concrete useful discoveries
  during the pilot and describe an existing checking habit it replaces or reduces.
- **Noise:** among explicitly rated updates, at least 70% are useful or welcome. Report rating
  coverage, mute reasons, and participants overwhelmed by volume; unrated items are not successes.
- **Presentation and trust:** at least 70% rate visual quality at least 4/5, and observed tasks
  reveal no unresolved confusion between historical facts, current status, and source freshness.
- **Reliability and privacy:** no unresolved cross-source authority or unauthorised disclosure
  failure; controlled replay/restart drills produce no lost or duplicated feed occurrences.

Collect feedback through opt-in, local measurements or participant diaries and interviews. Do not
upload private feed bodies, repository details, household routines, or calendar content as
telemetry. Report raw counts alongside percentages and separate actual source failures from
absence of activity. Small-cohort results guide the next experiment; they do not estimate market
size or prove willingness to pay.

Expand only when activation, return use, utility, noise, and presentation meet the thresholds and
the reliability and trust gates pass. If utility is strong but a source or onboarding path fails,
narrow the scope and repeat that part. If people enjoy the appearance but do not return or cannot name useful
discoveries, stop feed expansion and retain the dashboard. Unresolved privacy/authority failures
block a wider pilot regardless of engagement. Record the evidence and decision in this proposal
or a follow-up decision document before changing the default product direction.
