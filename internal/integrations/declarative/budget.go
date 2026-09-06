// SPDX-License-Identifier: AGPL-3.0-or-later

// Package declarative executes approved declarative integration manifests.
package declarative

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"time"
)

var errBudgetExceeded = errors.New("declarative runtime budget exceeded")

type budget struct {
	mu                                           sync.Mutex
	deadline                                     time.Time
	iterations, bytes, nodes, maxBytes, maxNodes int
}

func (b *budget) check() error {
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
