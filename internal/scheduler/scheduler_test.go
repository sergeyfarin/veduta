// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

func TestSharedFlightAndViewerIndependentScheduling(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	var calls atomic.Int32
	release := make(chan struct{})
	run := func(context.Context) (widgets.Document, error) {
		calls.Add(1)
		<-release
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defs := make([]scheduler.Definition, 10)
	for i := range defs {
		defs[i] = scheduler.Definition{ID: string(rune('a' + i)), Hash: string(rune('a' + i)), Key: "same", Refresh: time.Hour, Timeout: time.Second, Source: state.Source{}, Run: run}
	}
	if err := m.Apply(ctx, defs); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want 1", calls.Load())
	}
}

func TestOnlyDeclaredNumericHistorySignalsAreStored(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	manager := scheduler.New(store)
	defer manager.Close()
	definition := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, HistorySignals: map[string]struct{}{"kept": {}}, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{"kept": {Value: float64(12)}, "ignored": {Value: float64(99)}}}, nil
	}}
	if err = manager.Apply(context.Background(), []scheduler.Definition{definition}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager, "card", state.StateOK)
	rows, err := store.DB().Query(`SELECT signal,value FROM signal_history WHERE card_id='card'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("declared signal was not stored")
	}
	var name string
	var value float64
	if err := rows.Scan(&name, &value); err != nil || name != "kept" || value != 12 || rows.Next() {
		t.Fatalf("name=%q value=%v err=%v", name, value, err)
	}
}

func TestHistoricalSignalTypeChangeIsRejected(t *testing.T) {
	manager := scheduler.New(nil)
	defer manager.Close()
	definition := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, HistorySignals: map[string]struct{}{"cpu": {}}, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{"cpu": {Value: "twelve"}}}, nil
	}}
	if err := manager.Apply(context.Background(), []scheduler.Definition{definition}); err != nil {
		t.Fatal(err)
	}
	card := waitForState(t, manager, "card", state.StateError)
	if card.Execution.Error == nil || !strings.Contains(card.Execution.Error.Message, "changed type") {
		t.Fatalf("error=%+v", card.Execution.Error)
	}
}

func waitForState(t *testing.T, manager *scheduler.Manager, id string, wanted state.ExecutionState) state.CardState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		card, _ := manager.State(id)
		if card.Execution.State == wanted {
			return card
		}
		time.Sleep(time.Millisecond)
	}
	card, _ := manager.State(id)
	t.Fatalf("state=%s, want %s", card.Execution.State, wanted)
	return state.CardState{}
}

func TestFailureKeepsLastGoodThenOpensCircuit(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	var fail atomic.Bool
	run := func(context.Context) (widgets.Document, error) {
		if fail.Load() {
			return widgets.Document{}, errors.New("down")
		}
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "a", Hash: "a", Refresh: time.Hour, Timeout: time.Second, Run: run}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	fail.Store(true)
	for range 3 {
		_ = m.Refresh(ctx, "a")
	}
	cs, _ := m.State("a")
	if cs.Execution.State != state.StateStale || cs.Execution.CircuitOpenUntil == "" {
		t.Fatalf("state=%+v", cs.Execution)
	}
	if cs.Execution.Error == nil || cs.Execution.Error.Message != "down" {
		t.Fatalf("latest error missing from stale state: %+v", cs.Execution.Error)
	}
}

func TestConfigSwapFencesAnOldInvocation(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	m := scheduler.New(store)
	defer m.Close()
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	oldRun := func(context.Context) (widgets.Document, error) {
		close(started)
		<-release
		return widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{"value": {Value: float64(1)}}}, nil
	}
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "a", Hash: "old", Key: "old", Refresh: time.Hour, HistorySignals: map[string]struct{}{"value": {}}, Run: oldRun}}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "a", Hash: "new", Key: "new", Refresh: time.Hour, HistorySignals: map[string]struct{}{"value": {}}, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{"value": {Value: float64(2)}}}, nil
	}}}); err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		cs, _ := m.State("a")
		if cs.Execution.State == state.StateOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new invocation did not commit: %+v", cs)
		}
		time.Sleep(time.Millisecond)
	}
	var samples, total float64
	if err := store.DB().QueryRow(`SELECT count(*),sum(value) FROM signal_history WHERE card_id='a'`).Scan(&samples, &total); err != nil {
		t.Fatal(err)
	}
	if samples != 1 || total != 2 {
		t.Fatalf("samples=%v total=%v; superseded invocation wrote history", samples, total)
	}
}

func TestDeadlineCancelsInvocation(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	ctx := context.Background()
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "a", Hash: "a", Refresh: time.Hour, Timeout: 10 * time.Millisecond, Run: func(ctx context.Context) (widgets.Document, error) {
		<-ctx.Done()
		return widgets.Document{}, ctx.Err()
	}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		cs, _ := m.State("a")
		if cs.Execution.State == state.StateError {
			if cs.Execution.Error.Code != state.ErrorTimeout {
				t.Fatalf("code=%s", cs.Execution.Error.Code)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deadline did not surface")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestManualRefreshIsRateLimited(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	ctx := context.Background()
	release := make(chan struct{})
	var once atomic.Bool
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "a", Hash: "a", Refresh: time.Hour, Run: func(context.Context) (widgets.Document, error) {
		if !once.Swap(true) {
			<-release
		}
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}}}); err != nil {
		t.Fatal(err)
	}
	close(release)
	time.Sleep(5 * time.Millisecond)
	if err := m.RefreshNow(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := m.RefreshNow(ctx, "a"); !errors.Is(err, scheduler.ErrRateLimited) {
		t.Fatalf("err=%v", err)
	}
}

func TestCircuitActuallyRunsHalfOpenProbeAndCloses(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	var calls atomic.Int32
	if err := m.Apply(context.Background(), []scheduler.Definition{{ID: "probe", Hash: "probe", Refresh: time.Millisecond, Timeout: time.Second, Run: func(context.Context) (widgets.Document, error) {
		if calls.Add(1) <= 3 {
			return widgets.Document{}, errors.New("down")
		}
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	sawOpen := false
	for {
		cs, _ := m.State("probe")
		if cs.Execution.CircuitOpenUntil != "" {
			sawOpen = true
		}
		if sawOpen && cs.Execution.State == state.StateOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("half-open probe did not recover: calls=%d state=%+v", calls.Load(), cs.Execution)
		}
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 4 {
		t.Fatalf("calls=%d want three failures plus a probe", calls.Load())
	}
}

// The flight must be released however runShared leaves, not only on its success path. It used to
// be released inline before each return, so a panic in a card's Run left the key in m.flights
// with its done channel never closed - and every later refresh of that card, from the loop or
// from the API, blocked on that channel for the life of the generation. The card stayed pending
// forever with no error, no retry and nothing in the log.
//
// The panic is driven through Refresh rather than Apply on purpose: Apply's loop runs the card on
// its own goroutine, where a panic would take the test process down and prove nothing. The
// barrier that stops that reaching the scheduler at all lives in internal/app; this pins the
// scheduler's own invariant, which must not depend on every caller installing one first.
func TestAPanickingRunReleasesItsFlightSoTheCardRunsAgain(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	var calls atomic.Int32
	var explode atomic.Bool
	run := func(context.Context) (widgets.Document, error) {
		calls.Add(1)
		if explode.Load() {
			panic("integration exploded")
		}
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}
	d := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, Timeout: time.Second, Run: run}
	if err := m.Apply(context.Background(), []scheduler.Definition{d}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, "card", state.StateOK)

	explode.Store(true)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the run did not panic; this test no longer exercises the case")
			}
		}()
		_ = m.Refresh(context.Background(), "card")
	}()
	explode.Store(false)

	// The refresh below must not block on the panicked run's flight. Against the old code it
	// blocks forever, so the timeout is the assertion.
	done := make(chan error, 1)
	go func() { done <- m.Refresh(context.Background(), "card") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("refresh after a panicked run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("refresh blocked on the panicked run's flight")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d want 3 (initial, panicking, recovered)", calls.Load())
	}
}

// A panicked run is classified as an internal fault, not an upstream one: nothing was wrong with
// the service the card queries, and an operator reading "upstream" would go and check it.
func TestPanickedRunIsReportedAsAnInternalError(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	d := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, Timeout: time.Second,
		Run: func(context.Context) (widgets.Document, error) {
			return widgets.Document{}, scheduler.ErrRunPanicked
		}}
	if err := m.Apply(context.Background(), []scheduler.Definition{d}); err != nil {
		t.Fatal(err)
	}
	cs := waitForState(t, m, "card", state.StateError)
	if cs.Execution.Error == nil {
		t.Fatal("no error recorded")
	}
	if cs.Execution.Error.Code != state.ErrorInternal {
		t.Fatalf("code=%q want %q", cs.Execution.Error.Code, state.ErrorInternal)
	}
	// The tile's text says what happened and where to look, and carries no panic value.
	if !strings.Contains(cs.Execution.Error.Message, "server log") {
		t.Fatalf("message=%q does not point at the log", cs.Execution.Error.Message)
	}
}
