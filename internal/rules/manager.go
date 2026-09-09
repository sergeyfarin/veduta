// SPDX-License-Identifier: AGPL-3.0-or-later

package rules

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
)

// Manager evaluates rules on card changes and owns persisted debounce wake-ups.
type Manager struct {
	store         *storage.Store
	scheduler     *scheduler.Manager
	logger        *slog.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	unsubscribe   func()
	mu            sync.Mutex
	definitions   map[string]Definition
	declarations  map[string]CardDefinition
	states        map[string]storage.RuleState
	timers        map[string]*time.Timer
	wrongReported map[string]bool
	notify        func(context.Context, Alert) error
	active        bool
}

// Alert is the credential-free notification request produced by a rule transition.
type Alert struct {
	RuleID, Event, Severity string
	Channels                []string
}

// New starts a rule manager subscribed to scheduler state commits.
func New(parent context.Context, store *storage.Store, cards *scheduler.Manager, logger *slog.Logger) *Manager {
	return NewWithNotifier(parent, store, cards, logger, nil)
}

// NewWithNotifier also sends fired and resolved transitions to a durable notification sink.
func NewWithNotifier(parent context.Context, store *storage.Store, cards *scheduler.Manager, logger *slog.Logger, notifier func(context.Context, Alert) error) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(parent)
	updates, unsubscribe := cards.Subscribe(32)
	manager := &Manager{store: store, scheduler: cards, logger: logger, ctx: ctx, cancel: cancel, unsubscribe: unsubscribe, definitions: map[string]Definition{}, declarations: map[string]CardDefinition{}, states: map[string]storage.RuleState{}, timers: map[string]*time.Timer{}, wrongReported: map[string]bool{}, notify: notifier}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-updates:
				if !ok {
					return
				}
				if err := manager.evaluateAll(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("evaluate rules", "error", err)
				}
			}
		}
	}()
	return manager
}

// Close stops subscriptions and deadline timers.
func (m *Manager) Close() {
	m.cancel()
	m.unsubscribe()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, timer := range m.timers {
		timer.Stop()
	}
	m.timers = map[string]*time.Timer{}
}

// Apply atomically replaces the active rule definitions and rebuilds persisted deadlines.
func (m *Manager) Apply(ctx context.Context, definitions []Definition, declarations map[string]CardDefinition) error {
	states := make(map[string]storage.RuleState, len(definitions))
	defs := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		if _, exists := defs[definition.ID]; exists {
			return errors.New("rules: duplicate rule id " + definition.ID)
		}
		defs[definition.ID] = definition
		persisted, ok, err := m.store.GetRuleState(ctx, definition.ID, definition.Hash)
		if err != nil {
			return err
		}
		if !ok {
			persisted = storage.RuleState{RuleID: definition.ID, RuleHash: definition.Hash, LastResult: string(ResultUnknown)}
		}
		states[definition.ID] = persisted
	}
	m.mu.Lock()
	for _, timer := range m.timers {
		timer.Stop()
	}
	m.definitions, m.declarations, m.states = defs, declarations, states
	m.timers = map[string]*time.Timer{}
	m.wrongReported = map[string]bool{}
	m.active = false
	m.mu.Unlock()
	return nil
}

// Evaluate evaluates every rule against one coherent scheduler snapshot.
func (m *Manager) Evaluate(ctx context.Context) error {
	m.mu.Lock()
	m.active = true
	m.mu.Unlock()
	return m.evaluateAll(ctx)
}

// Definitions returns a copy suitable for rolling back a configuration transaction.
func (m *Manager) Definitions() ([]Definition, map[string]CardDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	definitions := make([]Definition, 0, len(m.definitions))
	for _, definition := range m.definitions {
		definitions = append(definitions, definition)
	}
	declarations := make(map[string]CardDefinition, len(m.declarations))
	for id, declaration := range m.declarations {
		signals := make(map[string]string, len(declaration.Signals))
		for name, signalType := range declaration.Signals {
			signals[name] = signalType
		}
		declaration.Signals = signals
		declarations[id] = declaration
	}
	return definitions, declarations
}

func (m *Manager) evaluateAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return nil
	}
	cards := make(map[string]state.CardState)
	for _, card := range m.scheduler.States() {
		cards[card.CardID] = card
	}
	for id := range m.definitions {
		if err := m.evaluateLocked(ctx, id, cards, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) evaluateLocked(ctx context.Context, id string, cards map[string]state.CardState, now time.Time) error {
	definition, exists := m.definitions[id]
	if !exists {
		return nil
	}
	evaluation, err := Evaluate(definition, cards, m.declarations)
	if err != nil {
		return err
	}
	current := m.states[id]
	if len(evaluation.WrongTypes) > 0 && !m.wrongReported[id] {
		data, _ := json.Marshal(map[string]any{"rule": id, "signals": evaluation.WrongTypes})
		if err = m.store.AppendEvent(ctx, storage.Event{Type: "rule.signal-type", Severity: "warning", Source: "rules", Message: "Declared signal has the wrong runtime type", Data: data}); err != nil {
			return err
		}
		m.wrongReported[id] = true
	} else if len(evaluation.WrongTypes) == 0 {
		m.wrongReported[id] = false
	}
	if timer := m.timers[id]; timer != nil {
		timer.Stop()
		delete(m.timers, id)
	}
	switch evaluation.Result {
	case ResultTrue:
		if current.Since.IsZero() {
			current.Since = now
			current.Deadline = now.Add(definition.For)
		}
		if !now.Before(current.Deadline) && current.FiredAt.IsZero() {
			if err = m.appendRuleEvent(ctx, definition, "rule.fired", now); err != nil {
				return err
			}
			if m.notify != nil {
				err = m.notify(ctx, Alert{RuleID: definition.ID, Event: "fired", Severity: definition.Severity, Channels: definition.Notify})
				if err != nil {
					return err
				}
			}
			current.FiredAt = now
			current.ResolvedAt = time.Time{}
		} else if current.FiredAt.IsZero() {
			m.scheduleLocked(id, definition.Hash, time.Until(current.Deadline))
		}
	case ResultFalse:
		current.Since, current.Deadline = time.Time{}, time.Time{}
		if !current.FiredAt.IsZero() {
			if definition.Resolve {
				if err = m.appendRuleEvent(ctx, definition, "rule.resolved", now); err != nil {
					return err
				}
				if m.notify != nil {
					err = m.notify(ctx, Alert{RuleID: definition.ID, Event: "resolved", Severity: definition.Severity, Channels: definition.Notify})
					if err != nil {
						return err
					}
				}
			}
			current.ResolvedAt = now
			current.FiredAt = time.Time{}
		}
	case ResultUnknown:
		current.Since, current.Deadline = time.Time{}, time.Time{}
	}
	current.RuleID, current.RuleHash, current.LastResult = id, definition.Hash, string(evaluation.Result)
	if err = m.store.PutRuleState(ctx, current); err != nil {
		return err
	}
	m.states[id] = current
	return nil
}

func (m *Manager) scheduleLocked(id, hash string, delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	m.timers[id] = time.AfterFunc(delay, func() {
		m.mu.Lock()
		definition, ok := m.definitions[id]
		m.mu.Unlock()
		if !ok || definition.Hash != hash {
			return
		}
		if err := m.evaluateAll(m.ctx); err != nil && !errors.Is(err, context.Canceled) {
			m.logger.Error("evaluate rule deadline", "rule", id, "error", err)
		}
	})
}

func (m *Manager) appendRuleEvent(ctx context.Context, definition Definition, eventType string, now time.Time) error {
	data, err := json.Marshal(map[string]any{"rule": definition.ID, "notify": definition.Notify})
	if err != nil {
		return err
	}
	message := "Rule fired"
	if eventType == "rule.resolved" {
		message = "Rule resolved"
	}
	return m.store.AppendEvent(ctx, storage.Event{Timestamp: now.Format(time.RFC3339Nano), Type: eventType, Severity: definition.Severity, Source: "rules", Message: message, Data: data})
}
