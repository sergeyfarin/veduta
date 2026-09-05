// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretRevealAllowlist is exactly the three consumer adapters docs/01-architecture.md section 2
// names: internal/connections (upstream auth injection), internal/notify (channel tokens) and
// internal/auth (the admin password hash), plus internal/secrets itself (its own tests construct
// and reveal values directly). None of the three consumer packages exist yet (Phases D/F/H) -
// this allowlist is ready for them, not a reflection of what exists today.
var secretRevealAllowlist = []string{
	filepath.Join("internal", "connections"),
	filepath.Join("internal", "notify"),
	filepath.Join("internal", "auth"),
	filepath.Join("internal", "secrets"),
	// This file's own source necessarily contains the literal string ".Reveal(" to describe and
	// search for it - not a real call site.
	filepath.Join("internal", "contracts"),
}

// TestSecretRevealBoundary enforces that a resolved secret's real value is reachable from
// exactly the packages docs/01 names, not from anywhere a plugin's output or a stray log call
// could be built. It is a source-text scan, not an import-graph check, deliberately: a package
// that imports internal/secrets only to pass a Value along (never calling Reveal) is fine and
// common, so the thing worth catching is the literal call, not the import.
func TestSecretRevealBoundary(t *testing.T) {
	root := repoRoot(t)
	var violations []string

	for _, base := range []string{"cmd", "internal"} {
		dir := filepath.Join(root, base)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if allowed(rel) {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(body), ".Reveal(") {
				violations = append(violations, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("only %v may call .Reveal() on a secrets.Value; found it in: %v", secretRevealAllowlist, violations)
	}
}

func allowed(relPath string) bool {
	dir := filepath.Dir(relPath)
	for _, a := range secretRevealAllowlist {
		if dir == a || strings.HasPrefix(dir, a+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
