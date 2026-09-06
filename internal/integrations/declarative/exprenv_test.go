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

// TestBudgetCheck_ObservesContextCancellationNotJustItsOwnDeadline is the regression test for a
// gap found in review: check() used to compare only against its own wall-clock deadline, which is
// set to the same instant as ctx's deadline at construction (Invoke's own
// context.WithDeadline(ctx, deadline)) - so the two agree for "the manifest's timeout elapsed",
// but an EARLY cancellation (a disconnected client, a future scheduler fencing a stale
// invocation) would leave a long-running expr-only loop running until the wall-clock deadline
// regardless of ctx already being done. A far-future deadline must not mask an already-cancelled
// context.
func TestBudgetCheck_ObservesContextCancellationNotJustItsOwnDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &budget{ctx: ctx, deadline: time.Now().Add(time.Hour)}
	if err := b.check(); err == nil {
		t.Fatal("expected check() to observe an already-cancelled context despite a far-future wall-clock deadline")
	}
}

// TestBudgetCheck_NilContextFallsBackToWallClockOnly guards the nil case: a budget built without
// a context (as every pre-existing exprenv_test.go table test in this file does) must not panic
// and must keep behaving exactly as before this fix.
func TestBudgetCheck_NilContextFallsBackToWallClockOnly(t *testing.T) {
	b := &budget{deadline: time.Now().Add(time.Hour)}
	if err := b.check(); err != nil {
		t.Fatalf("unexpected error with no context and a far-future deadline: %v", err)
	}
}

// TestRun_StopsPromptlyWhenContextIsCancelledMidExpression proves the fix reaches real
// expr-only work through run() and charge(), not just budget.check() in isolation - a charged
// builtin call with no HTTP call anywhere in it must still observe an already-cancelled context.
func TestRun_StopsPromptlyWhenContextIsCancelledMidExpression(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := compile(`map(items, .x)`, 512)
	if err != nil {
		t.Fatal(err)
	}
	b := &budget{ctx: ctx, deadline: time.Now().Add(time.Hour), iterations: 1000}
	_, err = run(ctx, p, map[string]any{"items": []map[string]any{{"x": 1}}}, b)
	if err == nil {
		t.Fatal("expected run to observe the already-cancelled context rather than complete normally")
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
