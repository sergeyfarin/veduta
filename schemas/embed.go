// SPDX-License-Identifier: Apache-2.0

// Package schemas embeds the JSON Schema files so the compiled binary is self-contained - no
// runtime filesystem path, consistent with every other embedded asset in this project (see
// web/embed.go). Apache-2.0, not AGPL: third parties must be free to build compatible tooling,
// editors and validators against these files (LICENSING.md).
package schemas

import "embed"

// FS holds every JSON Schema file in this directory, embedded at build time.
//
//go:embed *.schema.json
var FS embed.FS
