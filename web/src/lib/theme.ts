/**
 * The two visual axes, and why they are two.
 *
 * Light/dark is a property of the room the viewer is sitting in; clean/veil is a property of the
 * instance's design. They are orthogonal - all four combinations are real and shipped - so
 * collapsing them into one four-valued control would have to drop something, and the thing it
 * would drop is "follow the system", which is the default and the only value that changes by
 * itself. Two controls, three values each.
 *
 * Both are per-viewer preferences in localStorage. They differ in what their 'auto' means: for the
 * colour scheme it is the browser's prefers-color-scheme, resolved by CSS; for the preset it is
 * dashboard.appearance, resolved here, because the server is the only one who knows it.
 */

/** Colour scheme preference: an explicit choice, or following the system. */
export type Theme = 'auto' | 'light' | 'dark';

/** Preset preference: an explicit choice, or following the instance's configuration. */
export type Appearance = 'auto' | 'clean' | 'veil';

const KEY = 'veduta.theme';
const APPEARANCE_KEY = 'veduta.appearance';

/**
 * The stored preference, or 'auto'. Storage can throw (private windows, blocked site data), and a
 * dashboard must render regardless, so failure means the default rather than a broken page.
 */
export function stored(): Theme {
  try {
    const v = localStorage.getItem(KEY);
    return v === 'light' || v === 'dark' || v === 'auto' ? v : 'auto';
  } catch {
    return 'auto';
  }
}

/** As stored(), for the preset axis. */
export function storedAppearance(): Appearance {
  try {
    const v = localStorage.getItem(APPEARANCE_KEY);
    return v === 'clean' || v === 'veil' || v === 'auto' ? v : 'auto';
  } catch {
    return 'auto';
  }
}

/**
 * Applies a preference. 'auto' removes the attribute entirely so the CSS falls through to
 * prefers-color-scheme - the tokens are written so that is the only thing distinguishing
 * "following the system" from an explicit choice.
 */
export function apply(theme: Theme): void {
  const root = document.documentElement;
  if (theme === 'auto') {
    delete root.dataset.theme;
  } else {
    root.dataset.theme = theme;
  }
  remember(KEY, theme);
}

/**
 * Applies the effective visual preset as data-appearance on <html>, the attribute tokens.css keys
 * its preset blocks off.
 *
 * `configured` is dashboard.appearance, which arrives with the layout on every load; `preference`
 * is the viewer's own override, and 'auto' defers to the instance. Unlike the colour scheme this
 * one cannot be resolved in CSS, because prefers-color-scheme has no equivalent for "whatever the
 * operator configured" - hence the resolution here.
 *
 * Clean is the ABSENCE of the attribute, not a value: a preset owns effect mechanics Clean must
 * not pay for at all - a zero-radius backdrop-filter still creates a stacking context and forces
 * GPU compositing on every card - so Clean must not match a rule that sets one. See
 * docs/decisions/0002-theming-and-visual-customisation.md.
 */
export function applyAppearance(
  preference: Appearance,
  configured: string | undefined,
  background = false
): void {
  const effective = preference === 'auto' ? configured : preference;
  const root = document.documentElement;
  if (effective === 'veil') {
    root.dataset.appearance = 'veil';
  } else {
    delete root.dataset.appearance;
  }
  // data-backdrop swaps the bundled painting for the configured image. The scrim that guarantees
  // contrast over an unknown image is in the CSS above both of them, not in a caller that could
  // forget to apply it - see tokens.css and TestVeilImageContrast.
  //
  // A viewer who turns Veil on for themselves on an instance configured clean gets the bundled
  // painting, because dashboard.background is refused without appearance: veil and so there is no
  // configured image to show. That is the schema's rule surfacing, not a special case here.
  if (effective === 'veil' && background) {
    root.dataset.backdrop = 'image';
  } else {
    delete root.dataset.backdrop;
  }
  remember(APPEARANCE_KEY, preference);
}

export function next(theme: Theme): Theme {
  return theme === 'auto' ? 'light' : theme === 'light' ? 'dark' : 'auto';
}

export function nextAppearance(appearance: Appearance): Appearance {
  return appearance === 'auto' ? 'clean' : appearance === 'clean' ? 'veil' : 'auto';
}

function remember(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // A remembered preference is a convenience, never a requirement.
  }
}
