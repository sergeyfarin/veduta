# Theming: two fixed presets, no public knobs, no user-authored CSS

Status: accepted 2026-09-10, amended the same day after review, amended again 2026-09-13
on two of its own revisit triggers. Revisit under the triggers below.

The amendment matters enough to state plainly: the first draft renamed
`dashboard.theme` to carry the preset axis, and planned to derive a public knob
vocabulary from whichever design tokens turned out to differ between the two
presets. Both were wrong, and both are reversed here. What survives is the
security and accessibility posture, which review did not challenge.

## Decision

Ship theming as **two fixed, curated presets** — Clean and Veil — selected by one
config field. No user-authored CSS. No theme editor. No public knobs until real
demand exists, and none derived from implementation details when it does.

1. **`dashboard.appearance`, defaulting to `clean`.** One field, one axis: the
   instance's visual preset. The enum admits only presets that actually exist —
   today just `clean` — and widens as presets ship. Widening an enum is
   backward-compatible; admitting `veil` before the preset is built would accept
   configuration that silently does nothing, which is the same defect as the
   `dashboard.theme` field this decision removes.
2. **The light/dark axis is not configured at all.** It stays a per-viewer
   browser preference in `localStorage`, as it already is. It is not the same
   axis as the preset and must not share a name with it — see Evidence.
3. **`dashboard.theme` is removed, not narrowed.** It was a published v1 field
   whose shipped meaning was the colour scheme. Redefining it to mean the preset
   would have broken every config copied from the shipped example.
4. **Presets are private implementations.** A preset owns its surface opacity,
   scrim, border alpha, shadow and blur. Those are mechanics, not user choices,
   and they are deliberately not configurable — which is what keeps the
   combination space small enough to validate at all.
5. **Clean and Veil share component structure and semantic tokens.** They do not
   share an "effect vocabulary": blur, scrim and surface alpha are Veil-private.
   Clean is not "Veil with the effects set to zero" — that framing leaks
   special-effect mechanics into the base design, and is also literally wrong,
   because a zero-radius `backdrop-filter` still creates a stacking context and
   forces GPU compositing per card. Clean must not have the property at all.
6. **Fixed presets are validated in CI, not at config load.** Two built-in
   presets are a closed set: contrast is a build-time property of the shipped
   CSS. Config-load semantic validation becomes necessary only if users can ever
   *combine* appearance values, which today they cannot.
7. **If public options ever ship, they express intent, not mechanics.** The
   plausible shape is `preset`, `accent` as a named palette, and a background
   reference — never `surfaceAlpha`, `scrimFloor`, `borderAlpha`, `blurRadius`.
   Config is a public API; design tokens are implementation. Deriving the former
   from the latter would freeze internals into a v1 schema and recreate the
   combinatorial validation problem this decision avoids.

### Accessibility guarantee

Stated precisely, because the first draft overclaimed. Config validation cannot
guarantee accessibility in general: motion, forced colours, zoom, focus
visibility, font rendering, browser differences and integration-authored content
all sit outside it. What is guaranteed:

> Every shipped preset satisfies the documented contrast matrix in both colour
> schemes, and status and authentication UI remains distinguishable without
> relying on colour alone.

For a translucent preset over an arbitrary backdrop, contrast is evaluated
across the whole compositing chain against **worst-case source pixels — pure
black and pure white — not a representative photograph**. Note the bound runs in
both directions: a dark scrim under light text caps composited luminance from
above, while a light scrim under dark text imposes a floor. Veil ships both
colour schemes, so both bounds are tested.

## Evidence from this codebase

**`dashboard.theme` already meant the colour scheme, publicly.** It shipped in
`examples/veduta.yaml` as `theme: auto`, with a comment advertising
`auto | light | dark | <custom theme name>`; it was listed in the generated
`docs/configuration.md`; and it appeared in five test fixtures. `dashboard` is
`additionalProperties: false`, so narrowing that field to `clean | veil` would
have made any config copied from the example fail to load. The first draft's
claim — that nothing could depend on a field nothing reads — was wrong: the
project advertised it, which is a form of depending on it.

**The collision was worst for the users the importer courts.**
`testdata/homepage/settings.yaml`, the gethomepage import fixture, contains
`theme: dark`. gethomepage's own `theme` key means light/dark. Reusing the word
for the preset axis would have contradicted the prior mental model of exactly
the audience `internal/homepageimport` exists to serve. (The importer drops the
key today, so removing the field breaks nothing there.)

**The token layer is ready; the enforcement layer is not.** Everything visual
routes through ~15 custom properties in `web/src/styles/tokens.css`, and
`TestComponentsUseTokensNotHardcodedColours` forbids a colour literal anywhere
in `web/src` outside that file. But `TestTokenContrast` parses hex only —
`(--v-[a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\s*;` — so a translucent `--v-surface`
drops out of the palette and the test fails with "token missing". It fails
loudly, which is right; the hazard is that the cheap fix is to delete the pair
and silently lose the guarantee. Generalising that parser to run per preset is a
prerequisite of Veil, not a follow-up.

**A single accent hex cannot serve both colour schemes — measured.** The shipped
light accent `#2f6f4f` scores **2.99:1** against the dark surface `#17171a`, one
hundredth under the 3:1 floor `TestTokenContrast` enforces; the dark accent
`#6fb894` scores **2.34:1** on white. Sweeping sRGB, only **32.8%** of colours
clear 3:1 against both surfaces, against 57.3% for light alone. This is why an
accent option, if it ever ships, is a named palette resolving to a validated
pair rather than a colour picker.

**Translucency over photographs is the `.dimmed` bug with an unknown backdrop.**
`--v-faint` sits at 4.71:1 on `--v-surface`, 0.21 above the AA floor, which is
why `TestStaleStateContrast` exists: any opacity reduction on that token, even
0.9, pushes it under. The project already solved the unknown-backdrop case once
— `--v-overlay-text` and `--v-overlay-scrim` are documented as needing to stay
legible "against ANY photo in either theme" — and that scrim is the mechanism
Veil reuses.

**The persistence question was already answered by D5.** Config is
hand-authored, comment-bearing YAML, validated and activated as a generation. A
server that rewrote it to save a theme would destroy the user's comments;
writing to the SQLite `settings` table instead would create the duelling store
D5 exists to prevent.

## Options declined

- **Narrowing `dashboard.theme` to a preset enum.** Declined for the breaking
  change and the terminology collision above. Removing it pre-tag costs one
  schema change; renaming its meaning would have cost users a failed load.
- **Adding a `colorScheme` config field while separating the axes.** Declined as
  an unneeded field. Light/dark is already per-viewer and works; a server-side
  default for first-time viewers is a real feature, but not one anybody has
  asked for, and it can be added later without ambiguity now that `appearance`
  owns the other axis.
- **Deriving public knobs from the Clean/Veil token diff.** Declined: that
  derives a public API from an implementation diff, exposes coupled mechanics as
  independent controls, and recreates the combinatorial validation problem.
- **User-authored CSS, in a textarea or a file.** Unsupported through 0.x.
  Unversionable, unvalidatable, and a real surface: CSS can exfiltrate via
  `background: url()` with attribute selectors and can obscure the persistent
  authentication-disabled warning H2 added. Reconsider only under the trigger
  below — not "never", but not on aesthetic demand either.
- **A server-side theme editor, or a preview UI that emits YAML.** The editor is
  declined on D5 grounds. The YAML-emitting picker is *deferred* rather than
  refused: with two presets and no options, a picker that cannot save is a
  half-editor, and two screenshots in the documentation carry the same
  information. Revisit when there are enough real choices that previewing a
  combination is genuinely useful.
- **macOS-like and Windows-like presets.** They are design languages, not
  palettes, so a token swap yields "clean, but bluer". Their fonts are not
  redistributable, and `--v-font` already begins
  `system-ui, -apple-system, "Segoe UI"` — so on a Mac, Clean *already* renders
  in the macOS system font, making a macOS preset that only looks right on a Mac
  self-cancelling. And shipping presets under those names weeks after adopting a
  trademark policy is an avoidable own-goal.
- **Per-user presets.** Deferred. It requires deciding whether appearance is an
  admin operation or a viewer preference, and adds per-user storage, for a
  product whose light/dark axis is already per-viewer.

## Resolved: the background image (2026-09-10)

Veil ships with both a generated default and a user-supplied option, and the
question above is settled as it was framed.

`dashboard.background` is a **local path**, absolute or relative to the config
file's directory — the rule `integrations.ResolveSource` already uses, so it does
not depend on the working directory. It is never a URL: a third-party URL would
make every viewer's browser fetch from a host the operator does not control.
Veduta reads the file and serves it from one fixed route, and the browser is told
only *whether* a background exists, never where it lives.

Content is sniffed, not trusted from the extension. That is not a privilege
boundary — the operator wrote the config and could already point `dataDir`
anywhere — but a blast-radius limit on a typo: without it, a mistyped path turns
the route into an arbitrary file read served to every viewer.
`TestBackground_RefusesAFileThatIsNotAnImage` holds that line.

The scrim is mandatory and lives in the CSS, not in a caller that could forget
it: at `--v-scrim-alpha: 0.8` an image contributes at most 20% of the backdrop,
and `TestVeilImageContrast` proves every text token clears its floor against both
extremes a photograph can present. Each colour scheme carries its own scrim
colour, because the bound runs in opposite directions — reusing one for both
fails the test. The visible cost is real and accepted: guaranteeing AA over an
arbitrary photograph and showing that photograph at full strength are not
compatible, and this project resolves that toward legibility.

A background under `appearance: clean` is a **configuration error**, not a
silently ignored field — the schema requires `appearance: veil` alongside it.
Accepting configuration that does nothing is the defect `dashboard.theme` was.

## Amended 2026-09-13: a preset picker, and paintings instead of a gradient

Two triggers this decision wrote down have fired, and both are answered here. Neither overturns
the security or accessibility posture; one narrows what "not configurable" means, the other
changes what the backdrop is made of.

### Appearance is a viewer preference with a configured default

*Trigger: "Per-user appearance becomes worth building if ... one instance serves audiences with
genuinely different needs."* It was asked for directly, which is the same signal arriving sooner.

`dashboard.appearance` is now the instance's **default**, not its only say. A viewer can choose
Clean or Veil for themselves; the choice lives in their own `localStorage` under
`veduta.appearance`, alongside `veduta.theme`, and `auto` means "whatever the operator
configured". The config field, the schema and every contrast guarantee are unchanged — what moved
is who gets the last word, and only in their own browser.

The original text declined "a server-side theme editor" on D5 grounds: a server that rewrote
hand-authored YAML would destroy the operator's comments, and writing to the `settings` table
would create the duelling store D5 exists to prevent. **That reasoning is untouched and still
holds** — this stores nothing server-side. It is the same mechanism light/dark has always used,
applied to the axis next to it.

**Two controls, not one with four values.** The two axes are independent and all four combinations
ship, so a single four-valued control would have to drop a value, and the value it would drop is
`auto`. That is the wrong one to lose twice over: `auto` is the default on both axes, it is the
only value that changes on its own (the system flipping to dark at sunset), and the two `auto`s do
not even mean the same thing — one resolves in CSS against `prefers-color-scheme`, the other
resolves against configuration the browser only learns from `GET /dashboard`. Three values times
two controls covers six states honestly; four values would cover four and lie about the rest.

### Veil's backdrop is two public-domain vedute

*Trigger: "a background reference" was named as a plausible future option, and the existing
`dashboard.background` had already established the mechanism.*

The default backdrop is no longer the generated gradient alone. Veil now ships a painting per
colour scheme — Canaletto's daylit *Molo, Venice, from the Bacino di San Marco* for light,
Vernet's *Entrance to the Port of Palermo by Moonlight* for dark. Both are public domain; both are
recorded in THIRD-PARTY-LICENSES.md.

The original argued for a gradient on four grounds. Three are answered and one is accepted as a
cost:

- *"It costs zero bytes in the binary."* Now ~218 KB, for two files that are cached immutably by
  content hash and fetched once. That is the real price and it is paid knowingly.
- *"It needs no font or image licence."* Both works are public domain, and the reproductions are
  faithful photographs of two-dimensional public-domain works.
- *"It flips with the colour scheme (a JPEG cannot)."* One JPEG cannot; two can, and each is
  toned for the scheme it serves.
- *"Its luminance range is exactly computable at build time."* A painting's is not — which is
  precisely the case this decision had already solved for `dashboard.background`. The bundled
  paintings sit under the same mandatory scrim, proven the same worst-case way, by the same test.
  Configuring a background now overrides one variable, `--v-backdrop-image`, and nothing else, so
  the bundled and configured cases cannot drift apart.

**The gradient is kept, as the layer beneath the painting.** It is what renders if the image has
not loaded or 404s, and it is the only part of the backdrop whose luminance is bounded by
arithmetic — so `TestVeilContrast` still proves the no-image case exhaustively, which no
image-based test can do.

**The naming argument reverses cleanly.** The original justified the gradient as aerial
perspective, "the defining device of the veduta genre this project is named after". A veduta is
not a device, it is a painting; two of them are now what the preset shows.

**Toning is baked into the files, not applied in CSS.** Each painting is desaturated and
contrast-compressed toward its scheme's scrim colour at encode time. A `filter` on the root would
cost a composited layer on every paint, and — the deciding half — could not be asserted. A baked
file can: `TestBundledBackdropTone` measures the *composited* backdrop's luminance span against
the gradient it replaced, so "calm enough to sit behind text" is a property of the repository
rather than a judgement someone once made in an image editor.

### What did not change

No user-authored CSS. No theme editor. No public knobs for surface alpha, scrim, border alpha,
blur radius. No server-side storage of anyone's preference. The contrast matrix, and the
worst-case evaluation that backs it for any image, are the same.

## Revisit triggers

- **Public appearance options** are reconsidered when users ask for a specific
  choice, one option at a time, expressed as intent. Not before, and not as a
  batch.
- **Per-user appearance** becomes worth building if multi-user deployments
  report that one instance serves audiences with genuinely different needs — a
  high-contrast requirement being the strongest case, since that is
  accessibility rather than taste.
- **A preview UI** is revisited once there are several supported options.
- **User-authored CSS** is reconsidered only behind a documented threat model
  covering exfiltration and UI obscuration, and plausibly only for a
  single-user deployment mode. No such mode exists today.
- **A third preset** is the point to check that two presets have not quietly
  become a styling language. If a preset needs a semantic token the others
  cannot express, the shared vocabulary is wrong — but if it only needs its own
  effect mechanics, that is working as intended.
