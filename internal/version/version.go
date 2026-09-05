// SPDX-License-Identifier: AGPL-3.0-or-later

// Package version carries build identity, including the AGPL section 13 source URL for the
// exact commit this binary was built from (see LICENSING.md).
package version

var (
	// Version is the release version, set by the linker at build time.
	Version = "0.0.0-dev"
	// Commit is the git revision this binary was built from.
	Commit = "unknown"
	// SourceURL points at that exact revision. AGPL section 13 requires that anyone
	// interacting with a running instance over a network be offered the corresponding
	// source; the UI footer and /api/v1/version both surface this.
	SourceURL = "https://github.com/sergeyfarin/veduta"
)

// Info is the payload returned by GET /api/v1/version.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	SourceURL string `json:"sourceUrl"`
}

// Current returns the build identity of this binary.
func Current() Info {
	return Info{Version: Version, Commit: Commit, SourceURL: SourceURL + "/tree/" + Commit}
}
