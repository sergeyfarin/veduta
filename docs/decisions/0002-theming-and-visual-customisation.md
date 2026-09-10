# Theming: bounded presets and typed knobs, no user-authored CSS

Status: accepted, 2026-09-10. Supersedes the "themes are CSS files" line in
[01-architecture.md](../01-architecture.md) §13. Revisit after 0.2 only if a
trigger below is met.

## Decision

Ship theming as **data, not files, and not code**. A theme is a named preset of
design-token values. The dashboard picks one, and a small set of typed knobs
tunes it. No user-authored CSS, ever, and no server-side theme editor.

Concretely, seven decisions:

1. **A theme is a named set of token values.** Not a CSS file, not JS. This
   replaces §13's "Themes are CSS files, not JS", which predates the token layer
   actually existing and is the wrong shape for a config-as-code product.
2. **Clean is the degenerate case of the same vocabulary.** Surface alpha 100%,
   blur 0, no background image. Veil — the translucent, gethomepage-flavoured
   look — is the identical vocabulary with different numbers. One mechanism, not
   two, and no cascade-conflict question between "the theme's opacity" and
   "your opacity".
3. **`dashboard.theme` becomes a validated enum and is actually wired up.** It
   currently exists in the published v1 schema as a free-form `"string"` that
   nothing reads; see Evidence.
4. **The light/dark axis stays per-viewer; the theme is per-instance.** Light
   and dark remain a browser-local preference in `localStorage`, as today. The
   theme and its knobs are deployment identity and live in config. No per-user
   theme storage, and therefore no new persistence layer and no question about
   whether a theme is an admin operation.
5. **The UI previews and emits YAML; it never persists.** A theme picker with
   live preview that produces a config fragment to paste. This is not a new
   position — it is D5 ("YAML is the single source of truth ... avoids duelling
   stores; GUI is additive later") applied to appearance.
6. **Every knob is constrained so that no combination can produce an
   inaccessible dashboard**, enforced by a semantic check at config-load time in
   the shape of `validateAuthNone` — a `config.Diagnostic` surfaced by
   `--check-config`, not a runtime surprise.
7. **Sequencing: the enum and Veil first, the knob vocabulary second.** Build
   Veil with hardcoded token values, observe which tokens actually differ from
   Clean, and promote *those* to typed config. Knob names in a published v1
   schema are permanent; discovering them beats guessing them.

### Knob constraints that follow

- **Accent is a pair, or a name that maps to a pair.** Never a single hex.
- **Font is an enum of bundled stacks, each defining both the text face and the
  numeric face.** No free strings, so no webfont URL, so no outbound request
  from a viewer's browser to a third-party host — a privacy regression a
  self-hosted product must not ship. The paired numeric face preserves the
  tabular-numeral guarantee `--v-font-num` exists for.
- **A background image makes a scrim mandatory**, and contrast is computed
  against the scrim's guaranteed luminance floor rather than against the photo.
  This is what makes the check possible in Go at all.
- **Blur is applied conditionally, never as `blur(0)`.** A zero-radius
  `backdrop-filter` still creates a stacking context and forces GPU compositing
  per card; Clean must not pay Veil's cost on the Pi-class and tablet hardware
  this product targets.

## Evidence from this codebase

**The token layer is ready; the enforcement layer is not.** Everything visual
already routes through ~15 custom properties in `web/src/styles/tokens.css`, and
`TestComponentsUseTokensNotHardcodedColours`
(`internal/contracts/contrast_test.go`) mechanically forbids a colour literal
anywhere in `web/src` outside that one file. That is why presets-as-data is
cheap. But the contrast parser is hex-only — `(--v-[a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\s*;`
— so the moment `--v-surface` becomes translucent the token is dropped and
`TestTokenContrast` fails with "token missing". It fails loudly, which is
correct; the danger is that the cheap fix is to stop checking that pair. The
parser must be generalised to run per theme *before* Veil lands, not after.

**A single accent hex cannot serve both schemes — measured, not assumed.** The
shipped light accent `#2f6f4f` scores **2.99:1** against the dark surface
`#17171a`, one hundredth below the 3:1 floor `TestTokenContrast` already
enforces. The dark accent `#6fb894` scores **2.34:1** on white. Sweeping the
sRGB cube, only **32.8%** of colours clear 3:1 against *both* surfaces, against
57.3% for the light surface alone: a free colour picker hands the user a failing
dashboard roughly two times in three. A single accent is not impossible — the
best available is about 4.23:1 on its worse side — but the safe region is too
narrow to expose as an unconstrained control.

**Translucency over photographs is the `.dimmed` bug with an unknown backdrop.**
`--v-faint` sits at 4.71:1 on `--v-surface`, 0.21 above the AA floor, which is
why `TestStaleStateContrast` exists: any opacity reduction on that token, even
0.9, pushes it under. A translucent card over a user-supplied image is strictly
worse, because the effective backdrop is unknown at build time. The project has
already solved this once — `--v-overlay-text` and `--v-overlay-scrim` are
documented as needing to stay legible "against ANY photo in either theme" — and
that scrim is the mechanism Veil reuses to make contrast computable again.

**`dashboard.theme` is a live, unvalidated commitment.** It is present in
`schemas/config.v1.schema.json` as a bare `"string"` with no enum, parsed into
`config.Theme` in `internal/config/types.go`, and read by nothing: the frontend
selects only light/dark/auto, from `localStorage`. Narrowing a published v1
field later is a breaking change, so the enum is added now, while in practice no
deployment can depend on a field that does nothing.

**The persistence question is already answered.** Config is hand-authored,
comment-bearing YAML validated, activated and rolled back as a generation. A
server that rewrites it to save a theme would destroy the user's comments;
writing to the SQLite `settings` table instead would create the duelling store
D5 exists to prevent. Neither is necessary once the UI emits YAML instead of
storing it.

## Options declined

- **User-authored CSS, in a textarea or a file.** Unversionable, unvalidatable,
  and a real surface: CSS can exfiltrate via `background: url()` with attribute
  selectors and can obscure the persistent authentication-disabled warning H2
  deliberately added. `{@html}` is banned outright; this is the same argument one
  step weaker, and the same answer.
- **A server-side "save as new theme" button.** Rejected on D5 grounds above.
  Saving a theme is something the user already does — in their config file, in
  git. Drag-and-drop persistence of layout and appearance is Homarr's turf, per
  §13.
- **macOS-like and Windows-like themes.** Three independent reasons. They are
  design languages, not palettes, so a token swap yields "clean, but bluer" and
  invites the complaint that it does not look like the thing. Their fonts are
  not redistributable, and `--v-font` already begins
  `system-ui, -apple-system, "Segoe UI"` — so on a Mac the Clean theme *already*
  renders in the macOS system font, making a macOS theme that only looks right
  on a Mac self-cancelling. And shipping themes under those names weeks after
  adopting a trademark policy is an avoidable own-goal. Equivalent flavours may
  ship under project-owned names.
- **Themes as separate CSS files.** Workable, but it splits the vocabulary
  across files, makes "theme plus knob overrides" a cascade-ordering question,
  and cannot be validated by the config layer. Data can.
- **Per-user themes.** Deferred, not refused. It requires answering whether a
  theme is an admin operation or a viewer preference, and adds per-user storage,
  for a product whose light/dark axis is already per-viewer and whose visual
  identity is reasonably a property of the deployment.
- **Freezing the full knob vocabulary in this pass.** Declined on sequencing
  grounds; see decision 7 and the backlog entry it references.

## Revisit triggers

- **Per-user themes** become worth building if multi-user deployments report
  that one instance genuinely serves audiences with different needs — a
  high-contrast requirement being the strongest case, since that is
  accessibility, not taste.
- **The knob vocabulary is reopened** once Veil exists and the set of tokens
  that actually differ between Clean and Veil is known. The current guess —
  accent, font, background image, card opacity — is explicitly a guess; the real
  set likely includes surface alpha, blur radius, scrim floor, border alpha and
  shadow.
- **User-authored CSS is reconsidered** only behind a documented threat model
  covering exfiltration and UI obscuration, and only for a single-user
  deployment mode. No such mode exists today.
- **A third shipped theme** would be the point to check that presets-as-data has
  not quietly grown into a styling language. If a theme needs a token the other
  themes cannot express, the vocabulary is wrong, not the theme.
