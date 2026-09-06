// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import (
	"net/http"
	"strings"
	"testing"
)

func TestSynthesizeHTTPJSON_Basic(t *testing.T) {
	m, err := SynthesizeHTTPJSON("server", "", "/control/stats", nil, map[string]any{
		"title": "AdGuard Home",
		"blocks": []any{map[string]any{
			"type": "metrics",
			"items": []any{
				map[string]any{"label": "Queries", "value": "num_dns_queries", "format": "number"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Digest != HTTPJSONDigest || m.Runtime != "declarative" {
		t.Fatalf("unexpected manifest: %#v", m)
	}
	if len(m.Operations) != 1 || len(m.Operations[0].Routes) != 1 {
		t.Fatalf("unexpected operations: %#v", m.Operations)
	}
	route := m.Operations[0].Routes[0]
	if route.Slot != "server" || route.Method != http.MethodGet || route.Path != "/control/stats" {
		t.Fatalf("unexpected route: %#v", route)
	}
}

func TestSynthesizeHTTPJSON_DefaultsMethodToGet(t *testing.T) {
	m, err := SynthesizeHTTPJSON("server", "", "/x", nil, map[string]any{"title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Operations[0].Routes[0].Method != http.MethodGet {
		t.Fatalf("method = %q, want GET", m.Operations[0].Routes[0].Method)
	}
}

func TestSynthesizeHTTPJSON_RejectsBadMethod(t *testing.T) {
	if _, err := SynthesizeHTTPJSON("server", "PUT", "/x", nil, map[string]any{}); err == nil {
		t.Fatal("expected PUT to be refused")
	}
}

func TestSynthesizeHTTPJSON_RejectsMalformedPath(t *testing.T) {
	if _, err := SynthesizeHTTPJSON("server", "", "/a/../b", nil, map[string]any{}); err == nil {
		t.Fatal("expected a path traversal segment to be refused")
	}
}

func TestSynthesizeHTTPJSON_RejectsOversizedViewExpression(t *testing.T) {
	big := "1"
	for range 600 {
		big += "+1"
	}
	view := map[string]any{"blocks": []any{map[string]any{"items": []any{
		map[string]any{"value": big},
	}}}}
	if _, err := SynthesizeHTTPJSON("server", "", "/x", nil, view); err == nil {
		t.Fatal("expected the oversized view expression to be refused")
	}
}

// TestSynthesizeHTTPJSON_RejectsMatchesInViewValues: "matches" is a binary operator, untouched
// by bindBareFieldsToData's identifier rewrite, so exprVisitor's rejection of it still applies
// unchanged to a view value. ($env is not tested here: bindBareFieldsToData rewrites the bare
// identifier "$env" into ordinary member access on "data" - ordinary, harmless JSON field access,
// not expr's real $env accessor - before compileExprSource ever sees it, the same "safe by
// construction" property TestBuild_BareFieldNamesCannotReachInternalEnvKeys proves for __grant.)
func TestSynthesizeHTTPJSON_RejectsMatchesInViewValues(t *testing.T) {
	view := map[string]any{"blocks": []any{map[string]any{"items": []any{
		map[string]any{"value": `x matches "^a"`},
	}}}}
	if _, err := SynthesizeHTTPJSON("server", "", "/x", nil, view); err == nil {
		t.Fatal(`expected "matches" to be refused`)
	}
}

func TestBindBareFieldsToData(t *testing.T) {
	cases := map[string]string{
		"a":              "data.a",
		"a / b":          "data.a / data.b",
		"a.b":            "data.a.b",
		"params.limit":   "params.limit",
		"now":            "now",
		"a > 1 && b < 2": "data.a > 1 && data.b < 2",
		"a ? b : c":      "data.a ? data.b : data.c",
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			got, err := bindBareFieldsToData(src)
			if err != nil {
				t.Fatal(err)
			}
			// Compare with whitespace normalised - expr's printer spacing is an implementation
			// detail this test should not be sensitive to.
			norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
			if norm(got) != norm(want) {
				t.Fatalf("bindBareFieldsToData(%q) = %q, want %q", src, got, want)
			}
		})
	}
}
