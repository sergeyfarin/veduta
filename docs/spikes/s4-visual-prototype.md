# S4 — Visual prototype

**Status:** complete. **Blocks:** B1 (design tokens and layout).
**Artifact:** [`s4-visual-prototype.html`](s4-visual-prototype.html) — open it directly; no build,
no framework, no external requests.

The prototype exists because "beautiful by default" is a product requirement, and once Svelte
components exist the tokens stop being negotiable. Settling them in one static file is cheap;
renegotiating them across twelve components is not.

## What it fixes

**24 design tokens**, and nothing outside them. No per-card CSS anywhere in the file — if a card
needs a bespoke rule, the vocabulary is wrong, and that is the constraint the prototype was built
to test.

Neutrals carry a hint of warmth (`#fbfbf9`, `#17171a`) rather than pure grey, which reads as
unfinished. One accent, used for progress fill and focus, never for decoration.

**Ten block renderings**: `status`, `metrics` (both layouts), `progress`, `list`, `image-grid`,
`poster-grid`, `weather`, `feed`, links, and the notice strip. They cover every v1 block type that carries visual weight; `table`,
`text` and `markdown` are typographic and need no prototype.

**Four card states, all rendered, none an afterthought:**

| State | Treatment |
| --- | --- |
| `ok` | full colour, status dot plus word |
| `stale` | content dimmed and desaturated, notice strip with a relative age — **never blanked** |
| `error` | no content, the upstream error, and the retry interval |
| `pending` | shimmer skeletons at the metric positions |

Stale deserves the emphasis: a homelab dashboard spends a lot of its life there, and the CardState
envelope makes it unavoidable rather than exceptional. It has to look deliberate, not broken.

## Second pass — review feedback

The first prototype drew five comments. All five were right; four are applied, one is split.

### Cards were leaving voids at the bottom **[applied]**

The Jellyfin card was half empty because grid items stretch to the tallest card in the row while
their content pools at the top. Homepage looks tidy precisely because every tile is identical — but
that uniformity is not available to us, and buying it would mean giving up the thing the product is
for. A photo wall and a three-number card genuinely want different amounts of space.

What *is* available is a shared rhythm. Heights are now **quantised to a 92px row unit**: a card is
an exact multiple of it, declared as `span: {rows: N}` — a field the config schema already had.
Tops and bottoms line up across the page even where cards differ, and inside each card the media
block takes the slack (`flex: 1`) so the poster row stretches to fill rather than leaving a gap.
The result is regular without being uniform.

### Two-figure cards looked unbalanced **[applied]**

Correct, and the cause is structural rather than cosmetic: two metrics in an equal-column grid each
left-align into their half, so the second value floats in the middle of the card with a trailing
void. Rather than a per-card alignment exception, the vocabulary splits on count: **one or two
figures render as key-value rows** (label left, value hard right, which is the arrangement the eye
wants for comparison); **three or more use columns**, where the width is genuinely used. Both are
existing block types, so this is a rendering rule, not a new primitive.

### A green dot next to the word "Online" **[applied]**

Two channels saying the same nothing. The rule is now: the dot is always present; **text appears
only when it carries information the dot cannot** — an uptime, a container count, a failure reason.
Healthy-and-unremarkable is a bare dot with an accessible label and a hover title.

This still satisfies the accessibility constraint, and arguably better than before: abnormal states
always carry a word, so *the presence of text* is itself a non-colour signal, rather than every
state carrying a word and colour being the only thing that distinguishes them.

### Links were repeating cards **[applied]**

Immich, Jellyfin and AdGuard appeared both as cards and as links. The rule: **the links block is
for services that do not have a card**. A card is already a link — its title is clickable — so
repeating it wastes the one thing a dashboard cannot buy more of, which is space above the fold.
The block is now titled "Elsewhere" and holds only uncarded services.

### External sources: weather, feeds, markets **[applied, with a caveat]**

Not too much — and cheaper than it looks. Weather and RSS are baseline expectations in this
category (Glance is built around feeds), they need no credentials, and they exercise exactly the
machinery that already exists: a declarative manifest against a public HTTP API, rendered through
`metrics` and `list`. The prototype adds a weather card, a feed card and a markets card; the feed
needs no new block type, and weather earns a small one.

The caveat is architectural rather than visual: these are the first connections that reach the
**public internet** rather than the LAN, which is a different trust category. An internet-facing
connection should be marked as such, rate-limited more tightly, and never granted to an
integration that also holds a LAN slot — otherwise a feed integration becomes an exfiltration path
for data read from a local service. That belongs in the connection model before the first such
integration ships, and is now noted against D1.

### Customize screen and grouping **[split — this is the one to argue about]**

Two different asks are bundled here, and they have different answers.

**Grouping by tag: yes, and it costs almost nothing.** Cards now take `tags: []`, and the dashboard
takes `groupBy: section | tag` with a runtime switch (sketched in the prototype's top bar). Sections
stay the authored arrangement; tags give a cross-cutting second dimension — by tag, by host,
by whatever the user finds useful — without duplicating cards.

**Nested hierarchy: recommended against.** Homepage allows nested groups, and they are a reliable
source of layouts nobody can navigate. Tags give the second dimension without a tree, and a tree is
very hard to remove once configurations depend on it.

**Drag-and-drop editor: yes eventually, but not in 0.1, and I would push back on doing it sooner.**
Three reasons. It is Homarr's core identity and we will not out-build it there in a first release.
It needs the config editor to exist first, because the whole point is that rearranging writes back
to YAML — the `config.Apply` path was designed for exactly this and is already in the architecture,
so nothing here is foreclosed. And it sits directly across the demo path: every day spent on it is
a day the Immich slice is not on screen. The honest sequence is 0.1 config-as-code with `span` and
`tags`, 0.3 config editor, and drag-and-drop on top of it.

If that trade is wrong, the place to say so is now, because it changes the milestone order rather
than the architecture.

## Decisions this settles for B1

1. **Status text only when it says something.** The dot is always there; a word accompanies it only
   when it carries information the dot cannot. Abnormal states always have one, so text presence is
   itself a non-colour signal.
2. **Heights are quantised** to one row unit and declared as `span: {rows}`, so cards line up
   without being uniform, and content fills its box rather than pooling at the top.
3. **Metrics split on count**: one or two as key-value rows, three or more as columns.
4. **Links are for uncarded services only.**
5. **Tabular numerals for every metric** (`font-variant-numeric: tabular-nums`), so live values do
   not jitter the layout as digits change width.
6. **Aspect ratios come from the block, not the image** — `1:1` for photos, `2:3` for posters — so
   nothing reflows when an image lands. This is what makes the cumulative-layout-shift target in
   B4 achievable rather than aspirational.
7. **Container queries, not media queries, inside cards.** A poster row reflows on the card's
   width, so the same card works in a one- or two-column span without variants.
8. **Grid spans are card configuration** (`span-2`/`h-3`), mapping directly onto
   `span: {columns, rows}` in the config schema.
9. **Captions appear on hover only.** A photo wall with permanent labels stops being a photo wall.
10. `prefers-reduced-motion` disables the skeleton shimmer.

## What it deliberately does not do

No drag-and-drop, no per-card colour customisation, no icon pack — icons are placeholder squares,
because the icon proxy is milestone L2 and the layout must hold without them. No real data: the
images are inline SVG gradients, so the file is self-contained and the visual-regression baseline
in B5 can be byte-stable.

## Carrying it into B1

`web/src/styles/tokens.css` currently holds five placeholder tokens. B1 replaces them with the 24
here, splits the block CSS into one file per block type under `web/src/lib/blocks/`, and keeps this
file as the reference: if a Svelte renderer and this prototype disagree, one of them is a bug.
