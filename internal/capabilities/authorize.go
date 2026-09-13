// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import (
	"fmt"
	"mime"
	"strings"

	"veduta.dev/veduta/internal/connections/routepath"
)

// coreDefaultBodyKB is the request-body ceiling when neither a route nor the manifest narrows
// it - docs/01-architecture.md: "There is no 'unlimited'."
const coreDefaultBodyKB = 64

// Authorize reports whether one concrete request is permitted. It is the ONLY place a request is
// compared against policy, and it evaluates all three independently - manifest, lock, connection
// - never a precomputed intersection (docs/01-architecture.md section 6). The request's path is
// canonicalised exactly once (routepath.Canonicalise) and that same canonical path is matched
// against each policy in turn.
func (g Grant) Authorize(req HTTPRequest, use UseKind) error {
	canonicalPath, err := routepath.Canonicalise(req.Path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRouteDenied, err)
	}

	if !anyRouteAllows(g.ManifestRoutes, req, canonicalPath, use, g.Limits.RequestBodyKB) {
		return fmt.Errorf("%w: no manifest route permits %s %s", ErrRouteDenied, req.Method, canonicalPath)
	}
	if !anyRouteAllows(g.ApprovedRoutes, req, canonicalPath, use, g.Limits.RequestBodyKB) {
		return fmt.Errorf("%w: no approved (lock) route permits %s %s", ErrRouteDenied, req.Method, canonicalPath)
	}
	if policy, ok := g.ConnectionPolicy[req.Slot]; ok && !connectionAllows(policy, canonicalPath) {
		return fmt.Errorf("%w: the connection's own allowedPaths forbids %s", ErrRouteDenied, canonicalPath)
	}
	return nil
}

// anyRouteAllows reports whether some route in routes permits req, matched against
// canonicalPath. Each route's own queryKeys/contentType/maxBodyKB are checked as part of that
// route's own tuple identity (docs/01-architecture.md's "route identity is the full tuple") -
// a route only "matches" if the ENTIRE tuple is satisfied, not just method+path.
func anyRouteAllows(routes []Route, req HTTPRequest, canonicalPath string, use UseKind, manifestRequestBodyKB int) bool {
	for _, route := range routes {
		if !routeMatchesSlotMethodAndPath(route, req.Slot, req.Method, canonicalPath, use) {
			continue
		}
		if !queryKeysAllowed(route.QueryKeys, req.Query) {
			continue
		}
		if !contentTypeAllowed(route.ContentType, req.Header) {
			continue
		}
		if !bodySizeAllowed(route.MaxBodyKB, manifestRequestBodyKB, req.bodyLen()) {
			continue
		}
		return true
	}
	return false
}

// routeMatchesSlotMethodAndPath is anyRouteAllows' slot+method+path+use match, factored out so
// AuthorizesRedirect can reuse it without the query/content-type/body checks that only apply to
// a caller-constructed request.
//
// The slot comparison is the first thing checked, and it is not optional. Route identity is
// defined by docs/01-architecture.md section 6 - and by plugin-manifest.v1.schema.json's own
// description - as "(slot, method, canonical path, use, sorted queryKeys, normalised
// contentType, effective maxBodyKB)", with slot as its FIRST element. Found in review: every
// other element of that tuple was compared here and slot was not, so for an integration bound
// to more than one connection, a route approved for one slot authorised the identical
// method+path against a DIFFERENT slot's connection and credentials - approval to read one
// service became approval to read another. The broker's own g.Slots[req.Slot] check does not
// catch this: it only proves the requested slot is bound to some connection, never that the
// route being matched is the one approved for it.
func routeMatchesSlotMethodAndPath(route Route, slot, method, canonicalPath string, use UseKind) bool {
	if route.Slot != slot || route.Use != use || route.Method != method {
		return false
	}
	routePattern, err := routepath.Canonicalise(route.Path)
	if err != nil {
		return false // a malformed route pattern can never match; schema validation should have
		// already rejected it at load time, but this never trusts that blindly.
	}
	return routepath.Match(routePattern, canonicalPath)
}

// queryKeysAllowed implements the nil-vs-empty distinction docs/01-architecture.md is explicit
// about: nil means unconstrained (any key), a non-nil slice, even empty, is an allowlist.
//
// This deliberately says nothing about connection-owned keys. An earlier version of this comment
// asserted they "are stripped before Authorize ever sees the request", which was not true of the
// ordinary request path: capabilities.Broker.HTTP calls Authorize first and only builds the
// connection's ownership set afterwards, so a plugin supplying the connection's own auth
// parameter is DENIED here (unless a route names that key) and then stripped by filterQuery
// regardless. That order is fail-closed and stays. The redirect path is the one place the
// stripping genuinely has to happen first, because there the key can arrive from the upstream's
// own Location header rather than from the plugin - connections.redirectPolicy does it there.
func queryKeysAllowed(allowed []string, query map[string]string) bool {
	if allowed == nil {
		return true
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allowedSet[k] = true
	}
	for k := range query {
		if !allowedSet[k] {
			return false
		}
	}
	return true
}

// contentTypeAllowed compares the request's own Content-Type header against a route's declared
// one, both normalised - mime.ParseMediaType + mime.FormatMediaType, lowercasing type/subtype and
// the charset parameter, keeping every parameter (dropping any would collapse
// application/vnd.api+json;profile=... into its base type - docs/01-architecture.md's own
// warning about an earlier draft's mistake here).
func contentTypeAllowed(declared string, header map[string]string) bool {
	if declared == "" {
		return true // no constraint declared
	}
	got := header["Content-Type"]
	normalisedGot, err := normaliseMediaType(got)
	if err != nil {
		return false
	}
	normalisedDeclared, err := normaliseMediaType(declared)
	if err != nil {
		return false
	}
	return normalisedGot == normalisedDeclared
}

func normaliseMediaType(v string) (string, error) {
	mt, params, err := mime.ParseMediaType(v)
	if err != nil {
		return "", err
	}
	if cs, ok := params["charset"]; ok {
		params["charset"] = strings.ToLower(cs)
	}
	out := mime.FormatMediaType(strings.ToLower(mt), params)
	if out == "" {
		return "", fmt.Errorf("cannot format media type %q", v)
	}
	return out, nil
}

// bodySizeAllowed enforces the effective ceiling: the route's own MaxBodyKB narrows the
// manifest's requestBodyKB, which narrows the core default - never unlimited. A negative bodyLen
// is an UNKNOWN length, not an empty body: nothing can establish that it fits, so it is refused
// rather than compared.
func bodySizeAllowed(routeMaxKB, manifestRequestBodyKB int, bodyLen int64) bool {
	if bodyLen < 0 {
		return false
	}
	effectiveKB := manifestRequestBodyKB
	if effectiveKB <= 0 {
		effectiveKB = coreDefaultBodyKB
	}
	if routeMaxKB > 0 && routeMaxKB < effectiveKB {
		effectiveKB = routeMaxKB
	}
	return bodyLen <= int64(effectiveKB)*1024
}

// connectionAllows checks a connection's own allowedPaths - the third, independent policy.
// Empty/nil AllowedPaths means any path under the connection's BaseURL is fine (D1's own
// default); a non-empty list is a subtree allowlist, matched via routepath.HasPathPrefix rather
// than a raw strings.HasPrefix - a naive prefix check would let an allowed "/api" also cover
// "/apievil", which is not a subtree of "/api" at all, just a string that happens to start the
// same way. Found and fixed in review: no test had ever exercised this specific boundary before.
func connectionAllows(policy ConnectionPolicy, canonicalPath string) bool {
	if len(policy.AllowedPaths) == 0 {
		return true
	}
	for _, allowed := range policy.AllowedPaths {
		canonicalAllowed, err := routepath.Canonicalise(allowed)
		if err != nil {
			continue
		}
		if routepath.HasPathPrefix(canonicalPath, canonicalAllowed) {
			return true
		}
	}
	return false
}
