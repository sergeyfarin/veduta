// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRawHTMLDirectiveInFrontend enforces the B3 milestone requirement directly: {@html}
// appears nowhere in web/. Every block a Widget Document can produce renders as real Svelte
// elements (see web/src/lib/blocks/), never as an injected HTML string - that is the whole
// security property markdown rendering depends on (see web/src/lib/markdown.ts's own
// commentary). This is checked here, in Go, rather than only as a shell grep in CI, so the same
// `go test ./...` that runs everywhere also runs this.
//
// A line is skipped if it is a `//` comment line (after trimming whitespace) - every legitimate
// mention of the string "{@html}" in this codebase is explanatory prose inside a comment, never
// real template markup, and template code is never preceded by `//` on the same line in
// practice. This is a heuristic, not a parser, and it is proven to actually catch a real
// directive by the mutation test alongside it.
func TestNoRawHTMLDirectiveInFrontend(t *testing.T) {
	root := repoRoot(t)
	webSrc := filepath.Join(root, "web", "src")
	found := 0
	err := filepath.WalkDir(webSrc, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".svelte") {
			return err
		}
		found++
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue // a comment line - see the heuristic note in the doc comment above
			}
			if strings.Contains(line, "{@html") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: {@html} directive found - every block renders as real Svelte "+
					"elements, never injected HTML. If a new block type genuinely needs raw HTML, "+
					"that is an architecture decision (docs/01-architecture.md section 8), not a "+
					"one-line addition.", rel, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("scanned zero .svelte files; the walk is broken, not the codebase")
	}
}
