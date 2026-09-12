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
// It walks every text-bearing field of the document envelope and of all nine v1 block types
// explicitly (matching this project's preference for an auditable type-switch over reflection -
// see internal/widgets/blocks.go's own dispatch). "Text-bearing" means served, not rendered: the
// envelope's link, notices and string-valued signals reach the client in GET /api/v1/cards even
// where no component draws them today, so they are checked like anything else.
//
// Its caller is internal/scheduler: every produced document is checked after the run returns and
// before a CardState is built from it, and a persisted document is re-checked when Apply restores
// it. A match fails the run with scheduler.ErrSecretInDocument.
func (r *Registry) ContainsSecretInDocument(doc widgets.Document) bool {
	if r.containsAny(doc.Title, doc.Subtitle, doc.Link) {
		return true
	}
	if doc.Status != nil && r.ContainsSecret(doc.Status.Text) {
		return true
	}
	for _, n := range doc.Notices {
		if r.ContainsSecret(n.Message) {
			return true
		}
	}
	for _, sig := range doc.Signals {
		if r.scalarLeaks(sig.Value) {
			return true
		}
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
			if r.containsAny(it.Title, it.Subtitle, it.Link) || r.scalarLeaks(it.Value) || r.imageLeaks(it.Image) {
				return true
			}
		}
		return false
	case widgets.BlockMedia:
		if r.containsAny(v.Title, v.Empty) {
			return true
		}
		for _, it := range v.Items {
			if r.containsAny(it.Title, it.Subtitle, it.Badge, it.Link) || r.imageLeaks(&it.Image) {
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
		for _, c := range v.Columns {
			if r.containsAny(c.Label) {
				return true
			}
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

// imageLeaks checks an asset node's free text. Ref is broker-minted rather than integration-
// written, but Alt is the integration's own prose and is served (and rendered) verbatim.
func (r *Registry) imageLeaks(img *widgets.Image) bool {
	return img != nil && r.ContainsSecret(img.Alt)
}

func (r *Registry) scalarLeaks(v widgets.Scalar) bool {
	s, ok := v.(string)
	return ok && r.ContainsSecret(s)
}
