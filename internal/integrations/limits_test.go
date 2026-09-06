// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations/manifestload"
	"veduta.dev/veduta/internal/widgets"
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

// TestManifestloadLimitsMatchesTheFieldSet guards the cross-type duplication this package,
// manifestload, and capabilities all deliberately carry (each package has its own Limits shape
// for its own decode/enforcement boundary, rather than one shared type - see capabilities.Limits'
// own doc comment on why). Found in the D2b/D3/D4/D5 review: that duplication is exactly what let
// outputKB and responseMB go unenforced for a time - a field present in the schema and in this
// package's own limitBounds table, but silently absent from (or ignored by) a consumer type,
// surfaces nowhere until a manual audit or a targeted regression test catches it. Rather than
// collapsing the types (each earns its shape from a real, previously-decided constraint -
// pointer-fielded here for "explicit zero vs unset", plain-int in manifestload for "already
// reconciled", a narrow broker-only subset in capabilities), this test is the cheap mitigation:
// every limitBounds key must have a same-named field in manifestload.Limits, so a schema field
// can never silently lack a reconciled-limits home.
func TestManifestloadLimitsMatchesTheFieldSet(t *testing.T) {
	mlType := reflect.TypeOf(manifestload.Limits{})
	mlFields := make(map[string]bool, mlType.NumField())
	for i := 0; i < mlType.NumField(); i++ {
		f := mlType.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" {
			t.Fatalf("manifestload.Limits field %s has no yaml tag", f.Name)
		}
		mlFields[tag] = true
	}
	for name := range limitBounds {
		if !mlFields[name] {
			t.Errorf("limitBounds has %q, missing from manifestload.Limits", name)
		}
	}
	for name := range mlFields {
		if _, ok := limitBounds[name]; !ok {
			t.Errorf("manifestload.Limits has %q, missing from limitBounds", name)
		}
	}
}

// TestCapabilitiesLimitsIsARealSubsetOfLimitBounds checks the other half of the same
// duplication: capabilities.Limits is a deliberate, narrower subset (only the fields the broker
// itself enforces - the declarative runtime's/WASM sandbox's own budgets live elsewhere), but
// every one of its field names must still be a real limitBounds key, or a typo/rename here would
// silently create a broker-enforced ceiling with no manifest-declarable counterpart.
func TestCapabilitiesLimitsIsARealSubsetOfLimitBounds(t *testing.T) {
	// Go field name -> limitBounds/schema key. Explicit rather than a lowercase-first-letter
	// heuristic: Go's own acronym capitalisation (HTTPRequests, not HttpRequests) breaks that
	// heuristic for exactly one of these six fields, and a heuristic that's wrong for one input
	// defeats the point of a drift-detecting test.
	toKey := map[string]string{
		"HTTPRequests":  "httpRequests",
		"ResponseMB":    "responseMB",
		"CacheEntries":  "cacheEntries",
		"CacheBytesKB":  "cacheBytesKB",
		"HostCalls":     "hostCalls",
		"RequestBodyKB": "requestBodyKB",
	}
	capType := reflect.TypeOf(capabilities.Limits{})
	if capType.NumField() != len(toKey) {
		t.Fatalf("capabilities.Limits has %d fields, this test's name map has %d - update toKey",
			capType.NumField(), len(toKey))
	}
	for i := 0; i < capType.NumField(); i++ {
		fieldName := capType.Field(i).Name
		key, known := toKey[fieldName]
		if !known {
			t.Errorf("capabilities.Limits field %s is not in this test's name map", fieldName)
			continue
		}
		if _, ok := limitBounds[key]; !ok {
			t.Errorf("capabilities.Limits field %s (%s) is missing from limitBounds", fieldName, key)
		}
	}
}

// TestOutputKBHardCapMatchesCoreMaximum cross-checks widgets.MaxDocumentBytesHardCap - the
// absolute ceiling ValidateWithLimit will accept regardless of what a caller passes - against
// this package's own core maximum for outputKB. The two live in different packages (widgets
// cannot import this one, which imports widgets; see MaxDocumentBytesHardCap's own doc comment)
// specifically so ValidateWithLimit's defensive clamp does not depend on a caller having already
// reconciled EffectiveLimits correctly - but that independence only holds if the two numbers
// actually agree, which is what this test guards.
func TestOutputKBHardCapMatchesCoreMaximum(t *testing.T) {
	want := limitBounds["outputKB"].max << 10
	if widgets.MaxDocumentBytesHardCap != want {
		t.Fatalf("widgets.MaxDocumentBytesHardCap = %d, want %d (limitBounds[outputKB].max<<10)",
			widgets.MaxDocumentBytesHardCap, want)
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
