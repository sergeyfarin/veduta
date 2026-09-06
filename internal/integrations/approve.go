// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"errors"
	"fmt"
	"time"
)

// ErrDigestChanged is returned when expectedManifestSHA256 no longer matches the manifest at
// approval time - docs/01-architecture.md section 6's two-step, digest-bound transaction: "the
// server recomputes the digest; if it differs ... nothing is approved." Callers should recompute
// ComputeDiff against the manifest they already have (its Digest is now the fresh one) and show
// that, rather than retrying blindly.
var ErrDigestChanged = errors.New("manifest changed since the approval was reviewed")

// ErrGrantExceedsRequest is returned when grants asks for more than the manifest itself
// requests - "the client sends the exact grants it is approving... approving a subset is
// natural, and a race cannot widen the grant" only holds if the server itself refuses a grant
// that is not a subset.
var ErrGrantExceedsRequest = errors.New("requested grant exceeds what the manifest itself asks for")

// Grants is what an approval actually grants - "the client sends the exact grants it is
// approving, not a bare yes" (docs/01-architecture.md section 6). Limits is the admin's explicit
// override for any field requested above its documented default (Limits{}, the zero value, means
// none - every such field then stays at its default regardless of what the manifest asks for;
// see ReconcileAtApproval). Callers that want "approve everything the manifest currently
// requests" - the CLI and REST handlers' own default - pass the manifest's own Capabilities,
// Routes and Limits verbatim; a narrower Grants is how a subset approval is expressed.
type Grants struct {
	Capabilities []string
	Routes       []Route
	Limits       Limits
}

// Approve is the second step of the two-step, digest-bound transaction: recompute the digest (m
// must already be freshly loaded), refuse if it no longer matches expectedManifestSHA256, refuse
// if grants asks for anything the manifest itself does not, then build the LockEntry to write.
// It does not write anything - callers persist the returned entry via WriteLock once they also
// hold whatever wider transaction (reading, mutating and rewriting the whole Lock document) their
// caller needs.
func Approve(m *Manifest, expectedManifestSHA256 string, grants Grants, approvedBy string, now time.Time) (*LockEntry, error) {
	if m.Digest != expectedManifestSHA256 {
		return nil, fmt.Errorf("%w: expected %s, manifest is now %s", ErrDigestChanged, expectedManifestSHA256, m.Digest)
	}

	manifestCaps := make(map[string]bool, len(m.Capabilities))
	for _, c := range m.Capabilities {
		manifestCaps[c] = true
	}
	for _, c := range grants.Capabilities {
		if !manifestCaps[c] {
			return nil, fmt.Errorf("%w: capability %q", ErrGrantExceedsRequest, c)
		}
	}

	manifestReqBodyKB := requested("requestBodyKB", m.Limits)
	manifestRoutes, err := identitySet(m.Routes, manifestReqBodyKB)
	if err != nil {
		return nil, err
	}
	for _, r := range grants.Routes {
		id, err := r.identity(manifestReqBodyKB)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrGrantExceedsRequest, err)
		}
		if !manifestRoutes[id] {
			return nil, fmt.Errorf("%w: route %s %s", ErrGrantExceedsRequest, r.Method, r.Path)
		}
	}

	// Only fields grants.Limits actually sets are checked - an unset field grants nothing (it
	// falls back to the default via ReconcileAtApproval regardless), so it can never "exceed" the
	// manifest's request even when that default happens to be above what the manifest itself
	// asks for (glances' httpRequests:4 is below the default of 8, for instance).
	for _, name := range limitFieldOrder {
		v, ok := grants.Limits.field(name)
		if !ok {
			continue
		}
		if v > requested(name, m.Limits) {
			return nil, fmt.Errorf("%w: limit %s", ErrGrantExceedsRequest, name)
		}
	}

	// Reason is manifest-only display metadata ("shown verbatim in the approval prompt" -
	// schemas/plugin-manifest.v1.schema.json) - the lock schema's own route $def has no such
	// field, so it is dropped before a route becomes part of the persisted record.
	lockRoutes := make([]Route, len(grants.Routes))
	for i, r := range grants.Routes {
		r.Reason = ""
		lockRoutes[i] = r
	}

	entry := &LockEntry{
		ManifestSHA256:  m.Digest,
		ModuleSHA256:    m.ModuleSHA256,
		Version:         m.Version,
		Runtime:         m.Runtime,
		ApprovedAt:      now.UTC().Format(time.RFC3339),
		ApprovedBy:      approvedBy,
		Capabilities:    grants.Capabilities,
		Routes:          lockRoutes,
		EffectiveLimits: ReconcileAtApproval(m.Limits, grants.Limits),
	}
	if !grants.Limits.isZero() {
		limits := grants.Limits
		entry.Limits = &limits
	}
	return entry, nil
}
