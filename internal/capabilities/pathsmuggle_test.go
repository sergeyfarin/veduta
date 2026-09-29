// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
)

// A request path is authorised as a string and then parsed as a URL, so any character a URL
// parser gives meaning to inside a path must never reach the wire having been authorised as
// ordinary segment text. A '?' would turn the rest of an approved segment into query keys the
// route's allowlist never saw; a '#' would cut the path short, so the request reaches a path no
// route approved. Both arrive through a '*' segment, which is exactly where an expression-built
// path - an Immich asset id today, a card parameter after M2 - puts a value the author did not
// write. Every one of these must be refused before anything is sent.
func TestBroker_HTTP_PathCannotSmuggleQueryOrFragment(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/items/*/detail", Use: capabilities.UseData, QueryKeys: []string{}}
	cases := map[string]string{
		"query in a segment":          "/api/items/x?admin=1/detail",
		"fragment truncates the path": "/api/items/x#/detail",
		"query then fragment":         "/api/items/x?a=1#/detail",
		"bare question mark":          "/api/items/?/detail",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			var hits []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits = append(hits, r.URL.RequestURI())
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
			_, err := b.HTTP(context.Background(), fullGrant([]capabilities.Route{route}), capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: path})
			if err == nil {
				t.Errorf("%q was authorised against %s", path, route.Path)
			}
			if len(hits) > 0 {
				t.Errorf("%q reached the upstream as %v", path, hits)
			}
		})
	}
}

// The asset path is the one that carries upstream data today: Immich's thumbnail path is built
// from an asset id the upstream returned. A signed asset token for a path the route did not
// approve would be served later without the plugin present, so the refusal has to happen at mint.
func TestBroker_AssetRef_PathCannotSmuggleQueryOrFragment(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/assets/*/thumbnail", Use: capabilities.UseAsset}
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("assets"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{}, capabilities.ExecutionIdentity{})
	for _, path := range []string{"/api/assets/x#/thumbnail", "/api/assets/x?size=original/thumbnail"} {
		if ref, err := b.AssetRef(context.Background(), g, "server", path, nil, capabilities.Transform{}); err == nil {
			t.Errorf("minted %q for %q against %s", ref, path, route.Path)
		}
	}
}
