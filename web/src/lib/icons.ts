// SPDX-License-Identifier: AGPL-3.0-or-later

const namedIcon = /^(mdi|si|sh):[a-z0-9][a-z0-9-]{0,63}$/;

/** Returns the core-owned icon URL, or undefined when the value is a literal glyph/initials. */
export function iconSource(spec: string): string | undefined {
  if (!namedIcon.test(spec) && !spec.startsWith('https://') && !spec.startsWith('http://')) {
    return undefined;
  }
  return `/api/v1/icons/${encodeURIComponent(spec)}`;
}

export function iconFallback(spec: string): string {
  const named = spec.match(namedIcon);
  if (named) {
    return spec.slice(spec.indexOf(':') + 1).replaceAll('-', ' ').slice(0, 2).toUpperCase();
  }
  if (spec.startsWith('http://') || spec.startsWith('https://')) {
    return '•';
  }
  return spec;
}
