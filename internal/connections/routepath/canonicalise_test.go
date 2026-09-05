// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath_test

import (
	"errors"
	"testing"

	"veduta.dev/veduta/internal/connections/routepath"
)

func TestCanonicalise_Valid(t *testing.T) {
	cases := map[string]string{
		"/":             "/",
		"/a":            "/a",
		"/a/b/c":        "/a/b/c",
		"/api/v1/stats": "/api/v1/stats",
		"/a%41b":        "/aAb",       // %41 = 'A', unreserved: decoded literally
		"/a~b_c-d.e":    "/a~b_c-d.e", // unreserved chars pass through unencoded
		"/%7euser":      "/~user",     // %7e = '~', unreserved
		"/caf%c3%a9":    "/caf%c3%a9", // %c3%a9 decode to non-ASCII bytes: not unreserved, kept encoded, lowercased
		"/%2C":          "/%2c",       // ',' (0x2C) is reserved: stays encoded, hex lowercased
	}
	for in, want := range cases {
		got, err := routepath.Canonicalise(in)
		if err != nil {
			t.Errorf("Canonicalise(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Canonicalise(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCanonicalise_RejectsAdversarialInputs is D1b's own mandatory test list, verbatim:
// %2e%2e, %2E%2E, %2f, %5c, literal backslash, control characters, malformed escapes, //,
// "." and ".." segments.
func TestCanonicalise_RejectsAdversarialInputs(t *testing.T) {
	cases := map[string]error{
		"/%2e%2e":     routepath.ErrDotSegment,
		"/%2E%2E":     routepath.ErrDotSegment,
		"/a/%2e%2e/b": routepath.ErrDotSegment,
		"/a/%2f":      routepath.ErrEncodedSeparator,
		"/a/%2F":      routepath.ErrEncodedSeparator,
		"/a%5cb":      routepath.ErrEncodedSeparator,
		"/a%5Cb":      routepath.ErrEncodedSeparator,
		`/a\b`:        routepath.ErrBackslash,
		"/a\x00b":     routepath.ErrControlOrSpace,
		"/a\tb":       routepath.ErrControlOrSpace,
		"/a\nb":       routepath.ErrControlOrSpace,
		"/a b":        routepath.ErrControlOrSpace,
		"/a\x7fb":     routepath.ErrControlOrSpace,
		"/a%":         routepath.ErrMalformedEscape,
		"/a%2":        routepath.ErrMalformedEscape,
		"/a%zz":       routepath.ErrMalformedEscape,
		"/a%2g":       routepath.ErrMalformedEscape,
		"/a//b":       routepath.ErrEmptySegment,
		"//":          routepath.ErrEmptySegment,
		"/a/":         routepath.ErrEmptySegment,
		"/.":          routepath.ErrDotSegment,
		"/..":         routepath.ErrDotSegment,
		"/a/.":        routepath.ErrDotSegment,
		"/a/..":       routepath.ErrDotSegment,
		"/a/./b":      routepath.ErrDotSegment,
		"/a/../b":     routepath.ErrDotSegment,
		"a/b":         routepath.ErrNotAbsolute,
		"":            routepath.ErrNotAbsolute,
	}
	for in, wantErr := range cases {
		_, err := routepath.Canonicalise(in)
		if !errors.Is(err, wantErr) {
			t.Errorf("Canonicalise(%q) error = %v, want %v", in, err, wantErr)
		}
	}
}

// TestCanonicalise_Idempotent is D1b's own AC: Canonicalise(Canonicalise(x)) == Canonicalise(x).
func TestCanonicalise_Idempotent(t *testing.T) {
	inputs := []string{
		"/a/b/c", "/a%41b", "/caf%c3%a9", "/%7euser", "/%2C", "/a~b_c-d.e", "/",
	}
	for _, in := range inputs {
		once, err := routepath.Canonicalise(in)
		if err != nil {
			t.Fatalf("Canonicalise(%q): %v", in, err)
		}
		twice, err := routepath.Canonicalise(once)
		if err != nil {
			t.Fatalf("Canonicalise(%q) (the once-canonicalised form) failed: %v", once, err)
		}
		if once != twice {
			t.Errorf("not idempotent: Canonicalise(%q) = %q, Canonicalise(that) = %q", in, once, twice)
		}
	}
}

// TestCanonicalise_DecodingNeverChangesSegmentCount is D1b's own AC.
func TestCanonicalise_DecodingNeverChangesSegmentCount(t *testing.T) {
	inputs := []string{"/a/b/c", "/a%41b/c", "/caf%c3%a9/x/y", "/%7e/a/b/c/d"}
	for _, in := range inputs {
		out, err := routepath.Canonicalise(in)
		if err != nil {
			t.Fatalf("Canonicalise(%q): %v", in, err)
		}
		if wantSegs, gotSegs := segmentCount(in), segmentCount(out); wantSegs != gotSegs {
			t.Errorf("segment count changed: %q (%d segments) -> %q (%d segments)", in, wantSegs, out, gotSegs)
		}
	}
}

func segmentCount(p string) int {
	n := 0
	for _, r := range p {
		if r == '/' {
			n++
		}
	}
	return n
}
