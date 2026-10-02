# Proposal: executing actions

Status: **deprioritised future direction**, 2026-10-02, for
[issue #2](https://github.com/sergeyfarin/veduta/issues/2). This is the design that issue asks
for before any code. Its six decisions were settled the same day (see [Decisions](#decisions)),
and the design was then deliberately put aside. Work goes first to making the dashboard usable,
good-looking, and easy to configure. Nothing here is implemented or scheduled. Action controls stay
display-only, and documentation keeps describing them that way, until this is picked up again.

## Where things stand

The Widget Document has an `actions` block. Each button carries `id`, `label`, `icon`, `confirm`
and `danger`, and the schema says the ids reference "actions declared in configuration". The
frontend renders every button disabled ([ActionsBlock.svelte](../../web/src/lib/blocks/ActionsBlock.svelte)).
Docker connections have an `allowActions` flag that nothing reads.

Two problems in that contract have to be fixed before any button is enabled:

1. **No configuration declares actions.** An id in a document points at nothing. An integration
   also cannot know which ids an administrator configured, so it has no correct value to emit.
2. **The untrusted party decides how dangerous a button looks.** `label`, `confirm` and `danger`
   come from integration output, which §8 of the architecture treats as hostile. An integration
   could emit a button labelled "Refresh" with `confirm: false` that is wired to a restart. If
   enabling buttons kept that contract, the integration would decide whether a person is asked
   before something consequential happens.

Everything already built and reusable: server-side sessions with double-submit CSRF and
`SameSite=Strict` cookies, the sudo window, the audit log, the capability broker with its route
authorisation, the connection revision used by asset tokens, and the two-step digest-bound
approval transaction. The design below adds as little as it can on top of these.

## What belongs in a home dashboard

An action is in scope if the person would otherwise open the service's own UI to do one routine
thing, and a wrong click is recoverable without data loss.

| In scope | Out of scope, by policy |
| --- | --- |
| Restart, start or stop a container | Delete, prune, remove or purge anything |
| Start, shut down or reboot a VM or LXC | Arbitrary commands, shells, SSH, scripts given as text |
| Run a Home Assistant script or scene that already exists | Creating or editing upstream configuration |
| Start a library scan or metadata refresh | Credential, user or permission management |
| Pause or resume a download queue | Anything that takes free-form input from the viewer |
| Wake-on-LAN through an upstream that offers it | Updates, upgrades or image pulls |

Each in-scope action has one of two **effect classes**, declared by whoever defines the action
and shown at approval:

- `routine`: idempotent or trivially undone, such as a library scan or a scene. No confirmation.
- `disruptive`: interrupts a service someone may be using, such as a restart or a shutdown.
  Always confirmed.

There is no `destructive` class. Veduta cannot see what an upstream `POST` actually does, so the
out-of-scope list is a review rule for first-party manifests and a disclosed limit for third-party
ones, not an enforced property. That is the same honesty the route model already uses for
[body-selected operations](../01-architecture.md#what-a-route-grant-cannot-express).

## The design

### Three parties must agree, as with routes

An action executes only if all three of these allow it, and each is checked independently at
execution time:

1. **The manifest declares it**: id, title, effect class, typed parameters and exactly one
   request. This is a request for authority, like a route.
2. **The lock approves its route.** An action's request must match a manifest route marked
   `use: action`, and that route is approved like any other. The lock gains no new section:
   approval already records routes, and the manifest digest already covers the action
   declaration, so adding an action, or changing its effect class, forces re-approval. The
   approval diff lists `use: action` routes first with their action's effect class, and they need
   the explicit keystroke that non-`GET` routes already need.
3. **The card configuration enables it**, with its parameters pinned:

```yaml
cards:
  - id: containers
    integration: docker
    operation: containers
    slots: { server: docker-local }
    actions:
      - id: restart-jellyfin       # unique within the card
        action: restart            # the manifest's (or builtin's) action id
        label: Restart Jellyfin    # optional; defaults to the manifest title
        params: { container: jellyfin }
```

The builtin `docker` integration has no manifest or lock. Its actions (`start`, `stop`,
`restart`) are compiled in, and its gates are the card configuration plus the connection's
existing `allowActions: true`. The documented socket proxy is read-only by default. Enabling
Docker actions also means allowing `POST` on the proxy, and the docs must say so.

WASM integrations do not get actions. [Decision 0005](../decisions/0005-wasm-frozen.md) forbids
new host functions or ABI changes, and actions are not a format a manifest cannot read.

### The core renders action buttons

Executable buttons come from configuration and the manifest, not from the document. The core
adds a core-owned `actions` array to the CardState envelope, next to `execution`:

```json
"actions": [
  { "id": "restart-jellyfin", "label": "Restart Jellyfin", "icon": "mdi:restart",
    "effect": "disruptive", "available": true, "binding": "b64url…" }
]
```

`available: false` carries a `reason`, for example a lock mismatch, a disabled connection or
`allowActions: false`. The frontend renders this list as a row at the foot of the card. Labels,
effect and confirmation all come from administrator- or approval-controlled sources, so the
document has no say in them.

The document's `actions` block stays in Widget Document v1 unchanged, so no frozen contract
breaks. It stays permanently display-only and is documented as presentation, then removed at the
next `apiVersion`.

### Execution is one bound request

```
POST /api/v1/cards/{card}/actions/{action}
     { binding }
  → 200 { outcome: "succeeded" | "failed" | "unknown", status?, message, auditId }
  → 409 { binding }   the action changed since the card state was rendered; nothing ran
```

**Approval binding.** `binding` is a SHA-256 digest over: card id, action id, integration id and
manifest digest, the approved `use: action` route, the connection id and its **revision**, and the
resolved parameters. The server recomputes it from the current configuration, lock and connection,
and returns `409` on any difference. This is the approval TOCTOU fix applied again: what the person
saw is what runs. Re-pointing the connection, rotating its secret, editing the card's parameters or
re-approving the manifest all invalidate an open button. A plain digest is enough, because every
input is either already visible to the viewer or, like the revision, random.

**Confirmation.** For `disruptive` actions the frontend shows a modal before it sends the request.
The modal text is **core-authored** from the manifest title, the card label and the resolved
parameters, for example *"Restart container: jellyfin, on docker-local"*. It never uses document
text. `routine` actions send at once.

**Replay.** There is no separate prepare step and no nonce. A first draft had both, and they
guarded nothing the rest does not: anyone who can replay a request already holds the session and
can simply click. A cross-site page cannot send the request at all (below). Accidental repeats,
like a double click or a retried fetch, are absorbed by single-flight and the cooldown.

**Cross-site requests.** The existing protections stay: double-submit CSRF and `SameSite=Strict`.
The action endpoint also requires `Origin` to equal the server's own origin, falling back to
`Sec-Fetch-Site: same-origin`. A request with neither header is refused. This adds a check that
does not depend on the cookie policy.

**Bounded inputs.** The request body carries only the binding, at most 512 bytes,
decoded strictly with unknown fields rejected. Parameters are pinned in configuration and
validated against the manifest's parameter schema at config load. The schema uses the same
restricted JSON Schema subset as operation `params`, plus a required `pattern` or `enum` on every
string. A parameter fills a `{param}` placeholder through `routepath.Template`, the whole-segment
rule M2 already enforces. The viewer cannot supply any value.

**Concurrency and rate.** Execution is single-flight per (card, action). A second click while one
is in flight gets `409`. There is a per-action cooldown (default 5 s) and a per-session limit (10
per minute), and each action passes through the connection's own rate limit and concurrency.

### Authentication and authorisation

| auth mode | who may execute |
| --- | --- |
| `password` | any authenticated session. No sudo window, because running an approved action uses authority that was already granted; the sudo window guards granting it |
| `forward` | members of `auth.forward.actionGroups`, which is empty by default, so actions are off until an administrator names groups |
| `none` | nobody. Configuration with card actions under `auth: none` is a load error, and `--i-know-what-im-doing` does not override it |

This tightens what [config.v1](../../schemas/config.v1.schema.json) currently describes for
`auth: none`, where the override flag would allow actions. That semantic check was never
implemented, so no existing behaviour changes.

### Execution path

An action's request goes through the broker like a data request. It is authorised against a new
route use kind, `use: action`. `HTTP` matches only `use: data` and `AssetRef` only `use: asset`,
and an action executor matches only `use: action`. A data operation therefore cannot call an
action route, and an action cannot be used to read. This is the same split that stops the asset
endpoint from widening a data grant.

- One request, with no pipeline and no output template. The body, if any, is built from a
  structured template over the pinned parameters, never a string.
- Timeout defaults to 10 s, capped at 30 s. Redirects are never followed. At most 64 KiB of the
  response is read and then discarded.
- **No automatic retry.** The core cannot know whether an action is idempotent.
- After a `succeeded` outcome, the card is refreshed through the existing single-flighted refresh
  path.

### Failure reporting

| outcome | when | shown as |
| --- | --- | --- |
| `succeeded` | upstream answered 2xx | brief confirmation; the card refreshes |
| `failed` | refused before sending (policy, rate, binding), or upstream answered non-2xx | error with the upstream status; nothing retried |
| `unknown` | the request was sent but no response arrived before the deadline | warning: "may or may not have run, check the service" |

Messages are core-authored. Upstream bodies never reach the browser, because they are untrusted
and may echo request details. Every outcome is also published as an SSE `event`, so other open
tabs see it. It is also a natural first non-rule source for the activity feed in
[proposal 0001](0001-private-activity-feed.md), if that test continues.

### Audit trail

Auditing is write-ahead and fails closed:

1. Before the upstream request, an `action.execute` row is written with outcome `started`. If the
   write fails, the action does not run.
2. After the request, an `action.result` row is written that references the first row, with the
   outcome, upstream status and duration.

Detail is actor, IP, hashed session id, card, action, integration id and version, manifest digest,
connection id and revision, resolved parameters, method and canonical path. It never includes
headers or bodies. Refusals (a stale binding, an in-flight or cooldown `409`, a rate limit, a user
outside the action groups) are audited too, since repeated refusals are what probing looks like. A `started` row with no result
after a crash is itself the record of an `unknown` outcome. An audit viewer is not part of this
work. `sqlite3` and a later read-only page can read the table.

## Decisions

Settled 2026-10-02. The tables record what was weighed.

| | Decision |
| --- | --- |
| D1 | **A, simplified**: manifest declares, an approved `use: action` route authorises, card config enables. No new lock section, and one request instead of prepare and execute |
| D2 | **A**: targets pinned in card config. Per-row buttons wait for card grouping, below |
| D3 | **A**: the core renders a config-driven action row |
| D4 | **A**: confirmation modal, no re-authentication |
| D5 | **A**: refused under `auth: none`, with no override |
| D6 | **A**: the document's `confirm` and `danger` are ignored, then removed at the next `apiVersion` |

### D1. What grants an action

| Option | For | Against |
| --- | --- | --- |
| **A. Manifest declares, lock approves, card config enables** | Same three-party model as routes; actions appear in approval diffs; no new trust path | Manifest and lock schemas change (additively); most work |
| B. Config alone declares method and path, like `http-json` | Small; no approval changes | Puts request-shape authority in YAML with no review step; teaches the `http-json` escape hatch to write |
| C. Builtin actions only (Docker), never in manifests | Smallest; trusted code only | Excludes Home Assistant, Proxmox and every other first-party integration that would benefit; no path forward without redoing it |

**Recommendation: A**, built in two slices. Docker builtin actions come first, because they need
no manifest or lock change and prove the execution core. Manifest actions follow.

**Is A too much for a personal dashboard?** It was reviewed for exactly that, and two parts of the
first draft were cut. Most of the cost is the execution core: the binding, Origin check,
single-flight, write-ahead audit and failure outcomes. Every option needs that core, B and C
included. What A adds on top is small once it reuses what exists:

- *Cut:* a separate lock section for actions. Actions authorise through `use: action` routes,
  which the existing approval, diff and digest machinery already handle.
- *Cut:* the prepare step and its nonce, for the reasons under [Replay](#execution-is-one-bound-request).
- *Kept:* the manifest `actions` entry (id, title, effect, parameters, one request), a new route
  use kind, and card `actions` in config.

A is also the easier option for the person configuring. Under B they write an HTTP method, path
and body for every button. Under A they write `action: restart` with `params: { container:
jellyfin }`, and the integration author has already worked out the API.

### D2. Where a target comes from

| Option | For | Against |
| --- | --- | --- |
| **A. Parameters pinned in card config** | No input from the viewer or the document; the binding covers them; no Widget Document change | A "restart" per container must be listed in YAML; no per-row buttons in a container list |
| B. Document supplies the target per row, validated by manifest pattern, required to appear in the card's current document, confirmed with core-authored text | Natural per-row UX ("restart this container") | A malicious integration chooses what a row's button targets; needs a Widget Document change (list items carrying an action reference), which is a frozen contract |

**Recommendation: A** now. B is a real UX gain, but it should be designed when someone needs it,
as its own Widget Document change, not added to the first slice.

**Decided: A, with per-row buttons coming from card grouping instead.** If cards can belong to a
group, and a group can render its members as compact subcards that look like rows, then "a
container list with a restart button on each row" becomes one small card per container. Each
card has its own pinned action, so B's per-row UX arrives without letting the document choose a
target. Grouping is a layout feature worth having on its own, and it is tracked in
[issue #14](https://github.com/sergeyfarin/veduta/issues/14).

### D3. Who places the buttons

| Option | For | Against |
| --- | --- | --- |
| **A. Core renders a config-driven action row; document `actions` block stays display-only** | Every authority-bearing field comes from a trusted source; integrations need not know config ids | The integration cannot place buttons beside the thing they act on |
| B. Document places buttons by config id; label, effect and confirmation are taken from config | Integration controls layout | An integration has to guess ids it cannot know; a document can still hide or reorder a button to mislead |

**Recommendation: A.**

### D4. Presence for disruptive actions

| Option | For | Against |
| --- | --- | --- |
| **A. Confirmation modal, no re-authentication** | Usable from a phone; the confirmation proves a click, the session proves the person | A stolen live session can restart services, though it cannot widen what is approved |
| B. Sudo window required for `disruptive` | Strongest; matches approval | A password prompt before every restart; under forward auth it would make disruptive actions CLI-only, which removes the point |

**Recommendation: A.** The sudo window protects *granting* authority, and that stays where it is.
A stolen session is already covered by revocable sessions, expiry and the audit trail.

### D5. Actions under `auth: none`

| Option | For | Against |
| --- | --- | --- |
| **A. Refused, no override** | Any local process or browser tab could otherwise trigger one; unauthenticated mode is for trying Veduta, not running it | A loopback-only single-user setup cannot use actions without a password |
| B. Allowed with `--i-know-what-im-doing`, as the schema text says | Matches the current text | The override already covers a non-loopback bind; stacking consequential writes on it makes the flag mean "anything goes" |

**Recommendation: A.**

### D6. What happens to the document's `confirm` and `danger`

| Option | For | Against |
| --- | --- | --- |
| **A. Ignored for execution; deprecated, removed at the next `apiVersion`** | One source of truth; no change to v1 now | Fields remain that mean nothing |
| B. Kept as hints that may only strengthen confirmation | Integration can ask for more caution | Two sources of truth to explain; under D3-A the document's buttons never execute anyway |

**Recommendation: A.** It follows from D3.

## Implementation outline, when picked up

Not scheduled. Each slice is a releasable state, and every security check lands with a test that
is shown to fail when its guard is removed.

1. **Contracts.** Card config `actions`, CardState `actions`, the auth-mode rules above, and docs
   (README, security.md, configuration.md). Manifest `actions` and `use: action` are designed here
   but ship in slice 4.
2. **Execution core with Docker.** The action endpoint, binding, Origin check,
   single-flight, rate limits, write-ahead audit, `use: action` in the broker, Docker
   `start`/`stop`/`restart` behind `allowActions`.
3. **Frontend.** Core-rendered action row, confirmation modal, outcome toasts, SSE event, and e2e
   against a real `veduta serve` with a fake Docker endpoint.
4. **Manifest actions.** Manifest schema, `use: action` routes, approval-diff presentation, declarative
   executor, then first-party actions: Home Assistant scripts and scenes, Proxmox guest power, and
   library scans.

Tests that must be shown to fail when their guard is removed include: a stale binding after a
connection revision change, a parameter edit or a re-approval; a second request while one is in
flight, and one inside the cooldown; a cross-origin `Origin`; a missing CSRF header; a data operation calling a
`use: action` route and the reverse; an audit insert failure stopping execution; card actions
under `auth: none` failing to load; a forward-auth user outside `actionGroups`; and a
document-supplied `confirm: false` having no effect on a disruptive action.
