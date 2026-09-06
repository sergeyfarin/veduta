// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"encoding/json"
	"os"
	"testing"
)

// TestLimitBoundsMatchManifestSchema reads schemas/plugin-manifest.v1.schema.json directly and
// checks limitBounds against it field by field, so the hardcoded table this package uses to
// reconcile effective limits cannot silently drift from the schema that is the actual source of
// truth - a schema edit that changes a default or maximum without updating limitBounds would
// otherwise only surface as a wrong effectiveLimits value in some later, unrelated test failure.
func TestLimitBoundsMatchManifestSchema(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/plugin-manifest.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs struct {
			Limits struct {
				Properties map[string]struct {
					Default *float64 `json:"default"`
					Maximum *float64 `json:"maximum"`
				} `json:"properties"`
			} `json:"limits"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	props := doc.Defs.Limits.Properties
	if len(props) != len(limitBounds) {
		t.Fatalf("schema declares %d limit fields, limitBounds has %d", len(props), len(limitBounds))
	}
	for name, bound := range limitBounds {
		p, ok := props[name]
		if !ok {
			t.Errorf("limitBounds has %q, not in the schema", name)
			continue
		}
		if p.Default == nil || p.Maximum == nil {
			t.Errorf("%s: schema is missing default/maximum", name)
			continue
		}
		if int(*p.Default) != bound.def {
			t.Errorf("%s: default = %d, schema says %v", name, bound.def, *p.Default)
		}
		if int(*p.Maximum) != bound.max {
			t.Errorf("%s: max = %d, schema says %v", name, bound.max, *p.Maximum)
		}
	}
	for _, name := range limitFieldOrder {
		if _, ok := limitBounds[name]; !ok {
			t.Errorf("limitFieldOrder has %q, missing from limitBounds", name)
		}
	}
	if len(limitFieldOrder) != len(limitBounds) {
		t.Fatalf("limitFieldOrder has %d entries, limitBounds has %d", len(limitFieldOrder), len(limitBounds))
	}
}

func TestRequested_UsesDefaultWhenUnset(t *testing.T) {
	if got := requested("timeoutMs", Limits{}); got != 3000 {
		t.Fatalf("got %d, want the schema default 3000", got)
	}
}

func TestRequested_ClampsToCoreMaximum(t *testing.T) {
	over := 999999999
	got := requested("timeoutMs", Limits{TimeoutMs: &over})
	if got != limitBounds["timeoutMs"].max {
		t.Fatalf("got %d, want clamped to %d", got, limitBounds["timeoutMs"].max)
	}
}

func TestRequested_CacheEntriesExplicitZeroIsRespected(t *testing.T) {
	zero := 0
	got := requested("cacheEntries", Limits{CacheEntries: &zero})
	if got != 0 {
		t.Fatalf("got %d, want explicit 0 to be honoured (not treated as unset)", got)
	}
}

func TestLimitsField_UnknownNameIsUnset(t *testing.T) {
	if _, ok := (Limits{}).field("not-a-real-field"); ok {
		t.Fatal("an unknown field name must report unset, not a stale value")
	}
}

func TestEffectiveLimitsField_UnknownNameIsZero(t *testing.T) {
	if got := (EffectiveLimits{}).field("not-a-real-field"); got != 0 {
		t.Fatalf("got %d, want 0 for an unknown field name", got)
	}
}

func TestReconcileAtApproval_EveryFieldPopulated(t *testing.T) {
	eff := ReconcileAtApproval(Limits{}, Limits{})
	for _, name := range limitFieldOrder {
		if eff.field(name) != limitBounds[name].def {
			t.Errorf("%s: got %d, want default %d", name, eff.field(name), limitBounds[name].def)
		}
	}
}

func TestEffectiveLimit_ApprovedAbsentCollapsesToDefault(t *testing.T) {
	requested := 4000
	got := effectiveLimit("timeoutMs", Limits{TimeoutMs: &requested}, Limits{})
	if got != limitBounds["timeoutMs"].def {
		t.Fatalf("got %d, want the default %d - no approved override was given", got, limitBounds["timeoutMs"].def)
	}
}

func TestEffectiveLimit_ApprovedNarrowerThanManifestWins(t *testing.T) {
	requested, approved := 4000, 1000
	got := effectiveLimit("timeoutMs", Limits{TimeoutMs: &requested}, Limits{TimeoutMs: &approved})
	if got != 1000 {
		t.Fatalf("got %d, want 1000 (the narrower, approved side)", got)
	}
}

func TestLimits_IsZero(t *testing.T) {
	if !(Limits{}).isZero() {
		t.Fatal("a Limits with every field nil must be zero")
	}
	v := 1
	if (Limits{TimeoutMs: &v}).isZero() {
		t.Fatal("a Limits with any field set must not be zero")
	}
}
