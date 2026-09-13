// SPDX-License-Identifier: AGPL-3.0-or-later

// Package scheduler refreshes configured cards independently of connected viewers.
package scheduler

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"strconv"
	"sync"
	"time"

	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

// RunFunc produces the next document for a card.
type RunFunc func(context.Context) (widgets.Document, error)

// Definition is the immutable scheduling and execution identity of one card.
type Definition struct {
	ID, Hash, Key, ManifestDigest, ApprovalRevision, SlotRevisions string
	Refresh, Timeout                                               time.Duration
	Source                                                         state.Source
	Run                                                            RunFunc
	Disabled                                                       state.DisabledReason
	HistorySignals                                                 map[string]struct{}
	SignalTypes                                                    map[string]string
	generation                                                     uint64
}

// Event reports a committed card-state change.
type Event struct {
	ID    uint64
	State state.CardState
}

type flight struct {
	done     chan struct{}
	doc      widgets.Document
	err      error
	duration time.Duration
}

// Manager schedules card refreshes, persists results, and publishes state changes.
type Manager struct {
	store      *storage.Store
	mu         sync.RWMutex
	defs       map[string]Definition
	states     map[string]state.CardState
	failures   map[string]int
	openUntil  map[string]time.Time
	manualAt   map[string]time.Time
	flights    map[string]*flight
	subs       map[chan Event]struct{}
	secrets    *secrets.Registry
	workers    chan struct{}
	nextID     uint64
	generation uint64
	cancel     context.CancelFunc
}

var (
	// ErrUnknownCard indicates that no active definition has the requested ID.
	ErrUnknownCard = errors.New("scheduler: unknown card")
	// ErrRateLimited indicates that manual refresh was requested too frequently.
	ErrRateLimited = errors.New("scheduler: refresh rate limited")
	// ErrSuperseded indicates that a newer configuration replaced an in-flight definition.
	ErrSuperseded = errors.New("scheduler: invocation superseded by a newer configuration")
	// ErrSecretInDocument indicates that a produced document repeated a configured secret value
	// verbatim. Its message is deliberately fixed and value-free: it becomes a card's visible
	// error text, so it must describe the leak without repeating it.
	ErrSecretInDocument = errors.New("scheduler: document contains a configured secret value")
	// ErrRunPanicked indicates that a card's run panicked and was converted into a failed run
	// rather than being allowed to unwind. The panic value and its stack go to the log, through
	// the scrubbing handler; this message is what the card shows, and is fixed and value-free for
	// the same reason ErrSecretInDocument's is - a panic value is arbitrary in-process data, and
	// a dashboard tile is the wrong place to render it.
	ErrRunPanicked = errors.New("scheduler: the integration failed unexpectedly; see the server log")
)

// New creates a scheduler backed by store. A nil store disables persistence. Produced documents
// are checked against the process-wide secret registry - the one every resolved secrets.Value
// registers itself with - so the check is on by construction rather than by wiring at each call
// site; NewWithSecrets overrides that for tests.
func New(store *storage.Store) *Manager {
	return NewWithSecrets(store, secrets.DefaultRegistry())
}

// NewWithSecrets is New against a caller-supplied secret registry, so a test can use its own
// instance rather than the process-global one. A nil registry disables the check.
func NewWithSecrets(store *storage.Store, reg *secrets.Registry) *Manager {
	return &Manager{secrets: reg, store: store, defs: map[string]Definition{}, states: map[string]state.CardState{}, failures: map[string]int{}, openUntil: map[string]time.Time{}, manualAt: map[string]time.Time{}, flights: map[string]*flight{}, subs: map[chan Event]struct{}{}, workers: make(chan struct{}, 8)}
}

// Apply atomically replaces all definitions and starts their refresh loops.
func (m *Manager) Apply(parent context.Context, defs []Definition) error {
	preparedDefs := make(map[string]Definition, len(defs))
	preparedStates := make(map[string]state.CardState, len(defs))
	preparedFailures := make(map[string]int, len(defs))
	preparedOpen := make(map[string]time.Time, len(defs))
	for i := range defs {
		d := defs[i]
		if d.Refresh <= 0 {
			d.Refresh = time.Minute
		}
		if d.Timeout <= 0 {
			d.Timeout = 10 * time.Second
		}
		if d.ID == "" {
			return errors.New("scheduler: card id is empty")
		}
		if _, exists := preparedDefs[d.ID]; exists {
			return errors.New("scheduler: duplicate card id " + d.ID)
		}
		defs[i] = d
		preparedDefs[d.ID] = d
		preparedStates[d.ID] = state.Pending(d.ID)
		if m.store != nil {
			if old, ok, err := m.store.GetCardFor(parent, d.ID, storage.CardRecord{CardHash: d.Hash, ManifestDigest: d.ManifestDigest, ApprovalRevision: d.ApprovalRevision, SlotRevisions: d.SlotRevisions}); err != nil {
				return err
			} else if ok && !m.documentLeaksSecret(old.Document) {
				preparedStates[d.ID] = restored(old, d.Source)
				preparedFailures[d.ID] = old.Execution.ConsecutiveFailures
				preparedOpen[d.ID] = parseTime(old.Execution.CircuitOpenUntil)
			}
		}
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	m.generation++
	generation := m.generation
	for id, d := range preparedDefs {
		d.generation = generation
		preparedDefs[id] = d
	}
	m.defs = preparedDefs
	m.states = preparedStates
	m.failures = preparedFailures
	m.openUntil = preparedOpen
	m.manualAt = map[string]time.Time{}
	m.mu.Unlock()
	for _, d := range preparedDefs {
		go m.loop(ctx, d)
	}
	return nil
}

// Definitions returns the active definitions for rolling back a composed configuration change.
func (m *Manager) Definitions() []Definition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Definition, 0, len(m.defs))
	for _, definition := range m.defs {
		out = append(out, definition)
	}
	return out
}

// Close stops refresh loops and closes all subscriptions.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	for ch := range m.subs {
		close(ch)
		delete(m.subs, ch)
	}
	m.mu.Unlock()
}
func (m *Manager) loop(ctx context.Context, d Definition) {
	for {
		err := m.refresh(ctx, d)
		wait := jitter(d.ID, d.Refresh)
		if err == nil {
			m.mu.RLock()
			cs := m.states[d.ID]
			m.mu.RUnlock()
			if next := parseTime(cs.Execution.NextRunAt); !next.IsZero() && time.Until(next) < wait {
				wait = time.Until(next)
			}
		}
		if wait < 0 {
			wait = 0
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// Refresh executes a named card through the shared-flight path.
func (m *Manager) Refresh(ctx context.Context, id string) error {
	m.mu.RLock()
	d, ok := m.defs[id]
	m.mu.RUnlock()
	if !ok {
		return ErrUnknownCard
	}
	return m.refresh(ctx, d)
}

// RefreshNow is the operator-facing refresh path. It is single-flighted by Refresh and also
// throttled so a browser cannot turn the endpoint into an upstream request loop.
func (m *Manager) RefreshNow(ctx context.Context, id string) error {
	m.mu.Lock()
	d, ok := m.defs[id]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownCard
	}
	now := time.Now()
	if last := m.manualAt[id]; !last.IsZero() && now.Sub(last) < time.Second {
		m.mu.Unlock()
		return ErrRateLimited
	}
	m.manualAt[id] = now
	m.mu.Unlock()
	return m.refresh(ctx, d)
}

func (m *Manager) refresh(ctx context.Context, d Definition) error {
	id := d.ID
	m.mu.RLock()
	current, exists := m.defs[id]
	open := m.openUntil[id]
	m.mu.RUnlock()
	if !exists || current.generation != d.generation {
		return ErrSuperseded
	}
	if d.Disabled != "" {
		return m.commit(ctx, d, state.Disabled(id, nil, d.Disabled))
	}
	if time.Now().Before(open) {
		return nil
	}
	if d.Run == nil {
		return m.commit(ctx, d, state.Disabled(id, nil, state.ReasonConfigError))
	}
	doc, dur, runErr := m.runShared(ctx, d)
	if runErr == nil && m.documentLeaksSecret(&doc) {
		// An integration never receives a credential, so a verbatim match means a resolved
		// secret came back through the data path - a compromised or merely careless upstream
		// echoing a header, say. Drop the document rather than storing or serving it, and fail
		// the run so the card shows an error (and the breaker eventually opens) instead of
		// silently rendering nothing. This is the production caller docs/03-backlog-resolved.md's
		// "ContainsSecretInDocument had no production caller" entry asked for; the log scrubber
		// and CI's secret-response scan stay as the layers around it.
		doc = widgets.Document{}
		runErr = ErrSecretInDocument
	}
	var history map[string]float64
	if runErr == nil {
		var historyErr error
		history, historyErr = historicalValues(d, doc)
		if historyErr != nil {
			runErr = historyErr
		}
	}
	if runErr == nil {
		ttl := d.Refresh
		if doc.Hints != nil && doc.Hints.TTLSeconds > 0 && time.Duration(doc.Hints.TTLSeconds)*time.Second < ttl {
			ttl = time.Duration(doc.Hints.TTLSeconds) * time.Second
		}
		if ttl < time.Second {
			ttl = time.Second
		}
		m.mu.Lock()
		m.failures[id] = 0
		delete(m.openUntil, id)
		m.mu.Unlock()
		return m.commitWithHistory(ctx, d, state.OK(id, doc, d.Source, ttl, dur), history)
	}
	m.mu.Lock()
	m.failures[id]++
	failures := m.failures[id]
	previous := m.states[id]
	m.mu.Unlock()
	retry := time.Now().Add(backoff(failures, d.Refresh))
	re := state.RunError{Code: classify(runErr), Message: runErr.Error(), Retryable: true}
	if failures >= 3 {
		open := time.Now().Add(minDuration(5*time.Minute, backoff(failures, d.Refresh)))
		m.mu.Lock()
		m.openUntil[id] = open
		m.mu.Unlock()
		if previous.Document != nil {
			return m.commit(ctx, d, state.StaleAfterErrorWithOpenCircuit(id, *previous.Document, d.Source, parseTime(previous.Execution.GeneratedAt), time.Now(), failures, open, re))
		}
		return m.commit(ctx, d, state.ErrorWithOpenCircuit(id, d.Source, re, open))
	}
	if previous.Document != nil {
		return m.commit(ctx, d, state.StaleAfterError(id, *previous.Document, d.Source, parseTime(previous.Execution.GeneratedAt), time.Now(), failures, retry, re))
	}
	cs := state.Error(id, d.Source, re)
	cs.Execution.ConsecutiveFailures = failures
	cs.Execution.NextRunAt = retry.UTC().Format(time.RFC3339Nano)
	return m.commit(ctx, d, cs)
}

// documentLeaksSecret reports whether doc repeats a configured secret value verbatim. It guards
// both paths a document can take into m.states: a fresh run, and a persisted document restored
// by Apply - which may predate the secret that now matches it, so the stored card is dropped and
// the card starts pending rather than serving it again.
func (m *Manager) documentLeaksSecret(doc *widgets.Document) bool {
	return m.secrets != nil && doc != nil && m.secrets.ContainsSecretInDocument(*doc)
}

func historicalValues(d Definition, doc widgets.Document) (map[string]float64, error) {
	out := make(map[string]float64, len(d.HistorySignals))
	for name := range d.HistorySignals {
		signal, ok := doc.Signals[name]
		if !ok || signal.Value == nil {
			continue
		}
		value, ok := signal.Value.(float64)
		if !ok {
			return nil, errors.New("scheduler: historical signal " + name + " changed type")
		}
		out[name] = value
	}
	return out, nil
}

func (m *Manager) runShared(ctx context.Context, d Definition) (widgets.Document, time.Duration, error) {
	key := d.Key
	if key == "" {
		key = d.ID
	}
	key = strconv.FormatUint(d.generation, 10) + ":" + key
	m.mu.Lock()
	if f := m.flights[key]; f != nil {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return widgets.Document{}, 0, ctx.Err()
		case <-f.done:
			return f.doc, f.duration, f.err
		}
	}
	f := &flight{done: make(chan struct{})}
	m.flights[key] = f
	m.mu.Unlock()
	// The flight is visible to every other caller from the line above, and they block on f.done.
	// Releasing it on the way out, whatever the reason, is what makes that safe: this used to
	// happen inline on each return path, so a panic below left the key occupied and the channel
	// unclosed, and every later refresh of that card blocked on it for the life of the
	// generation. d.Run no longer panics past its own barrier (see internal/app), but the
	// invariant here should not depend on a caller in another package getting that right.
	defer func() {
		m.mu.Lock()
		delete(m.flights, key)
		close(f.done)
		m.mu.Unlock()
	}()
	select {
	case m.workers <- struct{}{}:
		defer func() { <-m.workers }()
	case <-ctx.Done():
		f.err = ctx.Err()
		return widgets.Document{}, 0, ctx.Err()
	}
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, d.Timeout)
	f.doc, f.err = d.Run(runCtx)
	cancel()
	f.duration = time.Since(start)
	return f.doc, f.duration, f.err
}

func (m *Manager) commit(ctx context.Context, d Definition, cs state.CardState) error {
	return m.commitWithHistory(ctx, d, cs, nil)
}

func (m *Manager) commitWithHistory(ctx context.Context, d Definition, cs state.CardState, history map[string]float64) error {
	if err := cs.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	current, ok := m.defs[d.ID]
	if !ok || current.generation != d.generation {
		m.mu.Unlock()
		return ErrSuperseded
	}
	if m.store != nil {
		if err := m.store.PutCardWithSignals(ctx, storage.CardRecord{CardHash: d.Hash, ManifestDigest: d.ManifestDigest, ApprovalRevision: d.ApprovalRevision, SlotRevisions: d.SlotRevisions, State: cs}, history); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.states[d.ID] = cs
	m.nextID++
	ev := Event{ID: m.nextID, State: cs}
	for ch := range m.subs {
		select {
		case ch <- ev:
		default:
			close(ch)
			delete(m.subs, ch)
		}
	}
	m.mu.Unlock()
	return nil
}

// States returns a card-ID-sorted snapshot of every current state.
func (m *Manager) States() []state.CardState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]state.CardState, 0, len(m.defs))
	for id := range m.defs {
		out = append(out, m.states[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CardID < out[j].CardID })
	return out
}

// State returns the current state for id.
func (m *Manager) State(id string) (state.CardState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.states[id]
	return v, ok
}

// Subscribe returns a bounded state-event stream and its cancellation function.
func (m *Manager) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan Event, buffer)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		if _, ok := m.subs[ch]; ok {
			delete(m.subs, ch)
			close(ch)
		}
		m.mu.Unlock()
	}
}

func backoff(n int, base time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	for i := 1; i < n && base < time.Hour; i++ {
		base *= 2
	}
	return minDuration(base, time.Hour)
}
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
func parseTime(v string) time.Time { t, _ := time.Parse(time.RFC3339Nano, v); return t }
func restored(cs state.CardState, source state.Source) state.CardState {
	if cs.Execution.State == state.StateOK {
		expires := parseTime(cs.Execution.ExpiresAt)
		if !expires.IsZero() && !time.Now().Before(expires) && cs.Document != nil {
			return state.Stale(cs.CardID, *cs.Document, source, parseTime(cs.Execution.GeneratedAt), expires, 0, time.Now())
		}
	}
	return cs
}
func classify(err error) state.ErrorCode {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return state.ErrorTimeout
	}
	if errors.Is(err, ErrSecretInDocument) {
		return state.ErrorInvalid
	}
	if errors.Is(err, ErrRunPanicked) {
		return state.ErrorInternal
	}
	return state.ErrorUpstream
}

func jitter(id string, base time.Duration) time.Duration {
	if base <= 0 {
		return time.Minute
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	// Stable +/-5% jitter avoids a synchronised herd after restart while keeping tests and
	// operator expectations reproducible.
	percent := int(h.Sum32()%11) - 5
	return base + time.Duration(int64(base)*int64(percent)/100)
}
