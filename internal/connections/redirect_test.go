// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestRedirectPolicy_RefusesDifferentHost(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "https://evil.example/x")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected a different host to be refused")
	}
}

// TestRedirectPolicy_RefusesSchemeDowngrade is the regression test for a real gap found in
// review: an authorised HTTPS connection redirecting to plain HTTP on the identical host used to
// be followed, silently downgrading transport security for a request that still carries the
// connection's injected auth header.
func TestRedirectPolicy_RefusesSchemeDowngrade(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "http://svc.example/x")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected a scheme downgrade to be refused")
	}
}

func TestRedirectPolicy_RefusesExceedingMaxRedirects(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/x")}
	via := []*http.Request{{}, {}} // two prior hops, exceeding maxRedirects=1
	if err := policy(req, via); err == nil {
		t.Fatal("expected exceeding maxRedirects to be refused")
	}
}

func TestRedirectPolicy_NoAllowedPathsMeansAnyPathOnTheSameHostIsFine(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/anything")}
	if err := policy(req, nil); err != nil {
		t.Fatalf("unexpected error with no allowedPaths configured: %v", err)
	}
}

// TestRedirectPolicy_RefusesRedirectOutsideAllowedPaths is the regression test for the core of
// the finding: an authorised request to a path under an allowed subtree could redirect - same
// host, same scheme, within maxRedirects - to a DIFFERENT path on that same connection the
// administrator never allowed. This layer confines the connection's own allowedPaths - "the only
// thing standing between a card and every path on that connection" per docs/01-architecture.md -
// to actually applying on a followed redirect too. The residual gap it does NOT close - re-checking
// the lock's approved route against the redirect target - was closed separately by
// capabilities.Grant.Authorize, plumbed in through redirectauth.go; see
// docs/03-backlog-resolved.md and TestBroker_HTTP_RedirectToAnUnapprovedRouteIsDenied.
func TestRedirectPolicy_RefusesRedirectOutsideAllowedPaths(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, []string{"/api/public"}, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/api/admin")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected a redirect to a path outside allowedPaths to be refused")
	}
}

func TestRedirectPolicy_AllowsRedirectWithinAllowedPaths(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, []string{"/api/public"}, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/api/public/photo.jpg")}
	if err := policy(req, nil); err != nil {
		t.Fatalf("unexpected error for a redirect within allowedPaths: %v", err)
	}
}

// TestRedirectPolicy_AllowedPathsBoundaryIsASubtree proves the same prefix-boundary fix (finding
// #7's routepath.HasPathPrefix) applies here too, not just in capabilities.connectionAllows - a
// redirect to "/api/publicEVIL" must not be treated as within the "/api/public" subtree.
func TestRedirectPolicy_AllowedPathsBoundaryIsASubtree(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, []string{"/api/public"}, Auth{}, nil)
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/api/publicEVIL")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected /api/publicEVIL to be refused - it is not under the /api/public subtree")
	}
}

// captureRedirect runs policy against one destination and returns the RedirectRequest the
// authorizer was handed, which is what every normalisation below is actually about.
func captureRedirect(t *testing.T, base *url.URL, auth Auth, headers map[string]string, req *http.Request) (RedirectRequest, error) {
	t.Helper()
	var got RedirectRequest
	seen := false
	ctx := WithRedirectAuthorizer(context.Background(), func(dest RedirectRequest) error {
		got, seen = dest, true
		return nil
	})
	err := redirectPolicy(base, 1, nil, auth, headers)(req.WithContext(ctx), nil)
	if err == nil && !seen {
		t.Fatal("the authorizer was never consulted")
	}
	return got, err
}

// TestRedirectPolicy_PathIsRelativeToABaseWithItsOwnPath is the non-root base URL case. A
// connection based at .../api serves a route written as /items at /api/items, so an authorizer
// handed the absolute /api/items would compare it against /items and deny every legitimate
// redirect. joinPath's inverse has to be applied before the grant sees it.
func TestRedirectPolicy_PathIsRelativeToABaseWithItsOwnPath(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/api")
	req := &http.Request{Method: http.MethodGet, URL: mustParseURL(t, "https://svc.example/api/items")}
	got, err := captureRedirect(t, base, Auth{}, nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Path != "/items" {
		t.Fatalf("want the path relative to the base, got %q", got.Path)
	}
}

// TestRedirectPolicy_RefusesADestinationOutsideTheBasePath: a destination that is not under the
// connection's own base path cannot be expressed as a caller path at all. Reshaping it into one
// would be inventing a path the grant never described, so it is refused instead.
func TestRedirectPolicy_RefusesADestinationOutsideTheBasePath(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/api")
	req := &http.Request{Method: http.MethodGet, URL: mustParseURL(t, "https://svc.example/admin")}
	if _, err := captureRedirect(t, base, Auth{}, nil, req); err == nil {
		t.Fatal("expected a destination outside the base path to be refused")
	}
}

// TestRedirectPolicy_StripsTheConnectionsOwnQueryCredential is the permissive half of the
// filtering, and the one a naive implementation gets wrong. An upstream that echoes the
// connection's api_key back in its Location must not cause a denial: the plugin cannot set that
// parameter in the first place, so no route's queryKeys would ever name it.
func TestRedirectPolicy_StripsTheConnectionsOwnQueryCredential(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	auth := Auth{Type: AuthQuery, Name: "api_key"}
	req := &http.Request{Method: http.MethodGet, URL: mustParseURL(t, "https://svc.example/items?api_key=secret&page=2")}
	got, err := captureRedirect(t, base, auth, nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := got.Query["api_key"]; present {
		t.Fatal("the connection's own auth parameter must not reach the authorizer")
	}
	if got.Query["page"] != "2" {
		t.Fatalf("a genuine destination parameter must survive, got %v", got.Query)
	}
}

// TestRedirectPolicy_CarriesTheDestinationsOwnQuery is the restrictive half: a parameter the
// upstream introduces through Location alone is part of what gets authorised, because it is part
// of the request that will actually be sent with the connection's credentials.
func TestRedirectPolicy_CarriesTheDestinationsOwnQuery(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	req := &http.Request{Method: http.MethodGet, URL: mustParseURL(t, "https://svc.example/items?include=secrets")}
	got, err := captureRedirect(t, base, Auth{}, nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Query["include"] != "secrets" {
		t.Fatalf("the destination's own query must reach the authorizer, got %v", got.Query)
	}
}

// TestRedirectPolicy_ReportsAnUnknownBodyLengthAsUnknown: Go uses -1 for a body whose size it
// cannot state. Flattening that to zero would let it satisfy any ceiling.
func TestRedirectPolicy_ReportsAnUnknownBodyLengthAsUnknown(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	req := &http.Request{Method: http.MethodPost, URL: mustParseURL(t, "https://svc.example/items"), ContentLength: -1}
	got, err := captureRedirect(t, base, Auth{}, nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BodyLen != -1 {
		t.Fatalf("want an unknown length reported as -1, got %d", got.BodyLen)
	}
}
