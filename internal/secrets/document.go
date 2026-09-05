// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import "veduta.dev/veduta/internal/widgets"

// ContainsSecretInDocument reports whether any text an integration produced - the whole
// rendered surface of a Widget Document, not just one field - contains a known secret value
// verbatim. This is "a secret value appearing in a Widget Document is rejected by validation"
// (milestone C2's own acceptance test): an integration never receives a credential (the frozen
// import-boundary rule in docs/01-architecture.md section 3 keeps secrets out of
// internal/widgets entirely), so a match here means a connection's resolved secret leaked
// through as data - a misbehaving or compromised upstream echoing back a header, say - and the
// document must be rejected rather than stored or served.
//
// This is a real check today, not only a primitive waiting on a future caller: it walks every
// text-bearing field of all nine v1 block types explicitly (matching this project's preference
// for an auditable type-switch over reflection - see internal/widgets/blocks.go's own dispatch).
// What IS still pending is the caller: nothing runs a produced Document through this yet, because
// nothing produces one for real until Phase F's scheduler exists. Wire it there, after
// widgets.Validate succeeds and before a CardState is built from the result.
func (r *Registry) ContainsSecretInDocument(doc widgets.Document) bool {
	if r.ContainsSecret(doc.Title) || r.ContainsSecret(doc.Subtitle) {
		return true
	}
	if doc.Status != nil && r.ContainsSecret(doc.Status.Text) {
		return true
	}
	for _, b := range doc.Blocks {
		if r.blockLeaks(b) {
			return true
		}
	}
	return false
}

func (r *Registry) blockLeaks(b widgets.Block) bool {
	switch v := b.(type) {
	case widgets.BlockMetrics:
		return r.containsAny(v.Title) || r.metricItemsLeak(v.Items)
	case widgets.BlockKeyValue:
		return r.containsAny(v.Title) || r.metricItemsLeak(v.Items)
	case widgets.BlockProgress:
		if r.containsAny(v.Title) {
			return true
		}
		for _, it := range v.Items {
			if r.containsAny(it.Label) || r.scalarLeaks(it.Value) {
				return true
			}
		}
		return false
	case widgets.BlockStatus:
		if r.containsAny(v.Title) {
			return true
		}
		for _, it := range v.Items {
			if r.containsAny(it.Label, it.Text, it.Link) {
				return true
			}
		}
		return false
	case widgets.BlockList:
		if r.containsAny(v.Title, v.Empty) {
			return true
		}
		for _, it := range v.Items {
			if r.containsAny(it.Title, it.Subtitle, it.Link) || r.scalarLeaks(it.Value) {
				return true
			}
		}
		return false
	case widgets.BlockMedia:
		if r.containsAny(v.Title, v.Empty) {
			return true
		}
		for _, it := range v.Items {
			if r.containsAny(it.Title, it.Subtitle, it.Badge, it.Link) {
				return true
			}
		}
		return false
	case widgets.BlockText:
		return r.containsAny(v.Title, v.Content)
	case widgets.BlockTable:
		if r.containsAny(v.Title) {
			return true
		}
		for _, row := range v.Rows {
			for _, cell := range row {
				if r.scalarLeaks(cell) {
					return true
				}
			}
		}
		return false
	case widgets.BlockActions:
		if r.containsAny(v.Title) {
			return true
		}
		for _, a := range v.Actions {
			if r.containsAny(a.Label) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (r *Registry) metricItemsLeak(items []widgets.MetricItem) bool {
	for _, it := range items {
		if r.containsAny(it.Label, it.Unit, it.Link) || r.scalarLeaks(it.Value) {
			return true
		}
	}
	return false
}

func (r *Registry) containsAny(fields ...string) bool {
	for _, f := range fields {
		if r.ContainsSecret(f) {
			return true
		}
	}
	return false
}

func (r *Registry) scalarLeaks(v widgets.Scalar) bool {
	s, ok := v.(string)
	return ok && r.ContainsSecret(s)
}
