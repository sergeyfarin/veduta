// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler_test

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

// TestFiftyCardPiClassLoadBudget is the repeatable CI proxy for the release load gate. Four Go
// execution threads model a Raspberry Pi 4 class CPU; 50 cards distributed over eight logical
// connections must complete their initial refresh inside a deliberately conservative budget while
// respecting the scheduler's eight-worker ceiling.
func TestFiftyCardPiClassLoadBudget(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := scheduler.New(store)
	defer manager.Close()
	events, unsubscribe := manager.Subscribe(64)
	defer unsubscribe()

	var active, peak atomic.Int32
	definitions := make([]scheduler.Definition, 50)
	for i := range definitions {
		connection := i % 8
		definitions[i] = scheduler.Definition{
			ID:            fmt.Sprintf("card-%02d", i),
			Hash:          fmt.Sprintf("card-%02d", i),
			SlotRevisions: fmt.Sprintf("connection-%d", connection),
			Refresh:       time.Hour,
			Timeout:       time.Second,
			Run: func(context.Context) (widgets.Document, error) {
				current := active.Add(1)
				for observed := peak.Load(); current > observed && !peak.CompareAndSwap(observed, current); observed = peak.Load() {
				}
				time.Sleep(5 * time.Millisecond)
				active.Add(-1)
				return widgets.Document{Blocks: []widgets.Block{widgets.BlockText{Content: "ok"}}}, nil
			},
		}
	}
	started := time.Now()
	if err := manager.Apply(ctx, definitions); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for completed := 0; completed < len(definitions); completed++ {
		select {
		case event := <-events:
			if event.State.Execution.State != "ok" {
				t.Fatalf("%s completed in state %s", event.State.CardID, event.State.Execution.State)
			}
		case <-deadline.C:
			t.Fatalf("50-card initial refresh exceeded 5 seconds; completed %d", completed)
		}
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("50-card initial refresh took %s", elapsed)
	}
	if got := peak.Load(); got < 2 || got > 8 {
		t.Fatalf("peak concurrent card runs=%d, want 2..8", got)
	}
}
