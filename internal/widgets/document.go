// SPDX-License-Identifier: AGPL-3.0-or-later

// Package widgets implements the Widget Document: the ONLY artefact an integration may produce.
// It carries presentation (blocks) and machine-readable semantics (signals) - nothing else.
// Freshness, staleness, errors and provenance are core-owned and live in package state
// (internal/state), never here: an integration must not be able to assert its own freshness or
// hide its own failure. See schemas/widget-document.v1.schema.json and
// docs/01-architecture.md section 4.
package widgets

// Level is a semantic status/value level. The UI never conveys meaning through colour alone -
// see web/src/lib/Status.svelte - so this is a word, not a colour.
type Level string

// Level values.
const (
	LevelOK      Level = "ok"
	LevelWarn    Level = "warn"
	LevelError   Level = "error"
	LevelInfo    Level = "info"
	LevelMuted   Level = "muted"
	LevelUnknown Level = "unknown"
)

// Format is a semantic formatting hint. Units, locale and rounding belong to the renderer, so an
// integration says "bytes", never "MiB" - see docs/01-architecture.md section 4.
type Format string

// Format values.
const (
	FormatText         Format = "text"
	FormatNumber       Format = "number"
	FormatBytes        Format = "bytes"
	FormatBytesRate    Format = "bytes-rate"
	FormatPercent      Format = "percent"
	FormatDuration     Format = "duration"
	FormatRelativeTime Format = "relative-time"
	FormatTemperature  Format = "temperature"
	FormatCurrency     Format = "currency"
	FormatCount        Format = "count"
)

// Emphasis is a block-level visual weight hint.
type Emphasis string

// Emphasis values.
const (
	EmphasisNormal Emphasis = "normal"
	EmphasisStrong Emphasis = "strong"
	EmphasisSubtle Emphasis = "subtle"
)

// SignalUnit is the unit a signal's value is measured in.
type SignalUnit string

// SignalUnit values.
const (
	UnitCount          SignalUnit = "count"
	UnitBytes          SignalUnit = "bytes"
	UnitBytesPerSecond SignalUnit = "bytes_per_second"
	UnitPercent        SignalUnit = "percent"
	UnitSeconds        SignalUnit = "seconds"
	UnitCelsius        SignalUnit = "celsius"
	UnitRatio          SignalUnit = "ratio"
	UnitBoolean        SignalUnit = "boolean"
	UnitState          SignalUnit = "state"
	UnitNone           SignalUnit = "none"
)

// Scalar is any JSON scalar: string, float64, bool, or nil. encoding/json already decodes and
// encodes these correctly through `any`; this alias exists only to name the concept at call
// sites (MetricItem.Value, table cells, ...).
type Scalar = any

// Document is the complete, integration-owned Widget Document. schemaVersion is implicit here
// (this type IS v1); Validate rejects any other value before a Document is ever constructed.
type Document struct {
	Title    string            `json:"title,omitempty"`
	Subtitle string            `json:"subtitle,omitempty"`
	Link     string            `json:"link,omitempty"`
	Status   *Status           `json:"status,omitempty"`
	Blocks   []Block           `json:"blocks"`
	Signals  map[string]Signal `json:"signals,omitempty"`
	Notices  []Notice          `json:"notices,omitempty"`
	Hints    *Hints            `json:"hints,omitempty"`
}

// Status is a card's overall health, shown in the card head.
type Status struct {
	Level Level  `json:"level"`
	Text  string `json:"text,omitempty"`
	// Since is RFC3339. Kept as a string rather than time.Time: the schema's pattern is a
	// structural pre-filter, and Validate parses it for real with time.Parse - see the note on
	// timestamps in validate.go. A plain string also round-trips identically to and from the
	// TypeScript side, which has no separate date type.
	Since string `json:"since,omitempty"`
}

// Signal is one machine-readable value. Signals are the ONLY substrate for rules, history and
// alerts - blocks are never queried (docs/01-architecture.md section 4, decision D15).
type Signal struct {
	Value Scalar     `json:"value"`
	Unit  SignalUnit `json:"unit,omitempty"`
	Level Level      `json:"level,omitempty"`
}

// Notice is an integration-reported soft problem - a sub-request failed, data is partial. This
// is distinct from a failed invocation, which the core records in execution.error.
type Notice struct {
	Level   string `json:"level"` // "info" | "warn"
	Message string `json:"message"`
}

// Hints are advisory only; the core clamps them to the card's configured schedule and its own
// maxima. An integration cannot force its own refresh cadence.
type Hints struct {
	TTLSeconds int `json:"ttlSeconds,omitempty"`
}

// Image is a reference to a broker-minted, signed asset token. There is deliberately no URL
// field: an integration cannot construct a ref, only request one via the capability broker
// (docs/01-architecture.md section 7).
type Image struct {
	Ref      string `json:"ref"`
	Alt      string `json:"alt,omitempty"`
	Aspect   string `json:"aspect,omitempty"`
	Blurhash string `json:"blurhash,omitempty"`
}

// MetricItem backs the metrics and key-value blocks.
type MetricItem struct {
	Label  string `json:"label"`
	Value  Scalar `json:"value"`
	Format Format `json:"format,omitempty"`
	Unit   string `json:"unit,omitempty"`
	Level  Level  `json:"level,omitempty"`
	Icon   string `json:"icon,omitempty"`
	Link   string `json:"link,omitempty"`
}

// ProgressItem backs the progress block.
type ProgressItem struct {
	Label    string  `json:"label"`
	Progress float64 `json:"progress"`
	Value    Scalar  `json:"value,omitempty"`
	Format   Format  `json:"format,omitempty"`
	Level    Level   `json:"level,omitempty"`
}

// StatusItem backs the status block (a list of named statuses, distinct from Document.Status).
type StatusItem struct {
	Label string `json:"label"`
	Level Level  `json:"level"`
	Text  string `json:"text,omitempty"`
	Since string `json:"since,omitempty"`
	Link  string `json:"link,omitempty"`
}

// ListItem backs the list block. An empty Items slice on BlockList is legal and renders Empty -
// "no active streams" is a state, not a bug.
type ListItem struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Value     Scalar `json:"value,omitempty"`
	Format    Format `json:"format,omitempty"`
	Level     Level  `json:"level,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Image     *Image `json:"image,omitempty"`
	Link      string `json:"link,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// MediaItem backs the image, image-grid and poster-grid blocks.
type MediaItem struct {
	ID        string `json:"id,omitempty"`
	Image     Image  `json:"image"`
	Title     string `json:"title,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`
	Badge     string `json:"badge,omitempty"`
	Level     Level  `json:"level,omitempty"`
	Link      string `json:"link,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// TableColumn describes one column of a table block.
type TableColumn struct {
	Key    string `json:"key"`
	Label  string `json:"label,omitempty"`
	Format Format `json:"format,omitempty"`
	Align  string `json:"align,omitempty"` //nolint:misspell // "center" is the schema's literal enum value (start|center|end), not English prose
}

// Action references an action id declared in configuration. A Widget Document can never express
// a command - only which already-declared action a button should trigger.
type Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Icon    string `json:"icon,omitempty"`
	Confirm bool   `json:"confirm,omitempty"`
	Danger  bool   `json:"danger,omitempty"`
}
