// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Source is where a declared integration's manifest actually lives, resolved from
// config.Integration.Source ("builtin" or "path:./plugins/immich" - examples/veduta.yaml).
type Source struct {
	// Builtin integrations ship inside the signed binary and are exempt from the lock -
	// docs/01-architecture.md section 6: "they have no separate trust boundary." docker and
	// http-json are the only two today.
	Builtin bool
	// Dir is the manifest's directory, resolved relative to the primary config file - "" for a
	// builtin source.
	Dir string
}

// ResolveSource parses raw (a config.Integration.Source value) against configDir, the directory
// of the primary config file - `path:` sources are relative to the configuration, not the
// process's working directory, so `veduta integration approve` behaves the same run from any
// directory.
func ResolveSource(configDir, raw string) (Source, error) {
	if raw == "builtin" {
		return Source{Builtin: true}, nil
	}
	rest, ok := strings.CutPrefix(raw, "path:")
	if !ok || rest == "" {
		return Source{}, fmt.Errorf("integration source %q: expected \"builtin\" or \"path:<dir>\"", raw)
	}
	dir := rest
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(configDir, dir)
	}
	return Source{Dir: filepath.Clean(dir)}, nil
}
