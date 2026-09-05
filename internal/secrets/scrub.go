// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// minScrubLength excludes short values from scrubbing. docs/01-architecture.md section 2 is
// explicit about why: secrets shorter than 8 characters are excluded entirely to avoid false
// positives - a short value is also not meaningfully protected by text-scrubbing regardless.
const minScrubLength = 8

// Registry is the set of secret values known to this process, for the log scrubber.
// "Scrubbing is defence in depth, never a boundary" (docs/01, same section): base64,
// URL-encoding or chunking defeats it. The boundary is that plugins never receive secrets at
// all; this only limits the blast radius of a core-side mistake - an accidental Sprintf of a
// resolved value into a log line, not something wrapped in Value at all by the time it happens.
type Registry struct {
	mu     sync.RWMutex
	values map[string]struct{}
}

// NewRegistry creates an empty Registry. Exported so a test can use its own instance rather than
// sharing process-global state with every other test in the package.
func NewRegistry() *Registry { return &Registry{values: make(map[string]struct{})} }

// Track records v for scrubbing. Values under minScrubLength are silently ignored - see the
// constant's own comment. Exported so a caller with a value that is sensitive but never passes
// through a Value (a webhook URL fragment, say) can still register it for defence in depth.
func (r *Registry) Track(v string) {
	if len(v) < minScrubLength {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[v] = struct{}{}
}

// Scrub replaces every known secret value appearing in s with "***".
func (r *Registry) Scrub(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for v := range r.values {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, redacted)
		}
	}
	return s
}

// ContainsSecret reports whether s contains any known secret value verbatim - the primitive
// behind "a secret value appearing in a Widget Document is rejected by validation". It lives
// here, not in internal/widgets: widgets must never import secrets (integrations, and the
// widgets they emit into, never see credentials at all - docs/01-architecture.md's frozen
// import-boundary rule), so the caller that checks a produced Document against this has to be
// something that already sees both - the scheduler, which does not exist until Phase F. This is
// the ready primitive for that caller, not the wiring itself.
func (r *Registry) ContainsSecret(s string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for v := range r.values {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}

// defaultRegistry is the process-wide scrub set: every Value ever constructed via New registers
// itself here, regardless of whether Reveal is ever called on it, so scrubbing is active from
// the moment a secret is resolved.
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the process-wide Registry every Value constructed via New registers
// with.
func DefaultRegistry() *Registry { return defaultRegistry }

// scrubbingHandler wraps an slog.Handler, scrubbing every string value (message, string
// attributes, and attributes nested in groups) against a Registry before the wrapped handler
// ever sees them.
type scrubbingHandler struct {
	next slog.Handler
	reg  *Registry
}

// NewHandler wraps next so every log record passes through reg's scrubber first. Wire this in
// wherever the process builds its slog.Handler (cmd/veduta/main.go's serve), so scrubbing applies
// to every log line by construction, not by remembering to scrub at each call site.
func NewHandler(next slog.Handler, reg *Registry) slog.Handler {
	return &scrubbingHandler{next: next, reg: reg}
}

// Enabled implements slog.Handler by delegating to next.
func (h *scrubbingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle implements slog.Handler, scrubbing the message and every attribute before next sees them.
func (h *scrubbingHandler) Handle(ctx context.Context, r slog.Record) error {
	scrubbed := slog.NewRecord(r.Time, r.Level, h.reg.Scrub(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		scrubbed.AddAttrs(h.scrubAttr(a))
		return true
	})
	return h.next.Handle(ctx, scrubbed)
}

func (h *scrubbingHandler) scrubAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.reg.Scrub(a.Value.String()))
	case slog.KindGroup:
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, ga := range group {
			out[i] = h.scrubAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	default:
		return a
	}
}

// WithAttrs implements slog.Handler, scrubbing attrs before they reach next.
func (h *scrubbingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbed[i] = h.scrubAttr(a)
	}
	return &scrubbingHandler{next: h.next.WithAttrs(scrubbed), reg: h.reg}
}

// WithGroup implements slog.Handler by delegating to next.
func (h *scrubbingHandler) WithGroup(name string) slog.Handler {
	return &scrubbingHandler{next: h.next.WithGroup(name), reg: h.reg}
}
