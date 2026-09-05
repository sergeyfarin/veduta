// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath

import "strings"

// Match reports whether path (already canonical - see Canonicalise) matches pattern, a glob
// already validated against the schema's own pattern syntax (plugin-manifest.v1's `path` field:
// `*` matches within a single segment and never crosses `/`; multi-segment `**` is not part of
// v1 at all - docs/01-architecture.md's own words). Match does not canonicalise either argument
// itself: callers run both through Canonicalise first, so this is pure segment-by-segment
// comparison, nothing more.
func Match(pattern, path string) bool {
	patternSegs := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	pathSegs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(patternSegs) != len(pathSegs) {
		return false
	}
	for i, ps := range patternSegs {
		if !matchSegment(ps, pathSegs[i]) {
			return false
		}
	}
	return true
}

// matchSegment matches one pattern segment against one path segment with ordinary '*'-only glob
// semantics (no '?', no character classes - the schema's pattern syntax has neither): split the
// pattern on every '*' into literal parts, anchor the first part to the segment's start and the
// last to its end, then require the middle parts to appear in the remainder in order. '*' never
// crosses a '/' because this function is only ever given one segment at a time - Match already
// split both sides on '/' before calling it.
func matchSegment(pattern, seg string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == seg
	}
	parts := strings.Split(pattern, "*")
	first, last := parts[0], parts[len(parts)-1]

	if !strings.HasPrefix(seg, first) {
		return false
	}
	seg = seg[len(first):]
	if !strings.HasSuffix(seg, last) {
		return false
	}
	seg = seg[:len(seg)-len(last)]

	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue // consecutive '*'s, or one immediately after the leading part
		}
		idx := strings.Index(seg, part)
		if idx == -1 {
			return false
		}
		seg = seg[idx+len(part):]
	}
	return true
}
