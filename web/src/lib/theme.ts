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
