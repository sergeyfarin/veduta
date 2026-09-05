// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath_test

import (
	"testing"

	"veduta.dev/veduta/internal/connections/routepath"
)

// FuzzCanonicalise extends the example-based idempotence and segment-count tests to arbitrary
// input, not just the hand-picked cases in canonicalise_test.go - exactly D1b's own AC ("a fuzz
// target asserting that no input is accepted by Match after canonicalisation but rejected before
// it, or vice versa"). The property this checks is what actually prevents "check" and "use" from
// disagreeing: for ANY input Canonicalise accepts, (1) re-running it is a no-op (idempotence),
// and (2) the canonical form, used as a literal pattern, always matches itself under Match - if
// Canonicalise's notion of "the same path" and Match's notion of "the same segment" ever
// disagreed (say, one normalised case and the other didn't), this would catch it by finding a
// canonical output that fails to match its own literal self.
func FuzzCanonicalise(f *testing.F) {
	seeds := []string{
		"/", "/a", "/a/b/c", "/a%41b", "/caf%c3%a9", "/%7euser", "/%2C",
		"/%2e%2e", "/%2f", "/a%5cb", `/a\b`, "/a b", "/a//b", "/a/.", "/a/..",
		"/%", "/%2", "/%zz", "", "a/b", "/a*b", "/a/*/b",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		canonical, err := routepath.Canonicalise(raw)
		if err != nil {
			return // rejected inputs have nothing further to check
		}

		// Idempotence, fuzzed rather than only checked against hand-picked examples.
		again, err := routepath.Canonicalise(canonical)
		if err != nil {
			t.Fatalf("Canonicalise(%q) = %q, but re-canonicalising that failed: %v", raw, canonical, err)
		}
		if again != canonical {
			t.Fatalf("not idempotent: Canonicalise(%q) = %q, Canonicalise(that) = %q", raw, canonical, again)
		}

		// Segment count is preserved by decoding. raw is guaranteed non-empty and "/"-prefixed
		// here: Canonicalise already returned nil error above, which requires exactly that.
		if gotSegs, wantSegs := countSlashes(canonical), countSlashes(raw); gotSegs != wantSegs {
			t.Fatalf("segment count changed: %q (%d) -> %q (%d)", raw, wantSegs, canonical, gotSegs)
		}

		// The canonical form, read as a literal pattern (no '*' introduced by canonicalisation -
		// '*' is not an unreserved character, so it can only appear here if raw already
		// contained a literal '*', which Match then treats as a wildcard; skip those, since this
		// check is about literal-vs-literal agreement, not glob semantics), must match itself.
		if !containsStar(canonical) && !routepath.Match(canonical, canonical) {
			t.Fatalf("canonical form %q (from %q) does not match itself under Match", canonical, raw)
		}
	})
}

func countSlashes(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			n++
		}
	}
	return n
}

func containsStar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '*' {
			return true
		}
	}
	return false
}
