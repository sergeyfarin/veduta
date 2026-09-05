# 03 — Gaps, issues and improvement opportunities

Distinct from [docs/02-implementation-plan.md](02-implementation-plan.md)'s milestone table: that
document is deliberately-scoped, dependency-ordered *planned* work. This one is the opposite kind
of list - things noticed *while* building something else, real enough to write down, not yet worth
their own milestone or not in scope for the milestone that found them. An entry here is a small
debt or an open decision, not a task assignment; when one gets picked up, say so and where the fix
landed rather than deleting the entry silently, so the history of "we knew about this since when"
survives.

Each entry: what the gap is, which milestone found it, why it wasn't fixed there, and its rough
priority (which milestone should absorb it, or "before X" for a hard blocker).

---

## Open

### Config: `httpConnection.headers` values are plain strings, not secret-capable

`schemas/config.v1.schema.json`'s `httpConnection.headers` (line ~556) is
`additionalProperties: {type: string}`, while `notifications.channels.*.webhook.headers` (line
~390) is `additionalProperties: {$ref: secretRef}`. An admin can put `${secret:NAME}` in a webhook
header but not in a custom HTTP connection header - so an upstream API that authenticates via a
non-standard header (not one of the schema's typed `auth.type` values) forces the credential into
the config file in plaintext. Found while implementing C1, reading the schema closely enough to
notice the asymmetry; not fixed there because it is a schema change (needs re-validating existing
configs, arguably a design call, not C1's own scope of "build the loader for the schema as it
exists"). Priority: low-medium, whenever `schemas/config.v1.schema.json` next gets a deliberate
revision - do not roll it into an unrelated milestone's diff.

### `auth: none` + secrets/actions guard is not implemented

The schema's own description for `auth.mode: none` says: "additionally requires the
`--i-know-what-im-doing` flag when any action or secret is configured (semantic check)." C1 does
not implement this - found while writing `validateAuth`, deliberately not added there because
"any action... configured" has nothing to check yet (`ActionsBlock` renders permanently disabled;
no action can be configured until an execution path exists, several milestones out in Phase H),
and "any secret... configured" only becomes a meaningful count once C2 gives `SecretRef` real
resolution. Implementing half the check now (secrets only) would be a check that silently stops
covering half of what its own description promises the day actions exist. Priority: implement
alongside whichever of C2 (secrets half) or the Phase H action-execution milestone (actions half)
lands second - whoever notices the other half is already there.

### S3 (expr vs cel bake-off) is still undone and genuinely blocks D3 and J2

Found while starting C1, which lists S3 as a dependency. C1 routed around it - `rule.when` is only
pattern-scanned for card-id string literals there, never evaluated, so which expression language
rules use doesn't matter yet. **D3** (declarative runtime) and **J2** (rules) cannot take that
shortcut: D3 literally builds "an `expr` environment" and needs a real evaluator, and J2 evaluates
`when` for real. Priority: **before D3 starts** - there is runway (D1 → D1b → D2 → D2b sit between
here and D3), but this is a decision to make deliberately, not something to discover mid-D3.

---

## Resolved
