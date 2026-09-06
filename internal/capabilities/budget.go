// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

// consumeHostCall decrements the shared hostCalls budget - docs/01-architecture.md: "A single
// hostCalls budget ... is decremented by every broker method: HTTP, CacheGet, CachePut, AssetRef,
// Log, Emit." Every broker method calls this exactly once, before doing any real work.
func (g Grant) consumeHostCall() error {
	b := g.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if g.Limits.HostCalls > 0 && b.hostCallsUsed >= g.Limits.HostCalls {
		return ErrBudgetExceeded
	}
	b.hostCallsUsed++
	return nil
}

// consumeHTTPRequest decrements the httpRequests budget - upstream HTTP calls specifically,
// narrower than hostCalls.
func (g Grant) consumeHTTPRequest() error {
	b := g.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if g.Limits.HTTPRequests > 0 && b.httpRequestsUsed >= g.Limits.HTTPRequests {
		return ErrBudgetExceeded
	}
	b.httpRequestsUsed++
	return nil
}

// consumeCacheWrite decrements both cacheEntries (one write = one entry) and cacheBytesKB (by
// the value's size) - cacheEntries alone does not bound storage, docs/01-architecture.md notes,
// because a plugin could write few entries that are each enormous.
func (g Grant) consumeCacheWrite(valueBytes int) error {
	b := g.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if g.Limits.CacheEntries > 0 && b.cacheEntriesUsed >= g.Limits.CacheEntries {
		return ErrBudgetExceeded
	}
	kb := (valueBytes + 1023) / 1024
	if g.Limits.CacheBytesKB > 0 && b.cacheBytesKBUsed+kb > g.Limits.CacheBytesKB {
		return ErrBudgetExceeded
	}
	b.cacheEntriesUsed++
	b.cacheBytesKBUsed += kb
	return nil
}
