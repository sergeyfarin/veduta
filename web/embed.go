// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web carries the compiled dashboard so the binary is self-contained: one file to copy,
// no Node at runtime.
//
// The build output is generated, but //go:embed fails to compile when nothing matches, so
// web/build/.gitkeep is committed. Vite writes to build/app rather than build/ precisely because
// it empties its output directory on every build, dotfiles included. On a clean clone the
// embedded filesystem therefore holds no index.html, and Assets reports that rather than serving
// a blank page - see api.staticHandler.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var buildFS embed.FS

// Assets returns the compiled SPA rooted at build/, and whether it actually contains one.
func Assets() (fs.FS, bool) {
	sub, err := fs.Sub(buildFS, "build/app")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return sub, false
	}
	return sub, true
}
