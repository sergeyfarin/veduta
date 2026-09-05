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

### `auth: none` + actions guard is not implemented

The schema's own description for `auth.mode: none` says: "additionally requires the
`--i-know-what-im-doing` flag when any action or secret is configured (semantic check)." C3
implemented the secrets half: `serve` now loads a `*Snapshot`, resolves every secret before
publishing it, and refuses `auth.mode: none` plus any secret reference unless the override was
passed. "Any action... configured" remains blocked: `ActionsBlock` renders permanently disabled
until Phase H gives actions a real execution path. Priority: Phase H; extend the same startup guard
once an enabled action has a real configuration representation.

### `${secret:NAME}` cannot be embedded in a larger string - but Jellyfin's real auth header needs exactly that

Found for real, not hypothesised, while smoke-testing C2's resolver against the actual shipped
`examples/veduta.yaml` end to end for the first time (`config.LoadPath` → `secrets.ResolveAll`).
The Jellyfin connection's auth value must be `Authorization: MediaBrowser Token="${secret:
JELLYFIN_KEY}"` (`docs/spikes/s2-upstream-reality-check.md`'s F6 - this is Jellyfin's actual,
documented scheme, not a choice this project made). `secretRefPattern` only recognises
`${secret:NAME}` as an entire scalar value ("there is no defined way to redact half a string" -
`internal/config/secretref.go`), so as committed, this value is silently a **literal string
containing the placeholder text verbatim** - it would never resolve, and would send the wrong
header the moment a real HTTP client exists (Phase D). No error existed anywhere for this before
today. Mitigated, not fixed: `internal/config/secretsuspicious.go`'s `suspiciousSecretRefs` now
emits a **warning** (`TestLoad_RealExampleConfig` asserts it fires on the real file) for any
scalar containing `${secret:` that doesn't match the whole-value pattern, so this is at least
visible at `--check-config` time instead of failing silently at runtime. The real fix needs
`SecretRef` to become template-aware (a string with one or more named placeholders, composed and
wrapped in a single opaque `secrets.Value` - not a per-placeholder redaction problem, since the
*whole* composed result can just always print as `***`). Priority: **decide before D1** actually
implements auth injection for HTTP connections - D1 is the first place this needs to actually
work, and rewriting `examples/veduta.yaml`'s Jellyfin block to a syntax that doesn't yet exist
would be premature before D1 settles the shape.

### S3 (expr vs cel bake-off) is still undone and genuinely blocks D3 and J2

Found while starting C1, which lists S3 as a dependency. C1 routed around it - `rule.when` is only
pattern-scanned for card-id string literals there, never evaluated, so which expression language
rules use doesn't matter yet. **D3** (declarative runtime) and **J2** (rules) cannot take that
shortcut: D3 literally builds "an `expr` environment" and needs a real evaluator, and J2 evaluates
`when` for real. Priority: **before D3 starts** - there is runway (D1 → D1b → D2 → D2b sit between
here and D3), but this is a decision to make deliberately, not something to discover mid-D3.

---

## Resolved

### Config: `baseUrl` whitespace regex accidentally rejected the letter `s`

Found while adding C3's startup security test with the ordinary hostname `service`. The JSON
Schema encoded `\\s` with one escaping layer too many, so the regex engine saw a character class
excluding a backslash and the literal letter `s`, rather than whitespace. Hosts and paths containing
`s` were rejected while existing examples happened not to expose it. Resolved in C3 by correcting
the JSON escaping and keeping `http://service:8080` in the regression test.

### Config: `SecretRef` carried no position, blocking C2's "diagnostic naming the config location"

Found while planning C2, which needs to report *where* a missing secret was referenced, not just
that one was missing - C1's `SecretRef{Literal, Name}` had no file/line/col, and nothing else
retained one after `Load` returned a `*Snapshot`. Resolved in C2, not by adding position fields to
`SecretRef` itself (that would need a reflection-based walk matching decoded struct fields back to
schema paths, and would be ambiguous whenever the same secret name is referenced more than once -
which name lives are the case for). Instead: `internal/config/secretlocations.go` scans the merged
tree by *content* (any scalar matching `${secret:NAME}`) rather than by structural path, producing
`Snapshot.SecretRefs []SecretLocation{Name, File, Line, Column}` with one entry per occurrence.
`secrets.ResolveAll` resolves each distinct name once and emits a `config.Diagnostic` - reusing
the existing type rather than inventing a parallel one - for every occurrence of a name that
failed, so a secret referenced in three places and missing is three real locations, not one
anonymous complaint. See `TestLoad_SecretRefsCollectsEveryOccurrence` and `TestResolveAll`.

### Config: where does "a secret value in a Widget Document is rejected" live?

Found while planning C2, which states this as its own acceptance test. `internal/widgets` cannot
import `internal/secrets` (docs/01-architecture.md's frozen import-boundary rule: integrations,
and the widgets package they emit into, never see credentials at all), so the check cannot live in
`widgets.Validate`. Resolved in the other direction instead: `internal/secrets` imports
`internal/widgets` (nothing forbids that direction - the frozen rule constrains what integrations
can reach, not what core packages may import) and `Registry.ContainsSecretInDocument(doc)` walks
every text-bearing field of all nine v1 block types explicitly (a type-switch, matching this
project's existing preference for that over reflection - see `internal/widgets/blocks.go`'s own
dispatch), reusing the same `Registry.ContainsSecret` substring check the log scrubber uses.
`TestContainsSecretInDocument_EveryBlockType` covers all nine block types and is itself mutation-
tested: deleting one type's case from the switch was confirmed to fail the test before this was
considered done. What's still pending, honestly: nothing calls this yet against a *real* produced
Document - that caller is Phase F's scheduler, which does not exist. This is the primitive ready
for that caller, not the end-to-end wiring.
