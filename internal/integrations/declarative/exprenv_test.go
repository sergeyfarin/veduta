// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/expr-lang/expr/builtin"
)

func TestPredicateCallBoundaryCharging(t *testing.T) {
	p, err := compile(`map(items, .x)`, 512)
	if err != nil {
		t.Fatal(err)
	}
	b := &budget{deadline: time.Now().Add(time.Second), iterations: 2}
	_, err = run(context.Background(), p, map[string]any{"items": []map[string]any{{"x": 1}, {"x": 2}, {"x": 3}}}, b)
	if !errors.Is(err, errBudgetExceeded) {
		t.Fatalf("got %v, want budget error", err)
	}
}

func TestEveryExprBuiltinIsExplicitlyClassified(t *testing.T) {
	allowed := map[string]bool{}
	for _, n := range scalarBuiltins {
		allowed[n] = true
	}
	for _, fn := range builtin.Builtins {
		n := fn.Name
		count := 0
		for _, set := range []map[string]bool{predicates, replacedBuiltins, allowed, deniedBuiltins} {
			if set[n] {
				count++
			}
		}
		if count != 1 {
			t.Errorf("builtin %q has %d classifications", n, count)
		}
	}
}

func TestSortByChargesNLogN(t *testing.T) {
	p, err := compile(`sortBy(items, .x)`, 512)
	if err != nil {
		t.Fatal(err)
	}
	b := &budget{deadline: time.Now().Add(time.Second), iterations: 7}
	_, err = run(context.Background(), p, map[string]any{"items": []map[string]any{{"x": 4}, {"x": 3}, {"x": 2}, {"x": 1}}}, b)
	if !errors.Is(err, errBudgetExceeded) {
		t.Fatalf("got %v, want budget error", err)
	}
}
