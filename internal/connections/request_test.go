// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"net/url"
	"testing"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestJoinPath_Simple(t *testing.T) {
	base := mustParseURL(t, "http://example.com/api")
	got, err := joinPath(base, "/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "http://example.com/api/v1/stats" {
		t.Fatalf("got %s", got.String())
	}
}

func TestJoinPath_EmptyMeansRoot(t *testing.T) {
	base := mustParseURL(t, "http://example.com/api")
	got, err := joinPath(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "http://example.com/api" {
		t.Fatalf("got %s", got.String())
	}
}

// TestJoinPath_RejectsTraversal is the D1 AC: "path traversal rejected."
func TestJoinPath_RejectsTraversal(t *testing.T) {
	base := mustParseURL(t, "http://example.com/api")
	for _, p := range []string{
		"../secret",
		"../../etc/passwd",
		"/api/../../etc/passwd",
		"foo/../../bar",
	} {
		if _, err := joinPath(base, p); err == nil {
			t.Errorf("joinPath(%q) should have been rejected as traversal", p)
		}
	}
}

// TestJoinPath_RejectsAbsoluteURL is the D1 AC: "absolute URL in Path rejected."
func TestJoinPath_RejectsAbsoluteURL(t *testing.T) {
	base := mustParseURL(t, "http://example.com/api")
	for _, p := range []string{
		"http://evil.com/steal",
		"https://evil.com/steal",
		"//evil.com/steal",
		"ftp://evil.com/x",
	} {
		if _, err := joinPath(base, p); err == nil {
			t.Errorf("joinPath(%q) should have been rejected as an absolute URL", p)
		}
	}
}

func TestJoinPath_StaysWithinBaseWithSubpath(t *testing.T) {
	base := mustParseURL(t, "http://example.com/api/v2")
	got, err := joinPath(base, "/items/5")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/api/v2/items/5" {
		t.Fatalf("path = %q", got.Path)
	}
}

func TestJoinPath_PreservesQueryFromPath(t *testing.T) {
	base := mustParseURL(t, "http://example.com/api")
	got, err := joinPath(base, "/search?q=x")
	if err != nil {
		t.Fatal(err)
	}
	if got.RawQuery != "q=x" {
		t.Fatalf("query = %q", got.RawQuery)
	}
}

// TestJoinPath_RelativeEscapeCannotReachAboveBase covers the case path.Join alone would clean
// silently: joining a base of "/a" with "../../etc" cleans to "/etc" - not an error from
// path.Join itself, which is exactly why joinPath checks the result stays under base afterwards
// rather than trusting path.Join's cleaning to also be a security boundary.
func TestJoinPath_RelativeEscapeCannotReachAboveBase(t *testing.T) {
	base := mustParseURL(t, "http://example.com/a")
	if _, err := joinPath(base, "../../etc"); err == nil {
		t.Fatal("want an error: path.Join alone would silently clean this to /etc")
	}
}
