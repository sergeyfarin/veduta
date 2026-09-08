// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// expectedSPDX maps a top-level directory to the licence its sources carry. The split is the one
// in LICENSING.md: the core is AGPL, while the SDKs, schemas and first-party integrations are
// Apache-2.0 so that writing an integration never raises a licensing question.
var expectedSPDX = map[string]string{
	"cmd":      "AGPL-3.0-or-later",
	"internal": "AGPL-3.0-or-later",
	"plugins":  "Apache-2.0",
	"sdk":      "Apache-2.0",
}

func TestSourceFilesCarrySPDXHeaders(t *testing.T) {
	root := repoRoot(t)
	for dir, want := range expectedSPDX {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "target" {
					return filepath.SkipDir
				}
				return nil
			}
			if ext := filepath.Ext(path); ext != ".go" && ext != ".rs" {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			line := "// SPDX-License-Identifier: " + want
			if !strings.HasPrefix(string(body), line) {
				t.Errorf("%s: first line must be %q", rel, line)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

var goModRequire = regexp.MustCompile(`(?m)^\s*([\w./~-]+\.[\w./~-]+/[\w./~-]+|[\w./~-]+\.[\w./~-]+)\s+v[\w.+-]+`)

// TestDependenciesAreRecorded makes adding a module a deliberate act: the licence has to be
// written down, where a human can see whether it is compatible with AGPL-3.0-or-later. The
// budget is about ten direct modules, and a silent import is how budgets stop meaning anything.
func TestDependenciesAreRecorded(t *testing.T) {
	root := repoRoot(t)
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	licenses, err := os.ReadFile(filepath.Join(root, "THIRD-PARTY-LICENSES.md"))
	if err != nil {
		t.Fatal(err)
	}
	recorded := string(licenses)

	var modules []string
	for _, line := range strings.Split(string(gomod), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "module ") ||
			strings.HasPrefix(line, "go ") || strings.HasPrefix(line, "require") ||
			strings.HasPrefix(line, ")") || strings.HasPrefix(line, "toolchain") {
			continue
		}
		if m := goModRequire.FindStringSubmatch(line); m != nil {
			modules = append(modules, m[1])
		}
	}
	if len(modules) == 0 {
		t.Fatal("parsed no modules from go.mod; the parser is broken, not the dependency list")
	}
	for _, mod := range modules {
		if !strings.Contains(recorded, mod) {
			t.Errorf("module %s is in go.mod but not recorded in THIRD-PARTY-LICENSES.md", mod)
		}
	}

	// Licences that cannot be combined into an AGPL-3.0-or-later work, or that are not open
	// source at all. Only TABLE ROWS are scanned: the document's own prose names these in order
	// to forbid them, and a checker that cannot tell a rule from an instance is not a checker.
	banned := []string{"GPL-2.0-only", "SSPL", "BUSL", "Business Source", "unlicensed"}
	for _, line := range strings.Split(recorded, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, b := range banned {
			if strings.Contains(line, b) {
				t.Errorf("THIRD-PARTY-LICENSES.md records %q in %q, which is incompatible with "+
					"the core licence", b, strings.TrimSpace(line))
			}
		}
	}
}
