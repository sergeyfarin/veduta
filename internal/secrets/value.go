// SPDX-License-Identifier: AGPL-3.0-or-later

// Package secrets implements milestone C2: resolving ${secret:NAME} references
// (internal/config.SecretRef) to real values, without ever letting one print, log or serialise
// as anything but "***". See docs/01-architecture.md section 2.
//
// Reveal is the one way out of a Value, and it is not open to the codebase at large: only
// internal/connections (upstream auth injection), internal/notify (channel tokens) and
// internal/auth (the admin password hash) may call it - enforced by
// internal/contracts.TestSecretRevealBoundary, a source-scanning test, not by Go visibility
// (Reveal must be exported for those packages to use it at all).
package secrets

import "encoding/json"

const redacted = "***"

// Value wraps a resolved secret. Every representation other than Reveal redacts it - String,
// GoString and MarshalJSON all return the same "***", so %v, %+v, %#v and any JSON encoding are
// covered by construction, not by remembering to redact at each call site.
type Value struct {
	resolved string
}

// New wraps a resolved secret value. Every Value ever constructed is registered with the
// package-level scrub Registry (see scrub.go) - defence in depth starts the moment a secret
// exists in the process, not only when something later calls Reveal.
func New(resolved string) Value {
	defaultRegistry.Track(resolved)
	return Value{resolved: resolved}
}

// String never returns the real value.
func (v Value) String() string { return redacted }

// GoString covers %#v, which bypasses fmt.Stringer - confirmed directly: %v and %+v both already
// respect Stringer, but %#v does not, so without this method %#v would print the unexported
// field's actual content via Go-syntax struct formatting.
func (v Value) GoString() string { return redacted }

// MarshalJSON never returns the real value.
func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// IsZero reports whether this Value wraps the empty string - useful for "was a secret configured
// at all" checks without ever needing Reveal to find out.
func (v Value) IsZero() bool { return v.resolved == "" }

// Reveal returns the real secret value. See the package doc comment: only
// internal/connections, internal/notify and internal/auth may call this.
func (v Value) Reveal() string { return v.resolved }
