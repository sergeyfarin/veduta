// SPDX-License-Identifier: AGPL-3.0-or-later

package rules_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/rules"
	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

func TestDebounceRequiresSustainedTruthAndOwnDeadlineFires(t *testing.T) {
	store, cards, manager, declarations := ruleHarness(t, float64(95))
	definition := compileRule(t, config.Rule{ID: "hot", When: `signal("server", "cpu") > 90`, For: "40ms"}, declarations)
	if err := manager.Apply(context.Background(), []rules.Definition{definition}, declarations); err != nil {
		t.Fatal(err)
	}
	if err := manager.Evaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	setCardRun(t, cards, float64(20), nil)
	time.Sleep(50 * time.Millisecond)
	if countEvents(t, store, "rule.fired") != 0 {
		t.Fatal("flapping rule fired")
	}
	setCardRun(t, cards, float64(95), nil)
	waitForEvents(t, store, "rule.fired", 1)
}

func TestRestartResumesDebounceAndFreshFalseResolves(t *testing.T) {
	store, cards, manager, declarations := ruleHarness(t, float64(95))
	definition := compileRule(t, config.Rule{ID: "hot", When: `signal("server", "cpu") > 90`, For: "200ms", Resolve: true}, declarations)
	if err := manager.Apply(context.Background(), []rules.Definition{definition}, declarations); err != nil {
		t.Fatal(err)
	}
	if err := manager.Evaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	time.Sleep(80 * time.Millisecond)
	manager.Close()
	restarted := rules.New(context.Background(), store, cards, nil)
	defer restarted.Close()
	if err := restarted.Apply(context.Background(), []rules.Definition{definition}, declarations); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Evaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForEvents(t, store, "rule.fired", 1)
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("restart reset debounce window; fired after %s", elapsed)
	}
	setCardRun(t, cards, nil, errors.New("stale"))
	time.Sleep(15 * time.Millisecond)
	if countEvents(t, store, "rule.resolved") != 0 {
		t.Fatal("stale data resolved a fired rule")
	}
	setCardRun(t, cards, float64(20), nil)
	waitForEvents(t, store, "rule.resolved", 1)
}

func TestWrongSignalTypeEmitsOncePerBadEpisode(t *testing.T) {
	store, cards, manager, declarations := ruleHarness(t, "95")
	definition := compileRule(t, config.Rule{ID: "hot", When: `signal("server", "cpu") > 90`}, declarations)
	if err := manager.Apply(context.Background(), []rules.Definition{definition}, declarations); err != nil {
		t.Fatal(err)
	}
	if err := manager.Evaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	setCardRun(t, cards, "96", nil)
	time.Sleep(15 * time.Millisecond)
	if got := countEvents(t, store, "rule.signal-type"); got != 1 {
		t.Fatalf("wrong-type events=%d want=1", got)
	}
	setCardRun(t, cards, float64(20), nil)
	time.Sleep(15 * time.Millisecond)
	setCardRun(t, cards, "97", nil)
	waitForEvents(t, store, "rule.signal-type", 2)
}

func ruleHarness(t *testing.T, initial any) (*storage.Store, *scheduler.Manager, *rules.Manager, map[string]rules.CardDefinition) {
	t.Helper()
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cards := scheduler.New(store)
	setCardRun(t, cards, initial, nil)
	waitForCard(t, cards)
	manager := rules.New(context.Background(), store, cards, nil)
	t.Cleanup(func() { manager.Close(); cards.Close(); _ = store.Close() })
	return store, cards, manager, map[string]rules.CardDefinition{"server": {Hash: "card-hash", Signals: map[string]string{"cpu": "number"}}}
}

func setCardRun(t *testing.T, cards *scheduler.Manager, value any, runErr error) {
	t.Helper()
	err := cards.Apply(context.Background(), []scheduler.Definition{{ID: "server", Hash: "card-hash", Refresh: time.Hour, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{"cpu": {Value: value}}}, runErr
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if runErr == nil {
		waitForCard(t, cards)
	} else {
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForCard(t *testing.T, cards *scheduler.Manager) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		card, _ := cards.State("server")
		if card.Execution.State == "ok" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("card did not become fresh")
}

func compileRule(t *testing.T, rule config.Rule, declarations map[string]rules.CardDefinition) rules.Definition {
	t.Helper()
	definition, err := rules.Compile(rule, declarations)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func countEvents(t *testing.T, store *storage.Store, eventType string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM events WHERE type=?`, eventType).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func waitForEvents(t *testing.T, store *storage.Store, eventType string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if countEvents(t, store, eventType) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s events=%d want=%d", eventType, countEvents(t, store, eventType), want)
}
