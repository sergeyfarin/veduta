// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"sort"
	"strings"
)

// Severity classifies a Diagnostic. v1 only ever produces Error, but the type exists now so a
// future warning (a deprecated field, an ignored default) does not need a breaking signature
// change to Load's return type.
type Severity string

// Severity values.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is one problem found while loading a config, attributed to a real file position
// whenever the source of the problem is a specific node in the tree - never just a JSON Schema
// path, which means nothing to someone editing a file in an editor.
type Diagnostic struct {
	Severity Severity
	File     string
	Line     int // 1-based; 0 means no specific position is available
	Column   int
	Message  string
}

// String renders "file:line:col: message", matching compiler-style diagnostics tools already
// print to a terminal. Line 0 omits the position entirely rather than printing ":0:0".
func (d Diagnostic) String() string {
	if d.Line == 0 {
		if d.File == "" {
			return d.Message
		}
		return fmt.Sprintf("%s: %s", d.File, d.Message)
	}
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Line, d.Column, d.Message)
}

// Diagnostics is every problem Load found, in a deterministic order. An empty Diagnostics is
// success; config.Load never returns a non-nil *Snapshot alongside one that HasErrors.
type Diagnostics []Diagnostic

// HasErrors reports whether any Diagnostic is an error rather than a warning.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// String renders every diagnostic, one per line, sorted for stable output - the human-readable
// form `veduta --check-config` prints.
func (ds Diagnostics) String() string {
	sorted := make(Diagnostics, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Message < b.Message
	})
	lines := make([]string, len(sorted))
	for i, d := range sorted {
		lines[i] = d.String()
	}
	return strings.Join(lines, "\n")
}
