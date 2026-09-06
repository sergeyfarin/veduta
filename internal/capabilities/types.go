// SPDX-License-Identifier: AGPL-3.0-or-later

// Package capabilities implements milestone D2: the capability broker. Authority is
// (slot, method, path), not (slot) - isolating a plugin from Veduta is only half the promise;
// the other half is that a plugin bound to Immich cannot delete an album. See
// docs/01-architecture.md section 6.
package capabilities

// UseKind distinguishes a route granted for real HTTP data from one granted only for minting an
// asset reference. AssetRef matches only UseAsset routes and HTTP only UseData routes - without
// that split, a route granted for image thumbnails could mint refs to any path the data grant
// forbids, and the asset endpoint would quietly become the wider permission.
type UseKind string

// UseKind values.
const (
	UseData  UseKind = "data"
	UseAsset UseKind = "asset"
)

// Route is one upstream request surface an integration may reach: method and path are part of
// the permission, same as the capability itself.
type Route struct {
	Slot   string // slot name, never a connection id
	Method string // GET|HEAD|POST|PUT|PATCH|DELETE
	Path   string // glob: '*' matches within one segment; there is no '**' in v1 (routepath.Match)
	Use    UseKind

	// QueryKeys distinguishes nil from empty deliberately: nil means "any key except
	// connection-owned" (unconstrained, the WIDER permission); a non-nil empty slice means "no
	// query parameters at all". Collapsing the two would silently widen a route on every round
	// trip through the lock file.
	QueryKeys []string

	// ContentType is "" (none declared) or a mime.ParseMediaType-normalised value - see
	// normaliseContentType.
	ContentType string

	// MaxBodyKB is 0 to fall back to the manifest's requestBodyKB ceiling, then the core
	// default (64). There is no "unlimited".
	MaxBodyKB int
}

// CapSet is the set of host capabilities a Grant carries: http, cache, assets, log, events.
type CapSet map[string]bool

// Has reports whether cap is present.
func (c CapSet) Has(cap string) bool { return c[cap] }

// NewCapSet builds a CapSet from a list of capability names.
func NewCapSet(caps ...string) CapSet {
	s := make(CapSet, len(caps))
	for _, c := range caps {
		s[c] = true
	}
	return s
}

// ConnectionPolicy is a connection's own authority, independent of anything a manifest requests
// or a lock approves - the third of the three independent policies. AllowedPaths mirrors
// internal/connections.HTTPConfig.AllowedPaths (nil/empty means any path under BaseURL); a Grant
// carries its own copy, built once when the grant is constructed, so authorising a request never
// needs to reach back into the connection registry.
type ConnectionPolicy struct {
	AllowedPaths []string
}

// Limits are the effective per-invocation ceilings the broker itself enforces - the subset of
// plugin-manifest.v1's full limits object that are budgets and body ceilings the broker checks,
// as opposed to the declarative runtime's or WASM sandbox's own resource controls (memoryMB,
// timeoutMs, jsonDepth, and so on - milestones D3/G, not this one). Every value here is already
// the EFFECTIVE one (docs/01-architecture.md's effective(k) = min(core max, manifest, approved))
// - computing that reconciliation is milestone D2b's job once a real manifest and lock exist;
// this package only enforces whatever Limits it is given.
type Limits struct {
	HTTPRequests  int // manifest's httpRequests: upstream HTTP calls this invocation may make
	ResponseMB    int // manifest's responseMB: per-response size ceiling (connections' own MaxResponseBytes narrows further)
	CacheEntries  int // manifest's cacheEntries: distinct cache keys this invocation may write
	CacheBytesKB  int // manifest's cacheBytesKB: total cache bytes this invocation may write
	HostCalls     int // manifest's hostCalls: shared ceiling across every broker method, not just HTTP
	RequestBodyKB int // manifest's requestBodyKB: ceiling for request bodies this integration may send
}

// ExecutionIdentity is what one invocation was authorised against - docs/01-architecture.md
// section 11 ("every invocation is fenced"). Milestone F's scheduler is what constructs one for
// real and cancels it on a config reload; this package only carries the field and uses
// ManifestDigest for cache namespacing (docs/01 section 6: cache keys are namespaced
// plugin:{id}:{manifest digest}:{instance}:{key}, precisely so an upgraded plugin starts cold
// rather than reading a previous version's entries).
type ExecutionIdentity struct {
	SnapshotGen      uint64
	CardHash         string
	ManifestDigest   string
	ApprovalRevision string
	SlotRevisions    map[string]string
}

// Transform is an asset-ref resize/format request, checked against a small allowlist by AssetRef
// - docs/01-architecture.md section 7. 0.1 is pass-through (E1); the type exists now so Broker's
// signature is stable.
type Transform struct {
	Width  int    // one of 0 (unset), 160, 320, 640, 1280
	Format string // "" (unset), "webp", "jpeg"
}

// Event is what Emit sends - the notification/rules substrate (docs/01-architecture.md section
// 12, milestone J). Broker only needs a shape to authorise and budget against for D2; J defines
// what a caller actually puts in Data.
type Event struct {
	Type string
	Data map[string]any
}
