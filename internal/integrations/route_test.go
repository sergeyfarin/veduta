// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import "testing"

func TestCanonicalQueryKeys_NilVsEmptyDistinct(t *testing.T) {
	nilKeys := canonicalQueryKeys(nil)
	emptyKeys := canonicalQueryKeys([]string{})
	if nilKeys == emptyKeys {
		t.Fatalf("nil and empty queryKeys must canonicalise differently: both got %q", nilKeys)
	}
}

func TestCanonicalQueryKeys_OrderAndDupesIgnored(t *testing.T) {
	a := canonicalQueryKeys([]string{"b", "a", "a"})
	b := canonicalQueryKeys([]string{"a", "b"})
	if a != b {
		t.Fatalf("reordering/deduping should not change identity: %q != %q", a, b)
	}
}

func TestEffectiveMaxBodyKB(t *testing.T) {
	cases := []struct {
		name              string
		routeMax, reqBody int
		want              int
	}{
		{"route narrows", 4, 64, 4},
		{"manifest narrows core default", 0, 32, 32},
		{"nothing set falls back to core default", 0, 0, coreDefaultBodyKB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveMaxBodyKB(c.routeMax, c.reqBody); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestNormaliseContentType(t *testing.T) {
	a, err := normaliseContentType("application/json; charset=UTF-8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := normaliseContentType("Application/JSON;CHARSET=utf-8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != b {
		t.Fatalf("normalisation should be case-insensitive on type/subtype/charset: %q != %q", a, b)
	}
	if _, err := normaliseContentType("application/vnd.api+json; profile=x"); err != nil {
		t.Fatalf("params must be retained without error: %v", err)
	}
}

func TestNormaliseContentType_DistinguishesParams(t *testing.T) {
	base, err := normaliseContentType("application/json")
	if err != nil {
		t.Fatal(err)
	}
	withCharset, err := normaliseContentType("application/json; charset=utf-8")
	if err != nil {
		t.Fatal(err)
	}
	if base == withCharset {
		t.Fatal("application/json and application/json;charset=utf-8 must NOT compare equal - " +
			"docs/01-architecture.md warns an earlier draft wrongly conflated these")
	}
}

func TestNormaliseContentType_Malformed(t *testing.T) {
	if _, err := normaliseContentType("not a content type;;;"); err == nil {
		t.Fatal("expected an error for a malformed content type")
	}
}

func TestDedupeRoutes(t *testing.T) {
	routes := []Route{
		{Slot: "server", Method: "GET", Path: "/a"},
		{Slot: "server", Method: "GET", Path: "/a"},
		{Slot: "server", Method: "GET", Path: "/b"},
	}
	got := dedupeRoutes(routes)
	if len(got) != 2 {
		t.Fatalf("got %d routes, want 2: %+v", len(got), got)
	}
}

func TestBodyBearing(t *testing.T) {
	for method, want := range map[string]bool{
		"GET": false, "HEAD": false, "DELETE": false,
		"POST": true, "PUT": true, "PATCH": true,
	} {
		if got := bodyBearing(method); got != want {
			t.Errorf("bodyBearing(%q) = %v, want %v", method, got, want)
		}
	}
}

func TestRouteIdentity_QueryKeysNilVsEmptyAreDifferentGrants(t *testing.T) {
	nilRoute := Route{Slot: "s", Method: "GET", Path: "/x"}
	emptyRoute := Route{Slot: "s", Method: "GET", Path: "/x", QueryKeys: []string{}}
	idNil, err := nilRoute.identity(0)
	if err != nil {
		t.Fatal(err)
	}
	idEmpty, err := emptyRoute.identity(0)
	if err != nil {
		t.Fatal(err)
	}
	if idNil == idEmpty {
		t.Fatal("nil queryKeys (unconstrained) must not equal explicit empty queryKeys (none allowed)")
	}
}

func TestRouteIdentity_MalformedPathRejected(t *testing.T) {
	r := Route{Slot: "s", Method: "GET", Path: "/a/../b"}
	if _, err := r.identity(0); err == nil {
		t.Fatal("expected a canonicalisation error for a path traversal segment")
	}
}

func TestRouteIdentity_MalformedContentTypeRejected(t *testing.T) {
	r := Route{Slot: "s", Method: "POST", Path: "/x", ContentType: "not a content type;;;"}
	if _, err := r.identity(0); err == nil {
		t.Fatal("expected a content-type normalisation error")
	}
}
