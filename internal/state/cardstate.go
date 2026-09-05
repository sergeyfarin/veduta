// SPDX-License-Identifier: AGPL-3.0-or-later

// Package state implements the CardState envelope: what the API and SSE return for a card. It
// separates integration-owned content (a widgets.Document) from core-owned execution facts -
// freshness, staleness, error, provenance - which an integration cannot influence. See
// schemas/card-state.v1.schema.json and docs/01-architecture.md sections 4 and 11.
package state

import (
	"fmt"
	"time"

	"veduta.dev/veduta/internal/widgets"
)

// ExecutionState is one of the five legal states a card can be in. The schema's own
// documentation is reproduced here because it is the whole point of this type: pending never
// ran, ok is fresh, stale is a retained last-good document past its TTL or after a failed run,
// error has no usable document, and disabled will not run without a human (see DisabledReason) -
// which is NOT what an open circuit breaker is (see docs/01-architecture.md section 4, D24).
type ExecutionState string

// ExecutionState values.
const (
	StatePending  ExecutionState = "pending"
	StateOK       ExecutionState = "ok"
	StateStale    ExecutionState = "stale"
	StateError    ExecutionState = "error"
	StateDisabled ExecutionState = "disabled"
)

// DisabledReason explains why a card in StateDisabled will not run without a human. Every value
// means exactly that - a tripped circuit breaker is deliberately NOT one of them; it is
// represented as Stale or Error carrying CircuitOpenUntil instead (D24).
type DisabledReason string

// DisabledReason values.
const (
	ReasonUnapproved         DisabledReason = "unapproved"
	ReasonPermissionsChanged DisabledReason = "permissions-changed"
	ReasonIntegrationMissing DisabledReason = "integration-missing"
	ReasonConfigError        DisabledReason = "config-error"
	ReasonOperator           DisabledReason = "operator"
)

// ErrorCode classifies why a run failed.
type ErrorCode string

// ErrorCode values.
const (
	ErrorUpstream ErrorCode = "upstream"
	ErrorAuth     ErrorCode = "auth"
	ErrorTimeout  ErrorCode = "timeout"
	ErrorConfig   ErrorCode = "config"
	ErrorDenied   ErrorCode = "denied"
	ErrorInvalid  ErrorCode = "invalid"
	ErrorLimit    ErrorCode = "limit"
	ErrorInternal ErrorCode = "internal"
)

// Runtime identifies which of the three runtimes produced a card.
type Runtime string

// Runtime values.
const (
	RuntimeBuiltin     Runtime = "builtin"
	RuntimeDeclarative Runtime = "declarative"
	RuntimeWASM        Runtime = "wasm"
)

// Source records provenance: which integration, operation and runtime produced this card, and
// which connection each of its slots was bound to.
type Source struct {
	Integration        string            `json:"integration,omitempty"`
	IntegrationVersion string            `json:"integrationVersion,omitempty"`
	Runtime            Runtime           `json:"runtime,omitempty"`
	Operation          string            `json:"operation,omitempty"`
	Slots              map[string]string `json:"slots,omitempty"`
}

// RunError is a failed invocation's cause. Distinct from widgets.Notice, which is an
// integration-reported SOFT problem inside an otherwise-successful run.
type RunError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable,omitempty"`
	At        string    `json:"at,omitempty"`
}

// Execution is core-owned. Nothing in this package accepts one from untrusted input - it is only
// ever produced by the five constructors below, each of which corresponds to exactly one legal
// (state, field) combination from the schema's conditional validation, so an invalid combination
// cannot be constructed by calling code, correct or otherwise. This is what makes "an integration
// cannot influence any field under execution" a property of the type system, not a convention.
type Execution struct {
	State               ExecutionState `json:"state"`
	GeneratedAt         string         `json:"generatedAt,omitempty"`
	TTLSeconds          int            `json:"ttlSeconds,omitempty"`
	ExpiresAt           string         `json:"expiresAt,omitempty"`
	StaleSince          string         `json:"staleSince,omitempty"`
	DurationMs          int            `json:"durationMs,omitempty"`
	ConsecutiveFailures int            `json:"consecutiveFailures,omitempty"`
	NextRunAt           string         `json:"nextRunAt,omitempty"`
	Source              *Source        `json:"source,omitempty"`
	Error               *RunError      `json:"error,omitempty"`
	DisabledReason      DisabledReason `json:"disabledReason,omitempty"`
	CircuitOpenUntil    string         `json:"circuitOpenUntil,omitempty"`
}

// CardState is the complete envelope: what every API response and SSE `card` event carries.
// Document is nil exactly when the schema says it must be (Pending, Error); everywhere else it
// is the last successfully validated widgets.Document, retained across failures.
type CardState struct {
	CardID    string            `json:"cardId"`
	Document  *widgets.Document `json:"document"`
	Execution Execution         `json:"execution"`
}

// nowRFC3339 is the one place "the current time" is formatted for the wire, so every constructor
// produces a value that both re-parses (time.Parse) and matches the schema's structural pattern.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Pending is the state before a card has ever run: no document, no timestamps, no error.
func Pending(cardID string) CardState {
	return CardState{CardID: cardID, Document: nil, Execution: Execution{State: StatePending}}
}

// OK is a fresh, successfully validated run. ttl is clamped by the caller to the card's
// configured schedule and the core's own maximum before it ever reaches here - Execution stores
// only the EFFECTIVE value, per docs/01-architecture.md section 6 ("every limit has one
// effective value").
func OK(cardID string, doc widgets.Document, src Source, ttl time.Duration, duration time.Duration) CardState {
	now := time.Now().UTC()
	return CardState{
		CardID:   cardID,
		Document: &doc,
		Execution: Execution{
			State:       StateOK,
			GeneratedAt: now.Format(time.RFC3339Nano),
			TTLSeconds:  int(ttl.Seconds()),
			ExpiresAt:   now.Add(ttl).Format(time.RFC3339Nano),
			DurationMs:  int(duration.Milliseconds()),
			Source:      &src,
		},
	}
}

// Stale retains the last good document after a run failed, or after the document's TTL passed
// with nothing newer to show. generatedAt/staleSince describe the RETAINED document, not now.
func Stale(cardID string, doc widgets.Document, src Source, generatedAt time.Time, staleSince time.Time,
	consecutiveFailures int, nextRunAt time.Time) CardState {
	return CardState{
		CardID:   cardID,
		Document: &doc,
		Execution: Execution{
			State:               StateStale,
			GeneratedAt:         generatedAt.UTC().Format(time.RFC3339Nano),
			StaleSince:          staleSince.UTC().Format(time.RFC3339Nano),
			ConsecutiveFailures: consecutiveFailures,
			NextRunAt:           nextRunAtOrEmpty(nextRunAt),
			Source:              &src,
		},
	}
}

// StaleWithOpenCircuit is Stale, plus the breaker's half-open probe time. circuitOpenUntil and
// nextRunAt are the SAME event seen from the breaker and the scheduler and must be equal per the
// schema's own documentation; nextRunAt is therefore not a separate parameter here.
func StaleWithOpenCircuit(cardID string, doc widgets.Document, src Source, generatedAt time.Time,
	staleSince time.Time, consecutiveFailures int, circuitOpenUntil time.Time) CardState {
	cs := Stale(cardID, doc, src, generatedAt, staleSince, consecutiveFailures, circuitOpenUntil)
	cs.Execution.CircuitOpenUntil = circuitOpenUntil.UTC().Format(time.RFC3339Nano)
	return cs
}

// Error is a failed run with no usable document at all.
func Error(cardID string, src Source, err RunError) CardState {
	if err.At == "" {
		err.At = nowRFC3339()
	}
	return CardState{
		CardID:   cardID,
		Document: nil,
		Execution: Execution{
			State:  StateError,
			Source: &src,
			Error:  &err,
		},
	}
}

// ErrorWithOpenCircuit is Error, plus the breaker's half-open probe time.
func ErrorWithOpenCircuit(cardID string, src Source, err RunError, circuitOpenUntil time.Time) CardState {
	cs := Error(cardID, src, err)
	cs.Execution.NextRunAt = circuitOpenUntil.UTC().Format(time.RFC3339Nano)
	cs.Execution.CircuitOpenUntil = circuitOpenUntil.UTC().Format(time.RFC3339Nano)
	return cs
}

// Disabled means the card will not run again without a human resolving reason. A retained
// document is permitted (unlike Error) so the UI can still show the last thing that worked.
func Disabled(cardID string, doc *widgets.Document, reason DisabledReason) CardState {
	return CardState{
		CardID:   cardID,
		Document: doc,
		Execution: Execution{
			State:          StateDisabled,
			DisabledReason: reason,
		},
	}
}

func nextRunAtOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// Validate re-checks a CardState's internal consistency at the type level, independent of JSON
// Schema: every constructor above already produces a legal combination, so this exists to catch
// a FUTURE constructor (or a struct literal built by mistake, bypassing them) that does not.
// Mirrors the schema's "allOf" conditional validation in docs/01 and schemas/card-state.v1.
func (c CardState) Validate() error {
	e := c.Execution
	switch e.State {
	case StatePending:
		if c.Document != nil {
			return fmt.Errorf("state pending: document must be nil")
		}
		if e.GeneratedAt != "" || e.ExpiresAt != "" || e.StaleSince != "" || e.Error != nil || e.CircuitOpenUntil != "" {
			return fmt.Errorf("state pending: no execution timestamps or error are permitted")
		}
	case StateOK:
		if c.Document == nil {
			return fmt.Errorf("state ok: document is required")
		}
		if e.GeneratedAt == "" || e.TTLSeconds == 0 || e.ExpiresAt == "" {
			return fmt.Errorf("state ok: generatedAt, ttlSeconds and expiresAt are required")
		}
		if e.Error != nil || e.StaleSince != "" || e.DisabledReason != "" || e.CircuitOpenUntil != "" {
			return fmt.Errorf("state ok: error, staleSince, disabledReason and circuitOpenUntil must be absent")
		}
	case StateStale:
		if c.Document == nil {
			return fmt.Errorf("state stale: document (last-good) is required")
		}
		if e.GeneratedAt == "" || e.StaleSince == "" {
			return fmt.Errorf("state stale: generatedAt and staleSince are required")
		}
		if e.DisabledReason != "" {
			return fmt.Errorf("state stale: disabledReason must be absent")
		}
	case StateError:
		if c.Document != nil {
			return fmt.Errorf("state error: document must be nil")
		}
		if e.Error == nil {
			return fmt.Errorf("state error: error is required")
		}
		if e.DisabledReason != "" {
			return fmt.Errorf("state error: disabledReason must be absent")
		}
	case StateDisabled:
		if e.DisabledReason == "" {
			return fmt.Errorf("state disabled: disabledReason is required")
		}
		if e.NextRunAt != "" || e.CircuitOpenUntil != "" {
			return fmt.Errorf("state disabled: nextRunAt and circuitOpenUntil must be absent")
		}
	default:
		return fmt.Errorf("unknown execution state %q", e.State)
	}
	if e.CircuitOpenUntil != "" && e.NextRunAt == "" {
		return fmt.Errorf("circuitOpenUntil requires nextRunAt")
	}
	return nil
}
