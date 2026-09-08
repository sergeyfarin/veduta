// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"sync"
)

// Dynamic atomically swaps a complete registry so config, resolved credentials, and callers
// move between coherent generations instead of mutating live clients in place.
type Dynamic struct {
	mu      sync.RWMutex
	current Registry
}

// NewDynamic wraps a registry that can be replaced as one coherent generation.
func NewDynamic(r Registry) *Dynamic { return &Dynamic{current: r} }

// Swap replaces the active registry.
func (d *Dynamic) Swap(r Registry) { d.mu.Lock(); d.current = r; d.mu.Unlock() }

// Get returns a connection from the active registry.
func (d *Dynamic) Get(id string) (*Connection, bool) {
	d.mu.RLock()
	r := d.current
	d.mu.RUnlock()
	if r == nil {
		return nil, false
	}
	return r.Get(id)
}

// Do sends an authorised request through the active registry.
func (d *Dynamic) Do(ctx context.Context, id string, req Request) (*Response, error) {
	d.mu.RLock()
	r := d.current
	d.mu.RUnlock()
	if r == nil {
		return nil, ErrUnknownConnection
	}
	return r.Do(ctx, id, req)
}

// Health reports connection health from the active registry.
func (d *Dynamic) Health(ctx context.Context, id string) Health {
	d.mu.RLock()
	r := d.current
	d.mu.RUnlock()
	if r == nil {
		return Health{Error: ErrUnknownConnection.Error()}
	}
	return r.Health(ctx, id)
}
