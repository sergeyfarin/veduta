// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

func boundDoc(signal string, value float64) widgets.Document {
	return widgets.Document{
		Blocks: []widgets.Block{widgets.BlockSeries{History: &widgets.SeriesHistory{Window: "1h",
			Lines: []widgets.HistoryLine{{Signal: signal, Label: "CPU"}}}}},
		Signals: map[string]widgets.Signal{"cpu": {Value: value}},
	}
}

// The core attaches a bound block's points to the card state from what it has retained - the
// integration only ever named the signal. Each refresh adds its own reading, so the history grows
// run by run, and it is persisted with the state so a restart brings it back.
func TestBoundSeriesCarriesTheCardsOwnRetainedHistory(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	m := scheduler.New(store)
	defer m.Close()
	var n atomic.Int64
	run := func(context.Context) (widgets.Document, error) { return boundDoc("cpu", float64(n.Add(1))), nil }
	ctx := context.Background()
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "c", Hash: "c", Refresh: time.Hour, Timeout: time.Second, HistorySignals: map[string]struct{}{"cpu": {}}, Run: run}}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, "c", state.StateOK)
	for range 3 {
		time.Sleep(5 * time.Millisecond) // distinct timestamps
		if err := m.RefreshNow(ctx, "c"); err != nil && !errors.Is(err, scheduler.ErrRateLimited) {
			t.Fatal(err)
		}
	}
	cs, _ := m.State("c")
	points := cs.History["cpu"]
	if len(points) < 2 {
		t.Fatalf("history = %+v, want the readings of several runs", cs.History)
	}
	last := points[len(points)-1]
	if last.V == nil || *last.V != float64(n.Load()) {
		t.Fatalf("last point = %+v, want the reading just taken (%d)", last, n.Load())
	}
	if len(cs.History) != 1 {
		t.Fatalf("history carries %d signals, want only the bound one", len(cs.History))
	}
}

// Binding a signal the operation does not retain would draw a chart that never fills. The document
// is refused, and the card says why, classified as an invalid document rather than an upstream
// failure the operator would go and look for in the wrong place.
func TestBindingAnUnretainedSignalFailsTheCard(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	run := func(context.Context) (widgets.Document, error) { return boundDoc("mem", 1), nil }
	if err := m.Apply(context.Background(), []scheduler.Definition{{ID: "c", Hash: "c", Refresh: time.Hour, Timeout: time.Second, HistorySignals: map[string]struct{}{"cpu": {}}, Run: run}}); err != nil {
		t.Fatal(err)
	}
	cs := waitForState(t, m, "c", state.StateError)
	if cs.Execution.Error == nil || cs.Execution.Error.Code != state.ErrorInvalid {
		t.Fatalf("error = %+v, want an invalid-document error", cs.Execution.Error)
	}
}

// A stale card still shows its last good document, and the chart drawn with that document has to
// come with it - otherwise every failed refresh would blank the trend it was meant to explain.
func TestStaleCardKeepsItsLastGoodHistory(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	m := scheduler.New(store)
	defer m.Close()
	var fail atomic.Bool
	run := func(context.Context) (widgets.Document, error) {
		if fail.Load() {
			return widgets.Document{}, errors.New("down")
		}
		return boundDoc("cpu", 7), nil
	}
	ctx := context.Background()
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "c", Hash: "c", Refresh: time.Hour, Timeout: time.Second, HistorySignals: map[string]struct{}{"cpu": {}}, Run: run}}); err != nil {
		t.Fatal(err)
	}
	good := waitForState(t, m, "c", state.StateOK)
	if len(good.History["cpu"]) == 0 {
		t.Fatal("no history on the good state")
	}
	fail.Store(true)
	_ = m.Refresh(ctx, "c")
	stale := waitForState(t, m, "c", state.StateStale)
	if len(stale.History["cpu"]) != len(good.History["cpu"]) {
		t.Fatalf("stale history = %+v, want the last good %+v", stale.History, good.History)
	}
}
