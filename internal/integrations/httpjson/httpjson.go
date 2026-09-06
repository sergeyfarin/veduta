// SPDX-License-Identifier: AGPL-3.0-or-later

// Package httpjson wires milestone D4's escape hatch together: a card names a path on a
// credentialed connection with no manifest and no approval record
// (docs/01-architecture.md's "http-json escape hatch"). It does not implement a second execution
// engine - Build synthesises a manifestload.Manifest and hands it to
// internal/integrations/declarative unchanged, so an http-json card gets exactly the same
// charged-expression sandbox, budgets and denials a real manifest does.
package httpjson

import (
	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// Card is the subset of a configured card's own fields http-json needs, decoupled from
// internal/config so this package is testable without it: a caller extracts Path/Method/Query
// from config.Card.Params and View from config.Card.View.
type Card struct {
	Path, Method string
	Query        map[string]string
	View         map[string]any
}

// Build synthesises the manifest, its self-approving lock entry (http-json is exempt from
// approval - docs/01-architecture.md section 6: "source: builtin integrations ... have no
// separate trust boundary" - there is nothing external for a digest to drift against, since the
// manifest is synthesised fresh from the same card configuration every invocation), and the
// Grant authorising exactly the one request the card names, bound to slot on connectionID.
// connPolicy is the connection's own allowedPaths, independent of everything else here - "the
// connection's allowedPaths still applies, and is the only thing standing between a card and
// every path on that connection" (docs/01-architecture.md's "http-json escape hatch").
func Build(slot, connectionID string, card Card, connPolicy capabilities.ConnectionPolicy) (
	*manifestload.Manifest, *integrations.LockEntry, capabilities.Grant, error,
) {
	m, err := manifestload.SynthesizeHTTPJSON(slot, card.Method, card.Path, card.Query, card.View)
	if err != nil {
		return nil, nil, capabilities.Grant{}, err
	}

	lock := &integrations.LockEntry{
		ManifestSHA256:  manifestload.HTTPJSONDigest,
		Capabilities:    m.Capabilities,
		EffectiveLimits: effectiveLimits(m.Limits),
	}

	routes := make([]capabilities.Route, len(m.Operations[0].Routes))
	for i, r := range m.Operations[0].Routes {
		routes[i] = capabilities.Route{Slot: r.Slot, Method: r.Method, Path: r.Path, Use: capabilities.UseData}
	}
	grant := capabilities.NewGrant("http-json", "1.0.0", connectionID,
		map[string]string{slot: connectionID}, capabilities.NewCapSet("http"),
		routes, routes, map[string]capabilities.ConnectionPolicy{slot: connPolicy},
		brokerLimits(m.Limits), capabilities.ExecutionIdentity{ManifestDigest: manifestload.HTTPJSONDigest})

	return m, lock, grant, nil
}

// effectiveLimits mirrors m.Limits (already defaulted by SynthesizeHTTPJSON) into the shape
// internal/integrations.LockEntry carries - every field required there, so a partial map would
// leave some limits silently governed by whatever the core default happens to be, which for
// http-json's own always-default limits is a distinction without a difference, but the shape
// still has to be filled in completely.
func effectiveLimits(l manifestload.Limits) integrations.EffectiveLimits {
	return integrations.EffectiveLimits{
		MemoryMB: l.MemoryMB, TimeoutMs: l.TimeoutMs, OutputKB: l.OutputKB, HTTPRequests: l.HTTPRequests,
		ResponseMB: l.ResponseMB, CacheEntries: l.CacheEntries, InputMB: l.InputMB, JSONDepth: l.JSONDepth,
		JSONNodes: l.JSONNodes, ExprNodes: l.ExprNodes, Iterations: l.Iterations, RequestBodyKB: l.RequestBodyKB,
		HostCalls: l.HostCalls, CacheBytesKB: l.CacheBytesKB,
	}
}

// brokerLimits is the broker-enforced subset of the full limits object - see
// capabilities.Limits' own doc comment.
func brokerLimits(l manifestload.Limits) capabilities.Limits {
	return capabilities.Limits{
		HTTPRequests: l.HTTPRequests, ResponseMB: l.ResponseMB, CacheEntries: l.CacheEntries,
		CacheBytesKB: l.CacheBytesKB, HostCalls: l.HostCalls, RequestBodyKB: l.RequestBodyKB,
	}
}
