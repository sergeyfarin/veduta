// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import "sync"

// Grant is one invocation's complete authority. The three route policies are carried
// SEPARATELY and evaluated independently - there is no combined "effective routes" field,
// because intersecting arbitrary globs is not a well-defined operation, and a field inviting
// that computation is exactly the bug docs/01-architecture.md section 6 exists to prevent.
type Grant struct {
	PluginID   string
	Version    string
	InstanceID string
	Slots      map[string]string // slot -> connection id (from config)
	Caps       CapSet

	ManifestRoutes   []Route                     // what the integration asked for
	ApprovedRoutes   []Route                     // what the administrator approved in veduta.lock.yaml
	ConnectionPolicy map[string]ConnectionPolicy // per slot

	Limits Limits
	Ident  ExecutionIdentity

	budget *budgetState
}

// NewGrant constructs a Grant with fresh, unconsumed budget counters - call once per invocation.
// Every broker call sharing this exact Grant value shares the same budget state, because Go
// copies the struct but not what budget (a pointer) points to.
func NewGrant(pluginID, version, instanceID string, slots map[string]string, caps CapSet,
	manifestRoutes, approvedRoutes []Route, connectionPolicy map[string]ConnectionPolicy,
	limits Limits, ident ExecutionIdentity) Grant {
	return Grant{
		PluginID:         pluginID,
		Version:          version,
		InstanceID:       instanceID,
		Slots:            slots,
		Caps:             caps,
		ManifestRoutes:   manifestRoutes,
		ApprovedRoutes:   approvedRoutes,
		ConnectionPolicy: connectionPolicy,
		Limits:           limits,
		Ident:            ident,
		budget:           &budgetState{},
	}
}

// Fresh returns the same immutable authority with new per-invocation counters. A scheduler may
// retain a Grant template between runs, but budgets are never allowed to leak across invocations.
func (g Grant) Fresh() Grant {
	g.budget = &budgetState{}
	return g
}

// budgetState is the mutable per-invocation counters a Grant's pointer shares across every
// broker call made with it. Guarded by a mutex: a single invocation is normally sequential, but
// nothing here assumes it can never be called concurrently (a future async pipeline step, say).
type budgetState struct {
	mu sync.Mutex

	hostCallsUsed    int
	httpRequestsUsed int
	cacheEntriesUsed int
	cacheBytesKBUsed int
}
