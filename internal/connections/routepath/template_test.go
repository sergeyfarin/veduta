// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath_test

import (
	"errors"
	"testing"

	"veduta.dev/veduta/internal/connections/routepath"
)

func TestParseTemplate_RejectsMalformed(t *testing.T) {
	cases := map[string]error{
		"api/{id}":           routepath.ErrNotAbsolute,
		"/api/x{id}":         routepath.ErrTemplateBrace,
		"/api/{id}x":         routepath.ErrTemplateBrace,
		"/api/{}":            routepath.ErrTemplateBrace,
		"/api/{a}{b}":        routepath.ErrTemplateBrace,
		"/api/{{id}}":        routepath.ErrTemplateBrace,
		"/api/{id":           routepath.ErrTemplateBrace,
		"/api/id}":           routepath.ErrTemplateBrace,
		"/api/{1id}":         routepath.ErrPlaceholderName,
		"/api/{id-x}":        routepath.ErrPlaceholderName,
		"/api/{a/b}":         routepath.ErrTemplateBrace,
		"/api/{id}/../admin": routepath.ErrDotSegment,
		"/api//{id}":         routepath.ErrEmptySegment,
		"/api/{id}?x=1":      routepath.ErrTemplateBrace,
		"/api/{id}/x?y":      routepath.ErrQueryOrFragment,
		"/api/{id}/%2f":      routepath.ErrEncodedSeparator,
		"/api/{id}/":         routepath.ErrEmptySegment,
		"/api/{id}/detail#x": routepath.ErrQueryOrFragment,
	}
	for raw, want := range cases {
		if _, err := routepath.ParseTemplate(raw); !errors.Is(err, want) {
			t.Errorf("ParseTemplate(%q) = %v, want %v", raw, err, want)
		}
	}
}

// Every value that could mean more than one segment, or be read differently after the request
// leaves - a separator, an escape, a query or fragment start, a dot segment, whitespace - is
// refused, not encoded. These are the values a card parameter would use to walk off the route.
func TestTemplateExpand_RefusesValuesThatAreNotOneSegment(t *testing.T) {
	tpl, err := routepath.ParseTemplate("/api/items/{id}/detail")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{
		"", ".", "..", "a/b", "../admin", "a%2fb", "%2e%2e", "x?admin=1", "x#", "a b", "a\tb",
		"a\\b", "a;b", "a,b", "a=b", "a&b", "a+b", "a*b", "ä", string(make([]byte, 129)),
	} {
		if got, err := tpl.Expand(map[string]string{"id": v}); !errors.Is(err, routepath.ErrPlaceholderValue) {
			t.Errorf("Expand(id=%q) = %q, %v; want ErrPlaceholderValue", v, got, err)
		}
	}
	if _, err := tpl.Expand(map[string]string{}); !errors.Is(err, routepath.ErrPlaceholderAbsent) {
		t.Errorf("Expand with no value = %v, want ErrPlaceholderAbsent", err)
	}
	for v, want := range map[string]string{
		"sensor.outside_temperature": "/api/items/sensor.outside_temperature/detail",
		"0":                          "/api/items/0/detail",
		"a-b_c~d:e@f":                "/api/items/a-b_c~d:e@f/detail",
		"...":                        "/api/items/.../detail",
	} {
		got, err := tpl.Expand(map[string]string{"id": v})
		if err != nil || got != want {
			t.Errorf("Expand(id=%q) = %q, %v; want %q", v, got, err, want)
		}
	}
}

// A placeholder is covered only where the route's glob accepts ANY single segment. "v*" accepts
// some values and not others, so a template whose placeholder lands on it is not provably inside
// the route, and the loader must say so rather than let the broker find out per request.
func TestTemplateCoveredBy(t *testing.T) {
	cases := []struct {
		template, glob string
		want           bool
	}{
		{"/api/environments/{id}/containers", "/api/environments/*/containers", true},
		{"/api/environments/{id}/containers", "/api/environments/0/containers", false},
		{"/api/environments/{id}/containers", "/api/environments/v*/containers", false},
		{"/api/environments/{id}/containers", "/api/environments/*", false},
		{"/api/environments/{id}/containers", "/api/*/*/containers", true},
		{"/api/environments/{id}/containers", "/api/*/*/*", true},
		{"/api/environments/0/containers", "/api/environments/*/containers", true},
		{"/api/environments/0/containers", "/api/environments/0/containers", true},
		{"/api/states", "/api/states", true},
		{"/api/states/{entity}", "/api/states", false},
		{"/", "/", true},
		{"/api/{a}/{b}", "/api/*/*", true},
		{"/api/{a}/{b}", "/api/*/x", false},
	}
	for _, c := range cases {
		tpl, err := routepath.ParseTemplate(c.template)
		if err != nil {
			t.Fatalf("ParseTemplate(%q): %v", c.template, err)
		}
		if got := tpl.CoveredBy(c.glob); got != c.want {
			t.Errorf("%q CoveredBy %q = %v, want %v", c.template, c.glob, got, c.want)
		}
	}
}

// Whatever Expand produces must be authorised by the same glob that covered the template, for
// every value Expand accepts - the static proof and the runtime check can never disagree.
func FuzzTemplateExpandStaysCovered(f *testing.F) {
	for _, s := range []string{"0", "sensor.x", "a/b", "..", "x?y", "%2f", "a:b@c"} {
		f.Add(s)
	}
	tpl, err := routepath.ParseTemplate("/api/items/{id}/detail")
	if err != nil {
		f.Fatal(err)
	}
	glob, _ := routepath.Canonicalise("/api/items/*/detail")
	f.Fuzz(func(t *testing.T, v string) {
		path, err := tpl.Expand(map[string]string{"id": v})
		if err != nil {
			return
		}
		if !routepath.Match(glob, path) {
			t.Fatalf("value %q expanded to %q, which the covering glob does not match", v, path)
		}
		if again, err := routepath.Canonicalise(path); err != nil || again != path {
			t.Fatalf("value %q expanded to non-canonical %q (%q, %v)", v, path, again, err)
		}
	})
}
