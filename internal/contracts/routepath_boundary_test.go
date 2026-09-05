// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pathHandlingCalls matches the standard-library functions that decode, clean or compare a path
// - exactly what docs/02-implementation-plan.md's D1b milestone means by "path comparison or
// unescaping". Matched by call syntax (name followed by "("), not by import, so a package that
// imports "path" or "net/url" for an unrelated reason (building a URL, say) is not flagged for
// merely importing it.
var pathHandlingCalls = regexp.MustCompile(`\b(path\.(?:Join|Clean|Split)|filepath\.(?:Join|Clean)|url\.PathUnescape|url\.PathEscape)\(`)

// routepathAllowlist is every current call outside internal/connections/routepath itself, each
// with why it is not a routepath violation: milestone D1b's six call sites
// (docs/01-architecture.md's "Route canonicalisation" section) are about the AUTHORITY a
// connection's upstream request path carries - which upstream route a plugin is allowed to
// reach. None of these are that: they are either local filesystem paths (config files, secret
// files - a completely different kind of "path"), or the embedded SPA's own static-file
// traversal guard (serving this binary's OWN bundled frontend assets, not a connection).
var routepathAllowlist = map[string]string{
	filepath.Join("internal", "api", "static.go"):       "SPA static file serving (embedded frontend assets) - not a connection's route",
	filepath.Join("internal", "config", "load.go"):      "local filesystem paths: the config file and its conf.d directory",
	filepath.Join("internal", "config", "watch.go"):     "local filesystem paths: the watched config file and conf.d directory",
	filepath.Join("internal", "secrets", "provider.go"): "local filesystem path: the file: secret provider's own directory",
}

// TestRoutepathBoundary is D1b's own AC: "no other package in the tree performs path comparison
// or unescaping." A hit in a file not in routepathAllowlist means either a new call site should
// route through internal/connections/routepath instead, or (if it is genuinely unrelated, like
// the entries above) the allowlist needs a new entry recording why - either way, a deliberate
// decision, not a silent pass.
func TestRoutepathBoundary(t *testing.T) {
	root := repoRoot(t)
	routepathDir := filepath.Join("internal", "connections", "routepath")

	for _, base := range []string{"cmd", "internal"} {
		dir := filepath.Join(root, base)
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if strings.HasPrefix(rel, routepathDir+string(filepath.Separator)) {
				return nil // the package itself
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !pathHandlingCalls.Match(body) {
				return nil
			}
			if _, allowed := routepathAllowlist[rel]; !allowed {
				t.Errorf("%s: calls a path-handling function not routed through "+
					"internal/connections/routepath, and is not in routepathAllowlist", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// The allowlist itself must not silently accumulate stale entries for files that no longer
	// exist or no longer make the matched call - each entry is a claim about real code.
	for rel := range routepathAllowlist {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("routepathAllowlist entry %q: %v", rel, err)
			continue
		}
		if !pathHandlingCalls.Match(body) {
			t.Errorf("routepathAllowlist entry %q no longer calls a path-handling function - remove the entry", rel)
		}
	}
}
