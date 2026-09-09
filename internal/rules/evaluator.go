// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rules compiles and evaluates the deliberately small Veduta rule language.
package rules

import (
	"errors"
	"fmt"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
)

// Result is the three-valued result of a rule predicate.
type Result string

const (
	// ResultTrue means the predicate is freshly satisfied.
	ResultTrue Result = "true"
	// ResultFalse means the predicate is freshly unsatisfied.
	ResultFalse Result = "false"
	// ResultUnknown means at least one signal observation was not fresh and type-valid.
	ResultUnknown Result = "unknown"
)

// CardDefinition is the load-time authority a rule may reference.
type CardDefinition struct {
	Hash    string
	Signals map[string]string
}

// Definition is a checked, compiled rule.
type Definition struct {
	ID       string
	Hash     string
	For      time.Duration
	Severity string
	Notify   []string
	Resolve  bool
	program  *vm.Program
}

// Evaluation contains the predicate result and any declared signals that arrived with a wrong type.
type Evaluation struct {
	Result     Result
	WrongTypes []string
}

type references struct {
	cards map[string]string
	err   error
}

// Visit collects literal signal and state references from the parsed expression tree.
func (v *references) Visit(node *ast.Node) {
	call, ok := (*node).(*ast.CallNode)
	if !ok {
		return
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || (callee.Value != "signal" && callee.Value != "state") {
		return
	}
	want := 1
	if callee.Value == "signal" {
		want = 2
	}
	if len(call.Arguments) != want {
		v.err = fmt.Errorf("rules: %s requires %d literal string arguments", callee.Value, want)
		return
	}
	values := make([]string, want)
	for i, argument := range call.Arguments {
		literal, literalOK := argument.(*ast.StringNode)
		if !literalOK {
			v.err = fmt.Errorf("rules: %s arguments must be literal strings", callee.Value)
			return
		}
		values[i] = literal.Value
	}
	if callee.Value == "signal" {
		v.cards[values[0]+"\x00"+values[1]] = "signal"
	} else {
		v.cards[values[0]] = "state"
	}
}

// Compile validates literal card/signal references and compiles a boolean expression with every
// expr builtin disabled. Only signal(card,name), state(card), literals, and operators remain.
func Compile(rule config.Rule, cards map[string]CardDefinition) (Definition, error) {
	tree, err := parser.Parse(rule.When)
	if err != nil {
		return Definition{}, fmt.Errorf("rule %q: %w", rule.ID, err)
	}
	refs := &references{cards: map[string]string{}}
	ast.Walk(&tree.Node, refs)
	if refs.err != nil {
		return Definition{}, fmt.Errorf("rule %q: %w", rule.ID, refs.err)
	}
	cardHashes := make(map[string]string)
	for ref, kind := range refs.cards {
		cardID, signalName := splitReference(ref)
		card, ok := cards[cardID]
		if !ok {
			return Definition{}, fmt.Errorf("rule %q: references unknown card %q", rule.ID, cardID)
		}
		cardHashes[cardID] = card.Hash
		if kind == "signal" {
			if _, declared := card.Signals[signalName]; !declared {
				return Definition{}, fmt.Errorf("rule %q: signal %q is not declared by card %q", rule.ID, signalName, cardID)
			}
		}
	}
	program, err := expr.Compile(rule.When,
		expr.Env(map[string]any{
			"signal": func(string, string) any { return float64(0) },
			"state":  func(string) string { return "pending" },
		}),
		expr.DisableAllBuiltins(), expr.AsBool())
	if err != nil {
		return Definition{}, fmt.Errorf("rule %q: %w", rule.ID, err)
	}
	forDuration, err := parseDuration(rule.For)
	if err != nil {
		return Definition{}, fmt.Errorf("rule %q: %w", rule.ID, err)
	}
	severity := rule.Severity
	if severity == "" {
		severity = "warning"
	}
	hash, err := storage.DefinitionHash(struct {
		Rule       config.Rule
		CardHashes map[string]string
	}{rule, cardHashes})
	if err != nil {
		return Definition{}, err
	}
	return Definition{ID: rule.ID, Hash: hash, For: forDuration, Severity: severity, Notify: append([]string(nil), rule.Notify...), Resolve: rule.Resolve, program: program}, nil
}

func splitReference(ref string) (string, string) {
	for i := range len(ref) {
		if ref[i] == 0 {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}

func parseDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, errors.New("invalid debounce duration")
	}
	return duration, nil
}

// Evaluate is pure over the current card-state snapshot. A stale, absent, null, or wrongly typed
// signal marks the whole predicate unknown; state() always reads core-owned execution state.
func Evaluate(definition Definition, cards map[string]state.CardState, declarations map[string]CardDefinition) (Evaluation, error) {
	unknown := false
	wrongTypes := make([]string, 0)
	env := map[string]any{
		"signal": func(cardID, name string) any {
			card, ok := cards[cardID]
			declaredType := declarations[cardID].Signals[name]
			if !ok || card.Execution.State != state.StateOK || card.Document == nil {
				unknown = true
				return float64(0)
			}
			signal, ok := card.Document.Signals[name]
			if !ok || signal.Value == nil {
				unknown = true
				return float64(0)
			}
			switch declaredType {
			case "number":
				value, valueOK := signal.Value.(float64)
				if !valueOK {
					unknown = true
					wrongTypes = append(wrongTypes, cardID+"."+name)
					return float64(0)
				}
				return value
			case "string":
				value, valueOK := signal.Value.(string)
				if !valueOK {
					unknown = true
					wrongTypes = append(wrongTypes, cardID+"."+name)
					return ""
				}
				return value
			case "boolean":
				value, valueOK := signal.Value.(bool)
				if !valueOK {
					unknown = true
					wrongTypes = append(wrongTypes, cardID+"."+name)
					return false
				}
				return value
			default:
				unknown = true
				return float64(0)
			}
		},
		"state": func(cardID string) string {
			card, ok := cards[cardID]
			if !ok {
				return string(state.StatePending)
			}
			return string(card.Execution.State)
		},
	}
	output, err := expr.Run(definition.program, env)
	if unknown {
		return Evaluation{Result: ResultUnknown, WrongTypes: wrongTypes}, nil
	}
	if err != nil {
		return Evaluation{}, err
	}
	value, ok := output.(bool)
	if !ok {
		return Evaluation{}, errors.New("rules: predicate did not return bool")
	}
	if value {
		return Evaluation{Result: ResultTrue}, nil
	}
	return Evaluation{Result: ResultFalse}, nil
}
