// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities_test

import (
	"errors"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
)

func grantWithRoutes(manifest, approved []capabilities.Route, policy map[string]capabilities.ConnectionPolicy) capabilities.Grant {
	return capabilities.NewGrant("plug", "1.0.0", "inst", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http", "cache", "assets", "log", "events"),
		manifest, approved, policy, capabilities.Limits{}, capabilities.ExecutionIdentity{})
}

func statsRoute(method string, use capabilities.UseKind) capabilities.Route {
	return capabilities.Route{Slot: "server", Method: method, Path: "/api/stats", Use: use}
}

func TestAuthorize_HappyPath(t *testing.T) {
	route := statsRoute("GET", capabilities.UseData)
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseData); err != nil {
		t.Fatalf("want no error, got %v", err)
	}
}

// TestAuthorize_WrongMethodDeniedForEveryMethod is the D1b/D2 AC: "a route-denial test exists
// for every method in the Route.Method enum." For each of the six methods, granting it and
// requesting a DIFFERENT one must be denied.
func TestAuthorize_WrongMethodDeniedForEveryMethod(t *testing.T) {
	methods := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}
	for _, granted := range methods {
		for _, requested := range methods {
			if granted == requested {
				continue
			}
			t.Run(granted+"_vs_"+requested, func(t *testing.T) {
				route := statsRoute(granted, capabilities.UseData)
				g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)
				req := capabilities.HTTPRequest{Slot: "server", Method: requested, Path: "/api/stats"}
				err := g.Authorize(req, capabilities.UseData)
				if !errors.Is(err, capabilities.ErrRouteDenied) {
					t.Errorf("granted %s, requested %s: err = %v, want ErrRouteDenied", granted, requested, err)
				}
			})
		}
	}
}

func TestAuthorize_UnapprovedPathDenied(t *testing.T) {
	manifestRoute := statsRoute("GET", capabilities.UseData)
	// Approved (lock) routes do NOT include /api/stats - only a different path.
	approvedRoute := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/other", Use: capabilities.UseData}
	g := grantWithRoutes([]capabilities.Route{manifestRoute}, []capabilities.Route{approvedRoute}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("err = %v, want ErrRouteDenied (manifest allows it, lock does not)", err)
	}
}

// TestAuthorize_ConnectionPolicyIsIndependent is the D2 AC: "a request permitted by the manifest
// and the lock but forbidden by the connection's allowedPaths is still denied."
func TestAuthorize_ConnectionPolicyIsIndependent(t *testing.T) {
	route := statsRoute("GET", capabilities.UseData)
	policy := map[string]capabilities.ConnectionPolicy{"server": {AllowedPaths: []string{"/api/other"}}}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, policy)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("err = %v, want ErrRouteDenied (manifest and lock both permit it, connection does not)", err)
	}
}

// TestAuthorize_NoGlobIntersectionShortcut proves the three policies are matched independently,
// not via a precomputed intersection: give the manifest and lock DIFFERENT patterns that each
// individually match the request, but whose "intersection" (if one were naively computed) would
// not obviously include it either way - the point is that each list is searched on its own.
func TestAuthorize_NoGlobIntersectionShortcut(t *testing.T) {
	manifestRoute := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/*", Use: capabilities.UseData}
	approvedRoute := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/stats", Use: capabilities.UseData}
	g := grantWithRoutes([]capabilities.Route{manifestRoute}, []capabilities.Route{approvedRoute}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseData); err != nil {
		t.Fatalf("want success: manifest's glob and lock's literal route each independently match /api/stats, got %v", err)
	}

	// Now a path the manifest's glob matches but the lock's literal route does not.
	req2 := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/other"}
	if err := g.Authorize(req2, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("want denial: lock only approved /api/stats, got %v", err)
	}
}

func TestAuthorize_UseKindMustMatch(t *testing.T) {
	dataRoute := statsRoute("GET", capabilities.UseData)
	g := grantWithRoutes([]capabilities.Route{dataRoute}, []capabilities.Route{dataRoute}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseAsset); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("a UseData route must not authorise a UseAsset request: err = %v", err)
	}
}

// TestAuthorize_QueryKeys_NilVsEmpty is the D2 AC: "a query key outside a route's queryKeys
// allowlist is denied while nil (unconstrained) and empty (no query at all) behave differently."
func TestAuthorize_QueryKeys_NilVsEmpty(t *testing.T) {
	unconstrained := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/stats", Use: capabilities.UseData, QueryKeys: nil}
	g := grantWithRoutes([]capabilities.Route{unconstrained}, []capabilities.Route{unconstrained}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats", Query: map[string]string{"anything": "1"}}
	if err := g.Authorize(req, capabilities.UseData); err != nil {
		t.Fatalf("nil QueryKeys should allow any query key, got %v", err)
	}

	noQuery := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/stats", Use: capabilities.UseData, QueryKeys: []string{}}
	g2 := grantWithRoutes([]capabilities.Route{noQuery}, []capabilities.Route{noQuery}, nil)
	if err := g2.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("empty (non-nil) QueryKeys should deny any query key, got %v", err)
	}
	reqNoQuery := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g2.Authorize(reqNoQuery, capabilities.UseData); err != nil {
		t.Fatalf("empty QueryKeys should still allow a request with no query at all, got %v", err)
	}
}

func TestAuthorize_QueryKeys_Allowlist(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/stats", Use: capabilities.UseData, QueryKeys: []string{"limit"}}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)

	ok := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats", Query: map[string]string{"limit": "5"}}
	if err := g.Authorize(ok, capabilities.UseData); err != nil {
		t.Fatalf("allowlisted key should pass, got %v", err)
	}
	bad := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats", Query: map[string]string{"offset": "5"}}
	if err := g.Authorize(bad, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("non-allowlisted key should be denied, got %v", err)
	}
}

// TestAuthorize_ContentTypeNormalisedComparison is the D2 AC: "a body whose Content-Type differs
// from the declared one after normalisation is denied" - phrased as "after normalisation" on
// purpose: this also proves equivalent-but-differently-written content types are ACCEPTED.
func TestAuthorize_ContentTypeNormalisedComparison(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "POST", Path: "/api/search", Use: capabilities.UseData, ContentType: "application/json; charset=utf-8"}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)

	equivalent := capabilities.HTTPRequest{Slot: "server", Method: "POST", Path: "/api/search",
		Header: map[string]string{"Content-Type": "APPLICATION/JSON; CHARSET=UTF-8"}}
	if err := g.Authorize(equivalent, capabilities.UseData); err != nil {
		t.Fatalf("a differently-cased but equivalent content type should pass, got %v", err)
	}

	different := capabilities.HTTPRequest{Slot: "server", Method: "POST", Path: "/api/search",
		Header: map[string]string{"Content-Type": "text/plain"}}
	if err := g.Authorize(different, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("a genuinely different content type should be denied, got %v", err)
	}
}

func TestAuthorize_BodySizeCeiling(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "POST", Path: "/api/search", Use: capabilities.UseData, MaxBodyKB: 1}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)

	small := capabilities.HTTPRequest{Slot: "server", Method: "POST", Path: "/api/search", Body: make([]byte, 512)}
	if err := g.Authorize(small, capabilities.UseData); err != nil {
		t.Fatalf("a body within the ceiling should pass, got %v", err)
	}
	big := capabilities.HTTPRequest{Slot: "server", Method: "POST", Path: "/api/search", Body: make([]byte, 2048)}
	if err := g.Authorize(big, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("a body over the ceiling should be denied, got %v", err)
	}
}

func TestAuthorize_StarNeverCrossesSlash(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/*", Use: capabilities.UseData}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/a/b"}
	if err := g.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("/api/* must not match /api/a/b, got %v", err)
	}
}

func TestAuthorize_TraversalPathDenied(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/*", Use: capabilities.UseData}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/../secret"}
	if err := g.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("a traversal path should be denied, got %v", err)
	}
}

func TestAuthorize_ContentType_MalformedRequestHeaderDenied(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "POST", Path: "/api/search", Use: capabilities.UseData, ContentType: "application/json"}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "POST", Path: "/api/search",
		Header: map[string]string{"Content-Type": "not a valid media type;;;"}}
	if err := g.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("a malformed Content-Type header should be denied, got %v", err)
	}
}

func TestAuthorize_ContentType_NoConstraintAllowsAnything(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "POST", Path: "/api/search", Use: capabilities.UseData} // ContentType: ""
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, nil)
	req := capabilities.HTTPRequest{Slot: "server", Method: "POST", Path: "/api/search",
		Header: map[string]string{"Content-Type": "anything/at-all"}}
	if err := g.Authorize(req, capabilities.UseData); err != nil {
		t.Fatalf("no declared content type should allow any request content type, got %v", err)
	}
}

// TestAuthorize_ConnectionPolicy_MultipleAllowedPathsSecondMatches covers connectionAllows'
// loop finding a match after an earlier entry does not match.
func TestAuthorize_ConnectionPolicy_MultipleAllowedPathsSecondMatches(t *testing.T) {
	route := statsRoute("GET", capabilities.UseData)
	policy := map[string]capabilities.ConnectionPolicy{"server": {AllowedPaths: []string{"/other", "/api"}}}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, policy)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseData); err != nil {
		t.Fatalf("the second allowedPaths entry should match, got %v", err)
	}
}

// TestAuthorize_ConnectionPolicy_PrefixIsASubtreeNotAStringPrefix is the regression test for a
// real, confirmed bug found in review: connectionAllows used to compare with a raw
// strings.HasPrefix, which would let an allowedPaths entry of "/api" also permit "/apievil" -
// not a subtree of "/api" at all, just a string that happens to start the same way. A request to
// a route that is itself manifest- and lock-approved, but whose path only superficially shares a
// prefix with the allowed subtree, must still be denied.
func TestAuthorize_ConnectionPolicy_PrefixIsASubtreeNotAStringPrefix(t *testing.T) {
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/apievil/x", Use: capabilities.UseData}
	policy := map[string]capabilities.ConnectionPolicy{"server": {AllowedPaths: []string{"/api"}}}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, policy)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/apievil/x"}
	if err := g.Authorize(req, capabilities.UseData); !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("got %v, want ErrRouteDenied - /apievil/x is not under the /api subtree", err)
	}
}

// TestAuthorize_ConnectionPolicy_MalformedAllowedPathIgnored covers connectionAllows' continue
// branch: a malformed entry in AllowedPaths must not crash Authorize, just never match.
func TestAuthorize_ConnectionPolicy_MalformedAllowedPathIgnored(t *testing.T) {
	route := statsRoute("GET", capabilities.UseData)
	policy := map[string]capabilities.ConnectionPolicy{"server": {AllowedPaths: []string{"not-absolute", "/api"}}}
	g := grantWithRoutes([]capabilities.Route{route}, []capabilities.Route{route}, policy)
	req := capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}
	if err := g.Authorize(req, capabilities.UseData); err != nil {
		t.Fatalf("a malformed allowedPaths entry should be skipped, not fatal, got %v", err)
	}
}
