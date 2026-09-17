# The Widget Document stays the closed presentation contract

Status: accepted 2026-09-17, in answer to an external design proposal. Revisit
under the triggers below.

The proposal was specific: make cards more expressive without granting custom
JS or CSS, by adopting Markdown with MyST-style `:::` directives as the human
authoring syntax, Microsoft Adaptive Cards as the integration-facing schema, and
Mermaid and Vega-Lite as embedded blocks for diagrams and charts, with
client-side bindings (`$server.cpu`) and a small expression language over them.

It is recorded here rather than answered in conversation because the question
recurs — "why not just let a card embed Mermaid?" is the natural first thought
for anyone who meets the restriction — and because the answer is not "it is
unsafe in general". Each of those tools is defensible in its own setting. The
answer is specific to what this repository has already frozen, and that is worth
writing down once.

## Decision

1. **The Widget Document remains the only presentation contract.** No second
   schema, no alternative document format, no host-negotiated dialect. An
   integration produces a Widget Document or it produces nothing.
2. **The renderer stays closed.** One Svelte component per block type, a
   registry keyed by `type`, unknown types rendering a labelled placeholder. A
   new block type is a deliberate core change. This is the status quo restated,
   not a new restriction — see
   [should there be a frontend plugin surface at all?](../03-backlog.md#open-question-should-there-be-a-frontend-plugin-surface-at-all).
3. **Adaptive Cards is not adopted**, as a schema or as an interoperability
   format, for 0.1 or as a planned direction.
4. **Mermaid and Vega-Lite are not embedded.** Neither as a block type, nor
   behind a subset translator, nor in a sandboxed frame.
5. **No client-side binding or expression layer.** Documents carry resolved
   values. There is no `$card.field` syntax, and no expression evaluator in the
   frontend.
6. **What is taken from the proposal is the visualisation gap, natively.** A
   chart or sparkline block drawn as an SVG element tree by a Svelte component,
   like every other block — tracked as
   [no native visualisation block for retained history](../03-backlog.md#no-native-visualisation-block-for-retained-history)
   and scheduled for 0.2, not for the alpha.
7. **Directive syntax stays a candidate for the authoring layer only**, recorded
   in [markdown has no authoring syntax for structure](../03-backlog.md#markdown-is-prose-only-with-no-authoring-syntax-for-structure),
   deferred with no work planned.

## Evidence

**The proposal's own principle is already the architecture.** "Card authors own
the content; the host owns the look and feel" is decision D2. The Widget Document
is the declared output, the renderer is closed, and `{@html}` appears nowhere in
`web/src` — enforced by
[`TestNoRawHTMLDirectiveInFrontend`](../../internal/contracts/no_html_directive_test.go),
a Go contract test rather than a shell grep, so the same `go test ./...` that
runs everywhere runs it. Agreement on the principle is therefore not evidence
for any of the four mechanisms.

**Adaptive Cards would replace a better-fitted schema with a worse one.**
`schemas/widget-document.v1.schema.json` already is a declarative, host-rendered
JSON UI. It additionally carries what Adaptive Cards has no concept of: the
CardState envelope that makes stale, error and loading first-class; declared
numeric `signal`s that J1 persists and J2's rules evaluate; `image` references
that resolve through E1's asset proxy rather than fetching a URL; and action
*identifiers* the broker authorises. Adaptive Cards' `Action.OpenUrl` takes an
arbitrary URL and its `Input.*` family submits arbitrary payloads — both
contradict the approved-action model directly. Adopting it means versioning
against a schema this project does not control, to gain a poorer fit. The
convergence is worth noting as corroboration and nothing more: two designs
starting from "the host owns rendering" arrived at the same shape.

**Mermaid and Vega-Lite cannot be mounted without giving up the invariant.**
Both render by constructing DOM or SVG themselves and inserting it. There is no
configuration of either that turns it into an inert value the renderer can walk.
Mermaid's `securityLevel: strict` addresses hostile content *inside* a diagram —
HTML labels, click handlers — not the insertion step, which is the part that
matters here. So the only two integrations available are `{@html}`, which fails
CI, or an iframe, which is the sandboxed-frame block the backlog already weighed
and rejected: a new threat model, a new protocol to version, and a direct
challenge to D2. Bundle weight against a single embedded binary is a real but
secondary objection.

**The binding layer solves a problem this architecture does not have.** Client
templating exists where the browser holds a data model and the document
describes how to read it. Veduta's scheduler resolves values server-side and
publishes a document containing `72`, not a reference to it. Adding bindings
would move evaluation to the least controllable side of the boundary. The
suggestion to reuse `expr-lang/expr` for this does not apply: `expr` is Go and
runs in D3's manifest pipeline; a frontend expression layer would be a *second*
evaluator, in a different language, with its own sandbox and fuzz surface, to
duplicate work already done in the better place.

**The one real gap the proposal found is visualisation.** There is no chart,
sparkline or gauge block. `chart` is, in fact, the string the renderer tests use
as their example of an *unknown* block type. Meanwhile J1 retains bounded numeric
history for declared signals, and `schemas/plugin-manifest.v1.schema.json`
describes that retention as being "for `for:` windows and sparklines" — so the
manifest schema has been advertising a renderer nobody wrote. That gap is worth
closing, and closing it natively costs no invariant: points in, an SVG element
tree out, drawn by a Svelte component. It needs no library and creates no
injection sink, which is precisely why the native route was chosen over the
embedded one.

## Consequences accepted

Veduta is less expressive than a dashboard that accepts user templates, and will
stay that way. An integration that genuinely needs a diagram cannot have one
until a native block exists for it. Authors who know Mermaid cannot reuse that
knowledge here. These are the costs of the trade the project is deliberately
making, and they should be stated plainly in documentation rather than presented
as temporary.

## Revisit conditions

- **A concrete integration cannot be expressed as a Widget Document, *and* the
  missing block type is too specific to justify adding to the core renderer.**
  Both halves, together; neither has been observed. This is the same trigger the
  frontend-plugin-surface entry sets, restated so the two stay aligned.
- **Someone asks for structural authoring in `veduta.yaml` that the existing card
  types cannot express.** That reopens the directive question — the authoring
  layer only, never the rendering vocabulary.
- **Interoperability is requested by a real consumer.** Exporting a Widget
  Document *to* Adaptive Cards is a different and much smaller question than
  accepting Adaptive Cards as input, and this decision does not foreclose it.

Visualisation work proceeding under item 6 is not a revisit and does not need
this decision reopened.
