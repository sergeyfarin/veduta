// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import "errors"

// Typed denials - docs/01-architecture.md section 6: "Failures are typed ... counted per plugin,
// written to the audit log and surfaced in the UI. Repeated denials are what a malicious plugin
// looks like, so they are a product signal, not just a log line."
var (
	// ErrCapDenied: the capability itself (http, cache, assets, log, events) is not in Caps.
	ErrCapDenied = errors.New("capabilities: capability not granted")
	// ErrSlotDenied: the request's Slot is not in Grant.Slots.
	ErrSlotDenied = errors.New("capabilities: slot not granted")
	// ErrRouteDenied: the concrete request failed manifest, lock, or connection-policy matching.
	ErrRouteDenied = errors.New("capabilities: route not authorised")
	// ErrBudgetExceeded: a per-invocation budget (hostCalls, httpRequests, cacheEntries,
	// cacheBytesKB) is exhausted.
	ErrBudgetExceeded = errors.New("capabilities: budget exceeded")
)
