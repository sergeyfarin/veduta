// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import "sync"

// Audit records a denial - docs/01-architecture.md section 6: "counted per plugin, written to
// the audit log and surfaced in the UI. Repeated denials are what a malicious plugin looks like,
// so they are a product signal, not just a log line." memAudit is an in-memory counter; the real
// audit_log table (docs/01-architecture.md section 10) is a later milestone's job once storage
// exists - this package only needs denials to be counted and attributable, which does not
// require persistence.
type Audit interface {
	Denied(pluginID, method string, err error)
}

// DenialRecord is one counted denial, for callers that want more than a running total.
type DenialRecord struct {
	Method string
	Err    error
	Count  int
}

type memAudit struct {
	mu      sync.Mutex
	byCause map[string]*DenialRecord // pluginID+"\x00"+method+"\x00"+err.Error() -> record
	total   map[string]int           // pluginID -> total denials
}

// NewMemAudit builds an empty in-memory Audit.
func NewMemAudit() *memAudit {
	return &memAudit{byCause: make(map[string]*DenialRecord), total: make(map[string]int)}
}

// Denied implements Audit.
func (a *memAudit) Denied(pluginID, method string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := pluginID + "\x00" + method + "\x00" + err.Error()
	rec, ok := a.byCause[key]
	if !ok {
		rec = &DenialRecord{Method: method, Err: err}
		a.byCause[key] = rec
	}
	rec.Count++
	a.total[pluginID]++
}

// Total returns how many denials have been recorded for pluginID across every method and cause.
func (a *memAudit) Total(pluginID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.total[pluginID]
}
