// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"net/http"
	"testing"
)

func TestRedirectPolicy_RefusesDifferentHost(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil)
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
	policy := redirectPolicy(base, 1, nil)
	req := &http.Request{URL: mustParseURL(t, "http://svc.example/x")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected a scheme downgrade to be refused")
	}
}

func TestRedirectPolicy_RefusesExceedingMaxRedirects(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil)
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/x")}
	via := []*http.Request{{}, {}} // two prior hops, exceeding maxRedirects=1
	if err := policy(req, via); err == nil {
		t.Fatal("expected exceeding maxRedirects to be refused")
	}
}

func TestRedirectPolicy_NoAllowedPathsMeansAnyPathOnTheSameHostIsFine(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, nil)
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
// capabilities.Grant.AuthorizesRedirect, plumbed in through redirectauth.go; see
// docs/03-backlog-resolved.md and TestBroker_HTTP_RedirectToAnUnapprovedRouteIsDenied.
func TestRedirectPolicy_RefusesRedirectOutsideAllowedPaths(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, []string{"/api/public"})
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/api/admin")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected a redirect to a path outside allowedPaths to be refused")
	}
}

func TestRedirectPolicy_AllowsRedirectWithinAllowedPaths(t *testing.T) {
	base := mustParseURL(t, "https://svc.example/")
	policy := redirectPolicy(base, 1, []string{"/api/public"})
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
	policy := redirectPolicy(base, 1, []string{"/api/public"})
	req := &http.Request{URL: mustParseURL(t, "https://svc.example/api/publicEVIL")}
	if err := policy(req, nil); err == nil {
		t.Fatal("expected /api/publicEVIL to be refused - it is not under the /api/public subtree")
	}
}
