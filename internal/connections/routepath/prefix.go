// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath

import "strings"

// HasPathPrefix reports whether canonicalPath falls under canonicalPrefix as a path *subtree* -
// exact equality, or canonicalPrefix followed by a "/" segment boundary. Both arguments must
// already be canonicalised (Canonicalise) by the caller, same as Match.
//
// A plain strings.HasPrefix is the wrong primitive here and was, for a time, what this project
// used for a connection's allowedPaths: it treats "/api" as a prefix of "/apievil", silently
// granting a path the administrator never intended to allow. Every "is this path under that
// allowed subtree" check in this project must go through this function instead of reimplementing
// the boundary rule inline.
func HasPathPrefix(canonicalPath, canonicalPrefix string) bool {
	if canonicalPrefix == "/" || canonicalPath == canonicalPrefix {
		return true
	}
	trimmed := strings.TrimSuffix(canonicalPrefix, "/")
	return strings.HasPrefix(canonicalPath, trimmed+"/")
}
