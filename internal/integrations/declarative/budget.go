// SPDX-License-Identifier: AGPL-3.0-or-later

// Package declarative executes approved declarative integration manifests.
package declarative

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"
)

var errBudgetExceeded = errors.New("declarative runtime budget exceeded")

type budget struct {
	mu                                           sync.Mutex
	ctx                                          context.Context
	deadline                                     time.Time
	iterations, bytes, nodes, maxBytes, maxNodes int
}

// check is the one place every charge/template call reports back to, and the one place a
// cancellation is actually observed during pure-expression work (a loop with no HTTP call in it
// never otherwise touches ctx at all). Found in review: this used to compare only against its own
// wall-clock deadline, which is set to the same instant as ctx's own deadline at construction
// (Invoke's context.WithDeadline(ctx, deadline)) - so the two agree for the ordinary "the
// manifest's own timeout elapsed" case, but not for an early cancellation: a caller context
// canceled *before* that deadline for any other reason (the request's own context cancelled by a
// disconnected client, a future scheduler fencing a stale invocation) would stop an HTTP call
// immediately, since net/http itself watches ctx, but would leave a long-running expr-only loop
// (map/filter/sortBy with no HTTP call inside it) running until the wall-clock deadline anyway,
// ctx.Err() notwithstanding. Checking ctx.Err() here closes that for every charge/template call,
// which is the finest granularity this budget can observe (see docs/03-backlog-resolved.md's S3
// entry for the coarser, expr-internal limitation this does not and cannot close: expr's own
// native builtin loops are not preemptible mid-iteration without forking expr, a call already
// made and recorded in docs/02-implementation-plan.md's D3 entry).
func (b *budget) check() error {
	if b.ctx != nil {
		if err := b.ctx.Err(); err != nil {
			return fmt.Errorf("declarative runtime: %w", err)
		}
	}
	if !b.deadline.IsZero() && time.Now().After(b.deadline) {
		return errors.New("declarative runtime deadline exceeded")
	}
	return nil
}
func (b *budget) charge(v any, name string) (any, error) {
	if e := b.check(); e != nil {
		return nil, e
	}
	n := length(v)
	cost := n
	if name == "sortBy" || name == "reduce" {
		if n > 1 {
			cost = int(math.Ceil(float64(n) * math.Log2(float64(n))))
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if cost > b.iterations {
		return nil, errBudgetExceeded
	}
	b.iterations -= cost
	return v, nil
}
func (b *budget) template() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.iterations < 1 {
		return errBudgetExceeded
	}
	b.iterations--
	return b.check()
}
func length(v any) int {
	if v == nil {
		return 0
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
		return r.Len()
	}
	return 0
}
