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

**Eight block renderings**: `status`, `metrics`, `progress`, `list`, `image-grid`, `poster-grid`,
links, and the notice strip. They cover every v1 block type that carries visual weight; `table`,
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

## Decisions this settles for B1

1. **Status is never colour alone.** A dot *and* a word, so it survives colour blindness and a
   greyscale screenshot.
2. **Tabular numerals for every metric** (`font-variant-numeric: tabular-nums`), so live values do
   not jitter the layout as digits change width.
3. **Aspect ratios come from the block, not the image** — `1:1` for photos, `2:3` for posters — so
   nothing reflows when an image lands. This is what makes the cumulative-layout-shift target in
   B4 achievable rather than aspirational.
4. **Container queries, not media queries, inside cards.** A poster row reflows on the card's
   width, so the same card works in a one- or two-column span without variants.
5. **Grid spans are card configuration** (`span-2`, `span-4`), which maps directly onto the
   `span: {columns, rows}` already in the config schema.
6. **Captions appear on hover only.** A photo wall with permanent labels stops being a photo wall.
7. `prefers-reduced-motion` disables the skeleton shimmer.

## What it deliberately does not do

No drag-and-drop, no per-card colour customisation, no icon pack — icons are placeholder squares,
because the icon proxy is milestone L2 and the layout must hold without them. No real data: the
images are inline SVG gradients, so the file is self-contained and the visual-regression baseline
in B5 can be byte-stable.

## Carrying it into B1

`web/src/styles/tokens.css` currently holds five placeholder tokens. B1 replaces them with the 24
here, splits the block CSS into one file per block type under `web/src/lib/blocks/`, and keeps this
file as the reference: if a Svelte renderer and this prototype disagree, one of them is a bug.
