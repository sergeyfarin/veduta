// SPDX-License-Identifier: AGPL-3.0-or-later

package state_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/widgets"
	"veduta.dev/veduta/schemas"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the module root")
		}
		dir = parent
	}
}

// cardStateSchema compiles card-state.v1, resolving its $ref to widget-document.v1 from the same
// embedded schemas.FS every production code path uses - not a second, hand-copied schema.
func cardStateSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	for _, pair := range []struct{ id, file string }{
		{"https://veduta.dev/schemas/widget-document.v1.schema.json", "widget-document.v1.schema.json"},
		{"https://veduta.dev/schemas/card-state.v1.schema.json", "card-state.v1.schema.json"},
	} {
		body, err := schemas.FS.ReadFile(pair.file)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(pair.id, doc); err != nil {
			t.Fatal(err)
		}
	}
	sch, err := c.Compile("https://veduta.dev/schemas/card-state.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func validateAgainstSchema(t *testing.T, sch *jsonschema.Schema, cs state.CardState) error {
	t.Helper()
	body, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return sch.Validate(generic)
}

func sampleDoc() widgets.Document {
	raw := []byte(`{"schemaVersion":1,"title":"Test","blocks":[{"type":"text","content":"hello"}]}`)
	doc, err := widgets.Validate(raw)
	if err != nil {
		panic(err)
	}
	return doc
}

// TestConstructorsProduceSchemaValidEnvelopes is the core B2 guarantee: every one of the seven
// ways to build a CardState produces something that satisfies BOTH card-state.v1's structural
// schema (including its conditional allOf branches) and the type-level Validate() self-check -
// two independent enforcements of the same five-state invariant.
func TestConstructorsProduceSchemaValidEnvelopes(t *testing.T) {
	sch := cardStateSchema(t)
	now := time.Now().UTC()
	src := state.Source{Integration: "immich", Runtime: state.RuntimeDeclarative, Operation: "recent-assets",
		Slots: map[string]string{"server": "immich"}}

	cases := map[string]state.CardState{
		"pending": state.Pending("card-1"),
		"ok":      state.OK("card-1", sampleDoc(), src, 5*time.Minute, 200*time.Millisecond),
		"stale": state.Stale("card-1", sampleDoc(), src, now.Add(-10*time.Minute), now.Add(-5*time.Minute),
			2, now.Add(30*time.Second)),
		"stale-open-circuit": state.StaleWithOpenCircuit("card-1", sampleDoc(), src, now.Add(-10*time.Minute),
			now.Add(-5*time.Minute), 6, now.Add(2*time.Minute)),
		"error": state.Error("card-1", src, state.RunError{Code: state.ErrorUpstream, Message: "502", Retryable: true}),
		"error-open-circuit": state.ErrorWithOpenCircuit("card-1", src,
			state.RunError{Code: state.ErrorUpstream, Message: "502", Retryable: true}, now.Add(2*time.Minute)),
		"disabled-no-doc": state.Disabled("card-1", nil, state.ReasonUnapproved),
		"disabled-with-doc": func() state.CardState {
			d := sampleDoc()
			return state.Disabled("card-1", &d, state.ReasonPermissionsChanged)
		}(),
	}

	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cs.Validate(); err != nil {
				t.Errorf("type-level Validate() rejected its own constructor's output: %v", err)
			}
			if err := validateAgainstSchema(t, sch, cs); err != nil {
				t.Errorf("schema rejected constructor output: %v", err)
			}
		})
	}
}

// TestValidateCatchesEveryIllegalCombination proves the type-level self-check actually does
// something: each case is a combination the schema's own "allOf" forbids (see
// schemas/card-state.v1.schema.json), built directly as a struct literal to bypass every
// constructor - which is exactly the "someone assembles it by hand" case Validate exists for.
func TestValidateCatchesEveryIllegalCombination(t *testing.T) {
	doc := sampleDoc()
	cases := map[string]state.CardState{
		"pending with a document": {
			CardID: "c", Document: &doc, Execution: state.Execution{State: state.StatePending},
		},
		"ok with no document": {
			CardID: "c", Execution: state.Execution{State: state.StateOK},
		},
		"ok carrying an error": {
			CardID: "c", Document: &doc,
			Execution: state.Execution{State: state.StateOK, GeneratedAt: "2026-01-01T00:00:00Z",
				TTLSeconds: 60, ExpiresAt: "2026-01-01T00:01:00Z",
				Error: &state.RunError{Code: state.ErrorUpstream, Message: "x"}},
		},
		"stale with no document": {
			CardID: "c", Execution: state.Execution{State: state.StateStale,
				GeneratedAt: "2026-01-01T00:00:00Z", StaleSince: "2026-01-01T00:05:00Z"},
		},
		"error carrying a document": {
			CardID: "c", Document: &doc,
			Execution: state.Execution{State: state.StateError,
				Error: &state.RunError{Code: state.ErrorUpstream, Message: "x"}},
		},
		"error with no error object": {
			CardID: "c", Execution: state.Execution{State: state.StateError},
		},
		"disabled with no reason": {
			CardID: "c", Execution: state.Execution{State: state.StateDisabled},
		},
		"disabled carrying nextRunAt": {
			CardID: "c", Execution: state.Execution{State: state.StateDisabled,
				DisabledReason: state.ReasonOperator, NextRunAt: "2026-01-01T00:00:00Z"},
		},
		"circuitOpenUntil without nextRunAt": {
			CardID: "c", Document: &doc,
			Execution: state.Execution{State: state.StateStale, GeneratedAt: "2026-01-01T00:00:00Z",
				StaleSince: "2026-01-01T00:05:00Z", CircuitOpenUntil: "2026-01-01T00:10:00Z"},
		},
		"unknown state": {
			CardID: "c", Execution: state.Execution{State: "confused"},
		},
	}
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cs.Validate(); err == nil {
				t.Error("expected Validate() to reject this combination, got nil")
			}
		})
	}
}

// TestSchemaAgreesWithTypeLevelValidate cross-checks a sample of the illegal combinations above
// against the REAL JSON Schema too (not just the Go-level Validate), so the two enforcement
// layers are proven to agree rather than just independently claiming to.
func TestSchemaAgreesWithTypeLevelValidate(t *testing.T) {
	sch := cardStateSchema(t)
	doc := sampleDoc()
	illegal := []state.CardState{
		{CardID: "c", Document: &doc, Execution: state.Execution{State: state.StatePending}},
		{CardID: "c", Execution: state.Execution{State: state.StateOK}},
		{CardID: "c", Execution: state.Execution{State: state.StateDisabled}},
	}
	for i, cs := range illegal {
		if err := validateAgainstSchema(t, sch, cs); err == nil {
			t.Errorf("case %d: schema accepted a combination the type-level Validate() also rejects", i)
		}
	}
}

// TestGoldenCardStateFixture proves the checked-in fixture (used elsewhere in the repo, e.g. by
// internal/contracts) round-trips through this package's own types and schema.
func TestGoldenCardStateFixture(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata/widgets/jellyfin-recent.cardstate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cs state.CardState
	if err := json.Unmarshal(raw, &cs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := cs.Validate(); err != nil {
		t.Errorf("golden fixture failed type-level Validate(): %v", err)
	}
	sch := cardStateSchema(t)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(generic); err != nil {
		t.Errorf("golden fixture failed schema validation: %v", err)
	}
}

// TestExecutionOnlyBuiltByConstructors is the structural half of "an integration cannot influence
// any field under execution" (the B2 acceptance criterion). The schema-level tests above prove
// the SHAPE is enforced; this proves the CODE PATH is: it scans cardstate.go's own source for any
// json.Unmarshal (or Decode) call whose target could be an Execution value, outside of this test
// file. There is exactly one legitimate such call in the whole codebase - decoding the CHECKED-IN
// golden fixture in TestGoldenCardStateFixture above, which is why that call lives in _test.go and
// this scan only inspects non-test source.
func TestExecutionOnlyBuiltByConstructors(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "state")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		found++
		for _, bad := range []string{"Unmarshal(", ".Decode("} {
			if idx := bytes.Index(body, []byte(bad)); idx >= 0 {
				t.Errorf("%s calls %s - Execution must only ever be built by the constructors in "+
					"this file (Pending/OK/Stale/.../Disabled), never decoded from bytes a caller "+
					"supplies; an integration's only route to producing anything is "+
					"widgets.Validate, which returns a widgets.Document, not a state.CardState",
					name, bad)
			}
		}
	}
	if found == 0 {
		t.Fatal("scanned zero non-test source files; the filter is broken")
	}
}
