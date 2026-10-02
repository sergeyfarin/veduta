# Proposal: a private activity feed, tested alongside the dashboard

Status: proposed 2026-09-30, and revised the same day. The dashboard remains Veduta's product and
its main line of work. The feed is a parallel experiment that begins as a limited test: a built-in
card on the dashboard, used on a handful of existing installs. Only the screen placement below is
settled, and only for the duration of that test. Nothing here is an accepted architecture
decision, a release commitment, or shipped functionality.

## Dashboard first, feed alongside

It is too early to change direction. The dashboard's own hypothesis, that visually rich
third-party integrations can work through a constrained, host-rendered contract, still needs
outside installs, more integrations, and ordinary use before it can be judged. Dashboard work
therefore continues as its [open issues](https://github.com/sergeyfarin/veduta/issues) describe,
and the feed must not delay it. The limited test below is tracked in
[issue #13](https://github.com/sergeyfarin/veduta/issues/13).

The feed runs in parallel under four constraints:

- **Dashboard work has priority.** When the two compete for time, the dashboard wins. Feed work is
  picked up between dashboard slices.
- **Time-boxed.** The immediate plan is about three developer-days, capped at five. Running past
  the cap means stopping to re-plan, not extending.
- **No contract changes.** The limited test changes no frozen contract and no schema: not the
  Widget Document, the manifest, the lock, or the configuration.
- **Removable.** The test adds no table, no screen, and no migration. Removing the card ends the
  experiment.

The two tracks also help each other. The feed's first sources are the dashboard's own rules and
card states, which have little real-use evidence so far. Using the feed exercises them on real
installs, and a problem it exposes is a dashboard bug worth fixing either way.

## Product hypothesis

> The dashboard shows what is true now. The feed shows what happened while nobody was looking,
> and what still needs attention.

A dashboard cannot show an outage that recovered overnight, a disk that crossed a threshold and
fell back, or a service that failed three times last week. Once everything is healthy again, the
grid looks as if nothing happened. The hypothesis is that a short, selective record of those
changes, next to current status, makes the dashboard more useful. Later, the same record could
carry reminders, completed work, and reports from more sources, which would give a reason to look
during healthy weeks as well.

Whether people look at it, whether its entries are useful rather than noise, and whether it
replaces an existing checking habit are all unproven. A feed is not automatically useful because
it looks familiar.

The social-feed analogy is useful for source identities, posts, and source pages. The experiment
needs no public profiles, followers between people, reactions, or social graph. It must provide
value to one person with a few connected services. A household experience is a possible later
direction, conditional on visibility controls.

## Where the feed lives: dashboard, separate screen, or both

Decided 2026-09-30, for the duration of the limited test.

| Option | For | Against |
| --- | --- | --- |
| A separate screen only | Room for a readable column, filters, older history, and detail. | It tests whether people remember to open another screen as much as whether its content is useful, and it needs new UI before there is any evidence. |
| Inside the dashboard only | Seen where people already look. Reuses the grid, the renderer, and the visual system. | A card holds a handful of entries, with no room for filters, older history, or detail. |
| Both | Each surface does what it is good at. | Two surfaces to build and keep consistent. |

**Decision: both eventually, starting inside the dashboard.**

1. **The dashboard stays the home screen and the only current-status view.** A source's current
   state is its card. The earlier idea of a separate "Apps" status view is dropped.
2. **The feed starts as a built-in Activity card.** Its first part lists what needs attention now,
   which replaces the earlier "Today" view. Its second part lists recent, resolved incidents with
   when they started and how long they lasted. The operator decides whether to add the card and
   where to put it. It renders with existing blocks, so it needs no new presentation contract
   ([decision 0004](../decisions/0004-card-expressiveness-and-the-presentation-contract.md)).
3. **A separate Activity screen follows only if the test earns it:** when people use the card and
   want more history, filtering, or detail than a card can hold. It would open from the header
   like the Integrations page, and the card would link to it.
4. **The feed does not become the landing screen.** Revisit that only with evidence from a wider
   pilot.

The test is designed to settle the placement with evidence rather than opinion:

| What testers do | Placement that follows |
| --- | --- |
| Use the card and ask for older entries, filters, or detail | Both: add the Activity screen. |
| Use the card and find it enough | Dashboard only: keep the card as an ordinary dashboard feature. |
| Ignore the card, or find it mostly noise | Neither: remove the card, and the dashboard continues unchanged. |

## Benefits and alternatives

| Potential benefit | What would demonstrate it |
| --- | --- |
| Less manual checking | Users discover useful changes without opening several apps or scanning every card. |
| Context that isolated notifications lose | An incident keeps its history and recovery, next to the source's current state. |
| A reason to return beyond failures | Later sources: reminders, completed work, and optional media are useful during healthy weeks. |
| Consistent, attractive presentation | Different sources share host-designed layouts, typography, spacing, media rules, and states. |
| Reuse of existing engineering | The test builds on the scheduler, rules, events, built-in cards, and typed rendering. |

The feed does not change Veduta's distinction, which remains the constrained extension contract:
integrations supply typed content and request approved upstream operations; the host owns
rendering and credentials. It is flexible for separately installed integrations but deliberately
less expressive than arbitrary frontend components. It is not evidence that Veduta is more secure
than every established dashboard. See [the current rationale](../why-veduta.md).

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
willingness to pay are separate hypotheses; no stage here establishes a commercial market.

## Immediate plan: a limited test

The goal is to learn, cheaply and without slowing the dashboard, whether a record of what happened
is useful next to current status, and whether a card is enough or a screen is needed.

**Who.** The developer's own instance, plus two to five people who already run the dashboard.
These are the same installs that are testing the dashboard, so the only extra cost to a tester is
one card in their configuration and a short weekly note.

**What is built.** Three small slices, about three developer-days in total.

**T1 · Card health incidents · 1 d.** When a card has been `error` or `stale` for longer than a
threshold, an incident opens; when the card is `ok` again, it resolves. The threshold is the
longer of ten minutes and two refresh intervals, so a single failed refresh of a slow card is not
an incident. Reuse the rule manager rather than writing a second transition tracker: it already
persists debounce state across restarts and commits each transition together with its event. One
form is an implicit, core-generated health rule per card with no notification channel. Two
details need care:

- Rules currently require a notification channel, so implicit rules must bypass that validation
  and must never reach the notification outbox.
- The rules' `state()` function reports a missing card as `pending`, not unknown. A card also
  restarts as `pending` whenever its configuration, integration version, approval, or connection
  revision changes. A naive health rule would therefore record a recovery that never happened.

Acceptance:

- A failure shorter than the threshold records nothing.
- A longer failure records exactly one opening and one resolution, including across a restart in
  between.
- A card that moves from `error` or `stale` to `pending` or `disabled` is not reported as
  recovered. Its incident stays open until the card is `ok`, and a removed card's incident closes
  as removed, not recovered.
- Nothing reaches a notification channel.

**T2 · Activity card · 1.5 d.** A built-in `activity` integration, following the Docker and HTTP
JSON built-ins, produces a Widget Document from the core's own records:

- **Needs attention:** open incidents from rules and from T1, each with its card or rule, its
  severity, and how long it has been open. The section is omitted when nothing is open.
- **Recent:** resolved incidents from the last seven days by default, newest first, with start
  time and duration. When nothing happened, the empty state says the period was quiet.

Rule entries need two workarounds. A rule records its end only when it sets `resolve: true`, so
the card reads the current rule state to decide whether a rule's incident is still open; earlier
firings of such a rule have no recorded end and appear without a duration. Rules also have no
title, so an entry shows the rule's ID; adding a title is a configuration schema change that waits
for the test's evidence. Card parameters set the window and exclude cards that are expected to go
offline, such as a laptop that sleeps at night.

Acceptance:

- The card renders with the existing `status` and `list` blocks only.
- An open incident stays under "Needs attention" regardless of age, and moves to "Recent" with its
  duration when resolved.
- An excluded card's incidents disappear from the Activity card but remain recorded.
- The Activity card's own failure does not open an incident about itself.
- Visual baselines cover both themes and mobile width, in the quiet and the busy state.

**T3 · Test kit · 0.5 d.** A documented configuration snippet with the card and two or three
example rules, a short note on what the card does and does not show, and a one-page weekly note
template. There is no telemetry: feed content, repository details, household routines, and
calendar content are never uploaded.

**Then observe for four weeks of ordinary use, fixing defects but adding no feed features.** The
first week runs on the developer's own instance only, and trust problems found there are fixed
before the other testers start. Each tester keeps a short weekly note:

- what the card showed that they had not already learned from the dashboard or notifications;
- entries that were noise, and cards they excluded;
- anything misleading: a missed incident, a false recovery, a duplicate, or a wrong time;
- whether they wanted older entries, filters, or more detail than the card holds.

A short conversation with each tester at the end replaces the earlier plan's separate interview
round.

**Out of scope for the test:** a separate screen; an inbound publisher or webhook endpoint; events
contributed by integrations; reminders and calendars; GitHub, media, fitness, and AI-task sources;
new tables; changes to any schema or frozen contract; changes to push notifications.

**What the test cannot show.** A healthy homelab produces few incidents, so the card will often be
empty. A quiet week is not evidence against the idea. It is evidence that an incident-only feed
gives little reason to look during healthy weeks. Whether reminders, completed work, or reports
create that reason is left to a later stage. Report how many incidents each install produced next
to every judgement about usefulness.

### Deciding after the test

With so few testers, the criteria are qualitative. Agree them before the test starts:

- **Useful discoveries:** most testers can name at least one thing the card told them that neither
  the dashboard nor their notifications did.
- **Noise:** most entries are ones the tester would keep, and nobody stopped looking because of
  volume.
- **Trust:** no missed incident, false recovery, duplicate, or wrong time remains unexplained.
- **Placement:** whether testers wanted more than the card holds.
- **Cost:** the build stayed within its time box, and no dashboard work was deferred for it.

Outcomes:

- **Useful, and the card is enough:** keep the Activity card as an ordinary dashboard feature, and
  stop the feed there.
- **Useful, and testers want more:** proceed to the later stages, starting with the Activity screen.
- **Not useful, or mostly noise:** remove the card and record why. The dashboard continues
  unchanged.
- **Untrustworthy:** fix the cause before anything else. A trust failure blocks expansion however
  useful the card is.

Record the evidence and the decision in this proposal before any later stage starts.

## Later stages, only if the limited test passes

These replace the earlier F0 to F6 plan. Each stage is proposed work with its own gate, and each is
scheduled only after the stage before it passes. Estimates are planning ranges for one developer,
to be split into the repository's usual 0.5 to 2 day slices.

| Stage | Scope | Gate | Estimate |
| --- | --- | --- | --- |
| **L1: Activity screen** | A third view beside the dashboard and Integrations. A feed read model with source filters, cursor pagination, incident revisions, and bounded history. The card links to it. A fixture prototype of richer entry types (reminder, media, report) for the design review. | Testers open it without prompting and do not confuse history, current status, and freshness. The design review passes on desktop and mobile, in both themes. | 3–4 d |
| **L2: Updates from integrations** | Expose the existing `events` capability to the declarative runtime, with occurrence identity, grouping keys, and a publishing policy. The broker attributes each event to the integration whose grant emitted it, so a source cannot claim another's identity. The declarative grammar is a frozen contract, so this needs an accepted decision, schema changes, and adversarial fixtures first. | Ordinary polls create no entries, bursts group correctly, and spoofing and floods are impossible or bounded. | 3–5 d |
| **L3: Reminders from calendars** | Use the core ICS decoder that [decision 0005](../decisions/0005-wasm-frozen.md) already names as the next candidate. Reminders are driven by time, in an explicit time zone, and are cancelled or replaced when the calendar changes. | Rescheduled and cancelled reminders, daylight-saving changes, and restarts behave correctly. | 2–3 d, plus the decoder |
| **L4: Inbound publisher** | Per-source write credentials with provisioning and revocation, validation, and limits; recipes for existing automation tools; GitHub through outbound polling or a signed webhook bridge. This needs its own accepted decision first. | Spoofed sources, revoked credentials, oversized payloads, replay, and floods are denied or bounded. | 5–8 d |
| **L5: Wider pilot** | 10–15 target users over three to four weeks, judged by the criteria below. Recruiting is likely the hardest step, and it is not counted in developer-days. | The criteria below. | 1–2 d of setup and reporting, plus observation |

L2 comes before L4 on purpose. Updates that integrations produce through the broker keep Veduta's
pull-based security model, while an inbound publisher adds an authority the architecture has so far
avoided. The earlier plan gave three to four days to a stage that included the publisher and every
pilot source. That was too low, and the L4 range above covers the publisher alone.

### Candidate sources

| Source | Example entry | Current-status detail | Earliest stage |
| --- | --- | --- | --- |
| Card health | Immich was unreachable from 03:12 to 03:40. | The card itself. | Limited test |
| A rule over a card's signals | The root disk on the coding server stayed above 90% for five minutes. | The card that declares the signal. | Limited test |
| Home Assistant | The washing machine finished. | Selected entities and their freshness. | L2 |
| Homelab backup | Last night's backup failed; the last successful backup was Monday. | Backup freshness, storage, service health. | L2, or L4 for a script |
| Calendar | Anna's birthday is next week; check-in opens tomorrow. | Upcoming dates and travel reminders. | L3 |
| GitHub | A PR has passed checks and received approval. | Selected PRs, checks, and releases. | L4 |
| Scheduled AI task | A weekend itinerary is ready to review. | Last run, task status, and result link. | Exploratory, after L4 |
| Optional media or fitness source | A photo memory or a completed activity. | Recent content and relevant totals. | Exploratory |

The examples describe intended semantics, not integrations already implemented. Travel reminders
can initially come from calendar entries; automatic booking discovery and trip planning are later
possibilities.

### Design notes for the later stages

Proposed flow: **approved polling (or, after L4, authenticated publishing) → validated occurrence
→ selection and grouping → durable feed item → Activity card and screen**. Push notification is a
separate policy decision after persistence. Operational and audit records remain available
independently, so system diagnostics do not become user-facing entries.

Use Go and SQLite. Introduce a small feed service and additive storage migrations, not a
distributed event platform. Scheduling is keyed by card, so sources that produce updates without a
visible card need a scheduler change; make it when a real case requires it.

The contract design must distinguish provider identity, core-validated source identity, occurrence
ID, occurrence and receipt times, type, grouping key, lifecycle, due and expiry times, and typed
content. Retain the content observed when an update was created: reading an old entry must not
substitute today's card document. Changes such as recovery are explicit revisions of the same item.
The host owns provenance, permission checks, and freshness. A machine publisher receives only the
authority to write its approved source, not to read the aggregated feed or any credential.

This introduces event metadata, not a second presentation language. Review the source and output
contract against decision 0004 before implementation. Any required change to a frozen contract
needs a separately accepted decision, schema, and adversarial fixtures. Action blocks remain
display-only under the existing [0.2 decision](https://github.com/sergeyfarin/veduta/issues/2).

The later stages do not require public sharing, household permissions, autonomous service actions,
a general workflow editor, arbitrary frontend plugins, automatic travel-booking extraction, native
mobile apps, AI ranking, or mandatory AI-generated summaries. Existing automation tools can supply
selected facts and reports.

### External services

Cloud webhook producers need a reachable callback. For a private homelab, begin with local
publishers or outbound polling; any public ingress or relay needs an explicit deployment design.
Use upstream signatures when ingesting provider webhooks, rather than assuming a browser session
authenticates their origin. GitHub documents that failed deliveries are not automatically retried,
so reconciliation matters
([delivery guidance](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries)).

Official OpenAI documentation says supported scheduled tasks can use connected tools and plugins
([scheduled tasks](https://learn.chatgpt.com/docs/automations)). A Veduta publishing tool is a
candidate to test with actual account availability, connectivity, and action permissions. A
general export stream for all ChatGPT task results has not been verified. OpenAI API completion
webhooks support a separate path for API jobs that the operator runs
([API webhooks](https://developers.openai.com/api/docs/guides/webhooks)). No stage may depend on a
universal native export of ChatGPT task results, and a limitation found there must be reported
rather than replaced by an undocumented scraping path.

Strava supports [webhooks](https://developers.strava.com/docs/webhooks/), but application capacity
and [rate limits](https://developers.strava.com/docs/rate-limits/) affect distribution. A working
personal integration would not by itself establish easy onboarding for a wider audience.

## Presentation

Beauty is an acceptance requirement. In the limited test, the Activity card must meet the
dashboard's existing bar: it uses existing blocks and passes the same visual baselines. The current
visual baseline demonstrates consistency but does not establish a distinctive feed aesthetic. That
becomes a requirement for the screen in L1, assessed with realistic, licensed fixture media and
realistic content lengths rather than placeholder posters.

For the screen, the host should own a shared entry header and a small family of layouts: incident,
reminder, milestone, media, and report. Reuse the existing block vocabulary, with a feed-specific
shell, a readable column width, clear hierarchy, bounded previews, and expandable detail. Sources
supply content and semantic hints; they do not supply CSS, HTML, or scripts. Review mobile and
desktop, light and dark themes, keyboard access, contrast, and empty, stale, error, and
long-content states. Visual acceptance needs human review, because the
[current pixel threshold has known gaps](https://github.com/sergeyfarin/veduta/issues/4).

## Publishing policy

An entry represents a meaningful occurrence, not every poll or changed value. Start with
deterministic rules and templates; AI summarisation is optional later work.

- Group repeated failures into one incident and attach recovery to it.
- Debounce noisy state changes and give each occurrence a stable identity.
- Default routine successes to summaries; show failures promptly.
- Keep optional enjoyable updates separate from urgent attention, and let them be muted
  independently.
- Schedule reminders in an explicit time zone; cancel or replace them when the source changes.
- Expire obsolete entries, and let users mute a source or event type.
- Record each entry's origin and whether it is machine-generated; keep links to supporting data.

The limited test covers the first two through rule debounce and the health threshold, and expires
entries through the existing 30-day event retention. The rest apply from L1 onwards. For example,
thirty restarts become one incident with detail, a daily backup success need not make a daily
entry, and a reminder is produced by time passing even if no source value changes.

## Risks and limits

| Risk | Consequence | Mitigation or experiment |
| --- | --- | --- |
| Diverting effort from the dashboard | The feed slows the product it is meant to complement. | Dashboard priority, a five-day cap on the limited test, and no later stage without a recorded decision. |
| A quiet test period | Few incidents leave the test inconclusive. | Report incident counts with every judgement, and test healthy-week value separately in later stages. |
| Notification overload | Users mute the feed or stop checking it. | Selective defaults, grouping, cooldowns, summaries, exclusions, and noise feedback. |
| Too little recurring value | The feed becomes a one-time configuration exercise. | Test voluntary use during ordinary weeks, and keep the card only if testers name real discoveries. |
| Misleading silence or old information | Missing events or stale status look healthy; old entries appear current. | Separate occurrence time, observation time, and source freshness. A card resetting to pending is not a recovery. |
| Delivery failures and duplicates | Important updates disappear or recur. | In the test, the rule manager's persisted transitions. Later, durable ingestion, idempotency, checkpoints, reconciliation, and restart and replay tests. |
| Integration upkeep | API changes, OAuth, and upstream restrictions dominate development. | A few supported sources, added one stage at a time. |
| Setup friction | The interested audience never reaches useful updates. | One card and a documented snippet in the test; guided setup and recorded abandonment reasons later. |
| Sensitive aggregation | A shared screen exposes plans, routines, private repositories, or location. | In the test, the card shows what the dashboard already shows, plus when it happened, to the same viewers. Timing alone can reveal routines, and testers should know that. Richer shared sources wait for enforced source visibility. |
| New inbound authority | A publisher spoofs another source, floods storage, or embeds hostile content. | Deferred to L4 behind its own decision: per-source write credentials, fixed validation, payload and rate limits, trusted source attribution, and denial tests. |
| Historical media and data retention | Expired asset tokens break old entries; history grows or outlives consent. | Explicit retention and media lifetime policies, deletion, and honest unavailable-media states. |
| Unreliable AI interpretation | An invented explanation or urgency undermines trust. | Deterministic facts first; later summaries retain evidence and cannot execute service actions. |
| Scope expansion | Building a universal life assistant prevents testing the core hypothesis. | The time box, a staged source list, explicit exclusions, and a decision before each stage. |
| Weak differentiation or willingness to pay | Existing tools satisfy the need more cheaply. | Ask which routine it replaces, and evaluate payment demand separately after retention is demonstrated. |

## Current foundations

| What exists | Use in the limited test | What later stages need |
| --- | --- | --- |
| The [scheduler](../../internal/scheduler/scheduler.go), keyed by card, with `ok`, `stale`, `error`, `pending`, and `disabled` states. A state survives a restart only while the card's configuration, integration, approval, and connection revisions are unchanged. | T1 derives health incidents from state changes. | Sources without a visible card need a scheduler change. |
| The [rule manager](../../internal/rules/manager.go): persisted debounce, with each fired or resolved transition committed together with its event and notifications. | T1 reuses it, and T2 reads rule state. | Rules require a notification channel and record their end only with `resolve: true`. Recording without pushing needs a configuration change. |
| The [events store](../../internal/storage/events.go) and [recent-events endpoint](../../internal/api/events.go), kept for 30 days. Only rule transitions write to it today, and the frontend does not read it. | T2 reads the store. | A feed read model with source filters, cursor pagination, lifecycle, and bounded history. |
| The `events` capability: present in the manifest and lock schemas, and implemented in the broker with a host-call budget. No runtime exposes it to integrations. | Not used. | L2 wires it into the declarative runtime. |
| Built-in cards for Docker and HTTP JSON in the [app runtime](../../internal/app/runtime.go). | T2 follows the same pattern. | None. |
| [Widget Document](../../internal/widgets/document.go) `status` and `list` blocks, with levels, times, links, and an empty state. | T2 renders with them. | A feed shell, and a policy for historical content and media. |
| An app shell with two views, dashboard and Integrations, switched by the URL hash. | Unchanged. | L1 adds a third view. |
| The [notification outbox](../../internal/storage/notifications.go), with cooldown and hourly limits. | Unchanged; T1's incidents never reach it. | Feed inclusion kept separate from push policy. |
| Authentication, grants, and the credential-owning broker. | The card has the dashboard's audience. | Source-bound publisher authority, and viewer authorisation for household use. |

## Criteria for the wider pilot

These apply to L5 only. Agree the measures before recruitment. They are proposed directional
thresholds for a small pilot, not statistically validated benchmarks: with 10–15 participants, one
person moves a percentage by seven to ten points.

- **Activation:** at least 70% of recruits obtain useful updates from three sources within a week.
  Record assisted setup separately, and include abandoned installs in the denominator.
- **Return use:** at least 60% of activated participants voluntarily return in the final week,
  outside research sessions and developer prompts. Record visit frequency without demanding daily
  use from sources that update weekly.
- **Utility:** at least half of activated participants identify two concrete useful discoveries
  during the pilot and describe an existing checking habit it replaces or reduces.
- **Noise:** among explicitly rated updates, at least 70% are useful or welcome. Report rating
  coverage, mute reasons, and participants overwhelmed by volume; unrated items are not successes.
- **Presentation and trust:** at least 70% rate visual quality at least 4 out of 5, and observed
  tasks reveal no unresolved confusion between historical facts, current status, and source
  freshness.
- **Reliability and privacy:** no unresolved cross-source authority or unauthorised disclosure
  failure, and controlled replay and restart drills produce no lost or duplicated feed occurrences.

Collect feedback through participant diaries, interviews, and opt-in local measurements. Do not
upload private feed bodies, repository details, household routines, or calendar content as
telemetry. Report raw counts alongside percentages, and separate actual source failures from an
absence of activity. Small-cohort results guide the next experiment; they do not estimate market
size or prove willingness to pay.

Expand only when activation, return use, utility, noise, and presentation meet the thresholds and
the reliability and trust gates pass. If utility is strong but a source or onboarding path fails,
narrow the scope and repeat that part. If people enjoy the appearance but do not return or cannot
name useful discoveries, stop feed expansion and keep the dashboard as it is. Unresolved privacy or
authority failures block a wider pilot regardless of engagement. Record the evidence and decision
in this proposal, or in a follow-up decision document, before changing the default product
direction.
