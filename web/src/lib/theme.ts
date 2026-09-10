/** Theme preference: an explicit choice, or following the system. */
export type Theme = 'auto' | 'light' | 'dark';

const KEY = 'veduta.theme';

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
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // A remembered theme is a convenience, never a requirement.
  }
}

export function next(theme: Theme): Theme {
  return theme === 'auto' ? 'light' : theme === 'light' ? 'dark' : 'auto';
}

/**
 * Applies the instance's visual preset as data-appearance on <html>, the attribute tokens.css
 * keys its preset blocks off. Clean is the absence of the attribute, not a value: a preset owns
 * effect mechanics Clean must not pay for at all - a zero-radius backdrop-filter still creates a
 * stacking context and forces GPU compositing on every card - so Clean must not match a rule that
 * sets one. See docs/decisions/0002-theming-and-visual-customisation.md.
 *
 * Unlike the light/dark preference this is NOT stored: it is instance configuration, arriving
 * with the layout on every load, so there is nothing viewer-local to remember.
 */
export function applyAppearance(appearance: string | undefined): void {
  const root = document.documentElement;
  if (appearance === 'veil') {
    root.dataset.appearance = 'veil';
  } else {
    delete root.dataset.appearance;
  }
}
