// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"fmt"
	"mime"
	"sort"
	"strings"

	"veduta.dev/veduta/internal/connections/routepath"
)

// coreDefaultBodyKB is the request-body ceiling when neither a route nor the manifest narrows
// it - docs/01-architecture.md: "There is no 'unlimited'." Mirrors internal/capabilities' own
// constant of the same name and meaning.
const coreDefaultBodyKB = 64

// Route mirrors one entry of a manifest's or lock's routes array - schemas/plugin-manifest.v1
// #/$defs/route and schemas/integration-lock.v1 #/$defs/route, which are deliberately the same
// shape (the lock schema's route $def is a near-duplicate by design, docs/01 section 6). Field
// names match the YAML/JSON exactly so a value decodes and re-encodes without translation.
type Route struct {
	Slot        string   `yaml:"slot" json:"slot"`
	Method      string   `yaml:"method" json:"method"`
	Path        string   `yaml:"path" json:"path"`
	Use         string   `yaml:"use,omitempty" json:"use,omitempty"` // "" means "data"
	Reason      string   `yaml:"reason,omitempty" json:"reason,omitempty"`
	QueryKeys   []string `yaml:"queryKeys,omitempty" json:"queryKeys,omitempty"`
	ContentType string   `yaml:"contentType,omitempty" json:"contentType,omitempty"`
	MaxBodyKB   int      `yaml:"maxBodyKB,omitempty" json:"maxBodyKB,omitempty"`
}

// identity is a Route reduced to the exact tuple docs/01-architecture.md section 6 defines as
// authority: "(slot, method, canonical path, use, sorted queryKeys, normalised contentType,
// effective maxBodyKB)". Two routes grant the same authority iff their identities are equal -
// comparing Route values directly would treat a reordered queryKeys list, an unnormalised
// contentType, or an implicit vs. explicit maxBodyKB as a different route, none of which the
// architecture treats as a real change of authority.
type identity struct {
	slot, method, path, use string
	queryKeys               string // "*" = unconstrained (nil), else sorted, deduped, NUL-joined
	contentType             string
	maxBodyKB               int
}

// identity computes r's authority tuple. requestBodyKB is the owning manifest's requestBodyKB
// limit (0 if unset), needed because maxBodyKB's effective value depends on it.
func (r Route) identity(requestBodyKB int) (identity, error) {
	path, err := routepath.Canonicalise(r.Path)
	if err != nil {
		return identity{}, fmt.Errorf("route %s %s: %w", r.Method, r.Path, err)
	}
	use := r.Use
	if use == "" {
		use = "data"
	}
	ct, err := normaliseContentType(r.ContentType)
	if err != nil {
		return identity{}, fmt.Errorf("route %s %s: %w", r.Method, r.Path, err)
	}
	return identity{
		slot:        r.Slot,
		method:      r.Method,
		path:        path,
		use:         use,
		queryKeys:   canonicalQueryKeys(r.QueryKeys),
		contentType: ct,
		maxBodyKB:   effectiveMaxBodyKB(r.MaxBodyKB, requestBodyKB),
	}, nil
}

// canonicalQueryKeys implements the nil-vs-empty distinction docs/01-architecture.md is explicit
// about: nil means unconstrained (any key except connection-owned ones - the WIDER permission);
// a non-nil slice, even empty, is an allowlist. The two must never compare equal.
func canonicalQueryKeys(keys []string) string {
	if keys == nil {
		return "*"
	}
	cp := append([]string(nil), keys...)
	sort.Strings(cp)
	deduped := cp[:0]
	for i, k := range cp {
		if i == 0 || k != cp[i-1] {
			deduped = append(deduped, k)
		}
	}
	return "\x00" + strings.Join(deduped, "\x00")
}

// effectiveMaxBodyKB is the route's own ceiling, else the manifest's requestBodyKB, else the
// core default - never "unlimited".
func effectiveMaxBodyKB(routeMaxKB, requestBodyKB int) int {
	if routeMaxKB > 0 {
		return routeMaxKB
	}
	if requestBodyKB > 0 {
		return requestBodyKB
	}
	return coreDefaultBodyKB
}

// normaliseContentType mirrors internal/capabilities' normaliseMediaType exactly
// (mime.ParseMediaType + mime.FormatMediaType, lowercasing type/subtype and the charset
// parameter, keeping every other parameter) - reimplemented locally rather than imported, this
// project's established convention for not letting one internal package depend on another's
// unexported details (see internal/config, internal/capabilities).
func normaliseContentType(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	mt, params, err := mime.ParseMediaType(v)
	if err != nil {
		return "", fmt.Errorf("content-type %q: %w", v, err)
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

// dedupeRoutes drops exact duplicate Route values (same manifest route declared by more than one
// operation) while preserving first-seen order, so a manifest's aggregate route set is stable and
// order does not itself invent apparent changes across reloads.
func dedupeRoutes(routes []Route) []Route {
	type key struct {
		slot, method, path, use, contentType string
		maxBodyKB                            int
		queryKeys                            string
	}
	seen := make(map[key]bool, len(routes))
	out := make([]Route, 0, len(routes))
	for _, r := range routes {
		k := key{r.Slot, r.Method, r.Path, r.Use, r.ContentType, r.MaxBodyKB, canonicalQueryKeys(r.QueryKeys)}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

// bodyBearing reports whether a route's method can carry a request body - the approval UI's
// "body-bearing routes are flagged" warning (docs/01-architecture.md section 6: a route grant
// cannot see inside the body, so approving one non-GET route approves everything its body could
// select).
func bodyBearing(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}
