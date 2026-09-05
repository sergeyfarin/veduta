// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath_test

import (
	"testing"

	"veduta.dev/veduta/internal/connections/routepath"
)

func TestMatch_Literal(t *testing.T) {
	if !routepath.Match("/api/v1/stats", "/api/v1/stats") {
		t.Fatal("identical literal paths should match")
	}
	if routepath.Match("/api/v1/stats", "/api/v1/other") {
		t.Fatal("different literal paths should not match")
	}
}

func TestMatch_StarWithinSegment(t *testing.T) {
	if !routepath.Match("/items/*", "/items/5") {
		t.Fatal("/items/* should match /items/5")
	}
	if !routepath.Match("/items/*", "/items/") {
		t.Error("bare * should match an empty remainder too (zero or more)")
	}
}

// TestMatch_StarNeverCrossesSlash is D1b's own AC: "* never crosses /."
func TestMatch_StarNeverCrossesSlash(t *testing.T) {
	if routepath.Match("/items/*", "/items/5/details") {
		t.Fatal("* must not match across a '/' - /items/* must not match /items/5/details")
	}
}

func TestMatch_StarPrefixSuffix(t *testing.T) {
	if !routepath.Match("/user-*-profile", "/user-42-profile") {
		t.Fatal("prefix*suffix should match")
	}
	if routepath.Match("/user-*-profile", "/user-42-details") {
		t.Fatal("wrong suffix should not match")
	}
}

func TestMatch_MultipleStarsInOneSegment(t *testing.T) {
	if !routepath.Match("/a*b*c", "/aXbYc") {
		t.Fatal("a*b*c should match aXbYc")
	}
	if !routepath.Match("/a*b*c", "/abc") {
		t.Fatal("a*b*c should match abc (each * can match zero characters)")
	}
	if routepath.Match("/a*b*c", "/acb") {
		t.Fatal("a*b*c should not match acb (b must appear after a's match, before c's)")
	}
}

func TestMatch_DifferentSegmentCountNeverMatches(t *testing.T) {
	if routepath.Match("/a/*", "/a") {
		t.Fatal("a pattern with more segments than the path must not match")
	}
	if routepath.Match("/a", "/a/b") {
		t.Fatal("a path with more segments than the pattern must not match")
	}
}

func TestMatch_BareStarMatchesWholeSegment(t *testing.T) {
	if !routepath.Match("/*", "/anything") {
		t.Fatal("/* should match any single segment")
	}
}
