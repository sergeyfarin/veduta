// SPDX-License-Identifier: AGPL-3.0-or-later

package notices_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/notices"
)

// The embedded copy exists only because //go:embed cannot reach the repository root. If the two
// diverge, the binary would disclose something different from what the repository publishes -
// which is the whole failure this package is meant to prevent.
func TestEmbeddedNoticesMatchRepositoryRoot(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "THIRD-PARTY-NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	if notices.Markdown() != string(root) {
		t.Error("internal/notices/NOTICES.md differs from THIRD-PARTY-NOTICES.md; " +
			"run go run ./hack/gen-third-party-notices.go")
	}
}

// A binary that ships an empty or truncated notices file satisfies no licence. Assert the
// substance is present rather than merely that the embed compiled.
func TestEmbeddedNoticesCoverEveryShippedComponent(t *testing.T) {
	got := notices.Markdown()
	for _, want := range []string{
		"# Third-party notices",
		"## Frontend bundle",
		"## WebAssembly plugins",
		"## Go binary",
		"## Embedded icon pack",
		"svelte",       // the one package actually in the bundle
		"extism-pdk",   // the plugin ABI crate
		"wazero",       // the Wasm runtime in the Go binary
		"Simple Icons", // CC0 art whose trademark caveat must travel with it
	} {
		if !strings.Contains(got, want) {
			t.Errorf("embedded notices are missing %q", want)
		}
	}
}
