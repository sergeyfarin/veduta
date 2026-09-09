// SPDX-License-Identifier: AGPL-3.0-or-later

package rules_test

import (
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/rules"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/widgets"
)

func TestCompileRejectsUnknownAndUndeclaredReferences(t *testing.T) {
	cards := map[string]rules.CardDefinition{"server": {Hash: "h", Signals: map[string]string{"cpu": "number"}}}
	for _, test := range []struct {
		name, expression string
	}{
		{"unknown card", `state("ghost") == "error"`},
		{"undeclared signal", `signal("server", "memory") > 90`},
		{"dynamic reference", `signal("server", name) > 90`},
		{"blocks inaccessible", `blocks[0] == 1`},
		{"builtins disabled", `len("x") > 0`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := rules.Compile(config.Rule{ID: "test", When: test.expression}, cards); err == nil {
				t.Fatal("invalid rule compiled")
			}
		})
	}
}

func TestEvaluateFreshnessTable(t *testing.T) {
	declarations := map[string]rules.CardDefinition{"server": {Hash: "h", Signals: map[string]string{"cpu": "number"}}}
	definition, err := rules.Compile(config.Rule{ID: "hot", When: `signal("server", "cpu") > 90`}, declarations)
	if err != nil {
		t.Fatal(err)
	}
	freshTrue := okCard("server", float64(95))
	freshFalse := okCard("server", float64(20))
	stale := state.Stale("server", *freshTrue.Document, state.Source{}, time.Now(), time.Now(), 1, time.Now().Add(time.Minute))
	open := state.StaleWithOpenCircuit("server", *freshTrue.Document, state.Source{}, time.Now(), time.Now(), 3, time.Now().Add(time.Minute))
	cases := []struct {
		name  string
		card  state.CardState
		want  rules.Result
		wrong bool
	}{
		{"fresh true", freshTrue, rules.ResultTrue, false},
		{"stale", stale, rules.ResultUnknown, false},
		{"absent", okCardWithoutSignal("server"), rules.ResultUnknown, false},
		{"null", okCard("server", nil), rules.ResultUnknown, false},
		{"wrong type", okCard("server", "95"), rules.ResultUnknown, true},
		{"open circuit", open, rules.ResultUnknown, false},
		{"fresh false", freshFalse, rules.ResultFalse, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, evaluateErr := rules.Evaluate(definition, map[string]state.CardState{"server": test.card}, declarations)
			if evaluateErr != nil || got.Result != test.want || (len(got.WrongTypes) > 0) != test.wrong {
				t.Fatalf("evaluation=%+v err=%v", got, evaluateErr)
			}
		})
	}
}

func TestStateFunctionAlertsIndependentlyOfSignalFreshness(t *testing.T) {
	declarations := map[string]rules.CardDefinition{"server": {Hash: "h"}}
	definition, err := rules.Compile(config.Rule{ID: "down", When: `state("server") == "error"`}, declarations)
	if err != nil {
		t.Fatal(err)
	}
	card := state.Error("server", state.Source{}, state.RunError{Code: state.ErrorUpstream, Message: "down"})
	got, err := rules.Evaluate(definition, map[string]state.CardState{"server": card}, declarations)
	if err != nil || got.Result != rules.ResultTrue {
		t.Fatalf("evaluation=%+v err=%v", got, err)
	}
}

func okCard(id string, value any) state.CardState {
	document := widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{"cpu": {Value: value}}}
	return state.OK(id, document, state.Source{}, time.Minute, 0)
}

func okCardWithoutSignal(id string) state.CardState {
	return state.OK(id, widgets.Document{Blocks: []widgets.Block{}, Signals: map[string]widgets.Signal{}}, state.Source{}, time.Minute, 0)
}
