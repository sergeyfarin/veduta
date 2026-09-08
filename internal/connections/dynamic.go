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

func NewDynamic(r Registry) *Dynamic { return &Dynamic{current: r} }
func (d *Dynamic) Swap(r Registry)   { d.mu.Lock(); d.current = r; d.mu.Unlock() }
func (d *Dynamic) Get(id string) (*Connection, bool) {
	d.mu.RLock()
	r := d.current
	d.mu.RUnlock()
	if r == nil {
		return nil, false
	}
	return r.Get(id)
}
func (d *Dynamic) Do(ctx context.Context, id string, req Request) (*Response, error) {
	d.mu.RLock()
	r := d.current
	d.mu.RUnlock()
	if r == nil {
		return nil, ErrUnknownConnection
	}
	return r.Do(ctx, id, req)
}
func (d *Dynamic) Health(ctx context.Context, id string) Health {
	d.mu.RLock()
	r := d.current
	d.mu.RUnlock()
	if r == nil {
		return Health{Error: ErrUnknownConnection.Error()}
	}
	return r.Health(ctx, id)
}
