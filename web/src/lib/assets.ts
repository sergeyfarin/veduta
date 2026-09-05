// SPDX-License-Identifier: AGPL-3.0-or-later

// Turns a Widget Document Image.ref (a broker-minted, opaque signed token - never a URL an
// integration constructed itself, see docs/01-architecture.md section 7) into the actual
// browser-fetchable address. One function, so the day the asset endpoint's path changes there is
// exactly one place to update.
//
// The endpoint itself does not exist yet (it arrives in milestone E1); until then every image in
// this build 404s, which is the expected and correct behaviour for the broken-image state this
// milestone also implements - a real backend failure looks exactly like this.
export function assetUrl(ref: string): string {
  return `/api/v1/assets/${encodeURIComponent(ref)}`;
}

/** Converts the schema's "W:H" aspect string (e.g. "2:3") to a CSS aspect-ratio value ("2 / 3").
 * Returns undefined for an absent or malformed value, so a caller can fall back to its own
 * per-block-kind default rather than render a broken CSS declaration. */
export function cssAspectRatio(aspect: string | undefined): string | undefined {
  if (!aspect) return undefined;
  const m = aspect.match(/^(\d+):(\d+)$/);
  if (!m) return undefined;
  return `${m[1]} / ${m[2]}`;
}
