// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import (
	"testing"
)

// TestFilterHeaders_Allowlist is the D2 AC: only a small allowlist survives at all.
func TestFilterHeaders_Allowlist(t *testing.T) {
	owned := newConnectionOwnership("none", "", nil)
	in := map[string]string{
		"Accept":        "application/json",
		"X-Custom":      "should be dropped",
		"Authorization": "Bearer stolen",
	}
	out := filterHeaders(in, owned)
	if out["Accept"] != "application/json" {
		t.Errorf("Accept should survive, got %+v", out)
	}
	if _, ok := out["X-Custom"]; ok {
		t.Error("X-Custom is not on the allowlist and must be dropped")
	}
	if _, ok := out["Authorization"]; ok {
		t.Error("Authorization is not on the allowlist and must be dropped")
	}
}

// TestFilterHeaders_AlwaysStripped is the D2 AC: Host, Content-Length, Transfer-Encoding,
// Connection, Upgrade are never settable, regardless of the allowlist.
func TestFilterHeaders_AlwaysStripped(t *testing.T) {
	owned := newConnectionOwnership("none", "", nil)
	always := []string{"Host", "Content-Length", "Transfer-Encoding", "Connection", "Upgrade"}
	for _, name := range always {
		in := map[string]string{name: "smuggled"}
		out := filterHeaders(in, owned)
		if _, ok := out[name]; ok {
			t.Errorf("%s must always be stripped", name)
		}
	}
}

// TestFilterHeaders_ConnectionOwnedDropped is the D2 AC: a plugin-supplied Authorization-shaped
// custom header the connection owns (via auth.type: header) is dropped, not forwarded.
func TestFilterHeaders_ConnectionOwnedDropped(t *testing.T) {
	owned := newConnectionOwnership("header", "X-Api-Key", nil)
	// X-Api-Key is not even on the allowlist, so this also demonstrates always-drop for a
	// connection-owned custom header regardless of casing.
	in := map[string]string{"x-api-key": "plugin-supplied-value", "Accept": "application/json"}
	out := filterHeaders(in, owned)
	if _, ok := out["x-api-key"]; ok {
		t.Error("a connection-owned header must be dropped even if the plugin supplies it")
	}
	if out["Accept"] != "application/json" {
		t.Error("an unrelated allowlisted header should still survive")
	}
}

// TestFilterHeaders_StaticConnectionHeadersDropped: any header name in the connection's own
// static `headers` map is connection-owned too, even if it happens to be on the plugin
// allowlist.
func TestFilterHeaders_StaticConnectionHeadersDropped(t *testing.T) {
	owned := newConnectionOwnership("none", "", map[string]string{"Accept": "application/vnd.custom+json"})
	in := map[string]string{"Accept": "application/json"}
	out := filterHeaders(in, owned)
	if _, ok := out["Accept"]; ok {
		t.Error("Accept is connection-owned here (in the static headers map) and must be dropped")
	}
}

// TestFilterQuery_ConnectionOwnedDiscarded is the D2 AC: "a plugin-supplied query key that the
// connection owns (auth.type: query) is discarded" - independent of any route's queryKeys
// allowlist, which is Authorize's own, separate concern.
func TestFilterQuery_ConnectionOwnedDiscarded(t *testing.T) {
	owned := newConnectionOwnership("query", "apikey", nil)
	in := map[string]string{"apikey": "plugin-supplied", "limit": "5"}
	out := filterQuery(in, owned)
	if _, ok := out["apikey"]; ok {
		t.Error("a connection-owned query key must be discarded even if the plugin supplies it")
	}
	if out["limit"] != "5" {
		t.Error("an unrelated query key should still survive")
	}
}

func TestFilterHeaders_DoesNotMutateInput(t *testing.T) {
	owned := newConnectionOwnership("none", "", nil)
	in := map[string]string{"Accept": "application/json", "X-Custom": "x"}
	_ = filterHeaders(in, owned)
	if len(in) != 2 {
		t.Fatal("filterHeaders must not mutate its input map")
	}
}
