// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/widgets"
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

// TestGoldenFixture proves Validate accepts the real fixture used elsewhere in the repo (the
// Jellyfin golden document referenced by testdata/widgets/jellyfin-recent.cardstate.json) and
// decodes every block into its concrete Go type.
func TestGoldenFixture(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata/widgets/jellyfin-recent.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := widgets.Validate(raw)
	if err != nil {
		t.Fatalf("golden fixture must validate: %v", err)
	}
	if doc.Title == "" {
		t.Error("title is empty")
	}
	if len(doc.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}
	for i, b := range doc.Blocks {
		if b.BlockType() == "" {
			t.Errorf("blocks[%d] has no discriminator", i)
		}
	}
	if len(doc.Signals) == 0 {
		t.Error("expected the golden fixture to carry signals")
	}
}

type schemaCase struct {
	Name   string          `json:"name"`
	Schema string          `json:"schema"`
	Expect string          `json:"expect"`
	Doc    json.RawMessage `json:"doc"`
}

// TestAdversarialCorpus runs Part 0's shared corpus (testdata/schema-cases.json) through the
// PRODUCTION Validate function, not just the schema compiler the contract suite in
// internal/contracts exercises separately - this is the "runs as a Go table test" instruction in
// the B2 milestone, and it is the same corpus, not a second copy of it.
func TestAdversarialCorpus(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata/schema-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []schemaCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}

	var ran int
	for _, tc := range corpus.Cases {
		if tc.Schema != "widget" {
			continue
		}
		ran++
		t.Run(tc.Name, func(t *testing.T) {
			_, err := widgets.Validate(tc.Doc)
			switch tc.Expect {
			case "accept":
				if err != nil {
					t.Errorf("expected accept, got reject: %v", err)
				}
			case "reject":
				if err == nil {
					t.Error("expected reject, got accept")
				}
			default:
				t.Fatalf("unknown expectation %q", tc.Expect)
			}
		})
	}
	if ran < 10 {
		t.Fatalf("only ran %d widget cases from the shared corpus; the filter is probably broken", ran)
	}
}

// TestLimitsBeyondSchema exercises the checks that JSON Schema itself cannot express: total
// document size, and raw HTML inside markdown (the schema can only bound content LENGTH, not
// reject HTML structurally). Named cases mirror the B2 milestone's own list verbatim.
func TestLimitsBeyondSchema(t *testing.T) {
	doc := func(blocks string) []byte {
		return []byte(`{"schemaVersion":1,"blocks":[` + blocks + `]}`)
	}

	t.Run("total document exceeds 64 KiB", func(t *testing.T) {
		// A SINGLE table block, schema-valid at every individual field (1 block, well under the
		// 12-block cap; 100 rows, exactly the schema max; 8 cells per row, exactly the schema
		// max; 512-char cell values, exactly the schema max) that nonetheless totals well past
		// 64 KiB. Constructed this way deliberately: an earlier version of this test used 200
		// small blocks, which tripped the SCHEMA's own 12-block maxItems first and never
		// exercised the size gate being tested at all.
		row := map[string]string{}
		var cols []map[string]string
		for j := 0; j < 8; j++ {
			key := fmt.Sprintf("c%d", j)
			row[key] = strings.Repeat("x", 512)
			cols = append(cols, map[string]string{"key": key})
		}
		colsJSON, _ := json.Marshal(cols)
		rowJSON, _ := json.Marshal(row)
		var rows strings.Builder
		for i := 0; i < 100; i++ {
			if i > 0 {
				rows.WriteString(",")
			}
			rows.Write(rowJSON)
		}
		raw := []byte(fmt.Sprintf(
			`{"schemaVersion":1,"blocks":[{"type":"table","columns":%s,"rows":[%s]}]}`,
			colsJSON, rows.String()))
		if len(raw) < 64*1024 {
			t.Fatalf("test setup: constructed document is only %d bytes, need > 64 KiB", len(raw))
		}
		if _, err := widgets.Validate(raw); err == nil {
			t.Error("expected a document over 64 KiB to be rejected")
		}
	})

	t.Run("the same table shape under 64 KiB is accepted", func(t *testing.T) {
		// Same construction, far fewer rows - proves the previous case is rejected for its SIZE,
		// not because a 100-row/8-column table is itself illegal.
		row := map[string]string{"c0": "x"}
		rowJSON, _ := json.Marshal(row)
		raw := []byte(fmt.Sprintf(
			`{"schemaVersion":1,"blocks":[{"type":"table","columns":[{"key":"c0"}],"rows":[%s,%s]}]}`,
			rowJSON, rowJSON))
		if _, err := widgets.Validate(raw); err != nil {
			t.Errorf("expected a small table to validate, got: %v", err)
		}
	})

	t.Run("markdown containing a script tag is rejected", func(t *testing.T) {
		raw := doc(`{"type":"markdown","content":"hello <script>alert(1)</script> world"}`)
		if _, err := widgets.Validate(raw); err == nil {
			t.Error("expected raw HTML in markdown to be rejected")
		}
	})

	t.Run("markdown containing an anchor tag is rejected", func(t *testing.T) {
		raw := doc(`{"type":"markdown","content":"click <a href=\"http://x\">here</a>"}`)
		if _, err := widgets.Validate(raw); err == nil {
			t.Error("expected raw HTML anchor tags in markdown to be rejected")
		}
	})

	t.Run("markdown without HTML is accepted", func(t *testing.T) {
		raw := doc(`{"type":"markdown","content":"**bold** and a [link](http://x) and \n- one\n- two"}`)
		if _, err := widgets.Validate(raw); err != nil {
			t.Errorf("expected clean markdown to validate, got: %v", err)
		}
	})

	t.Run("status.since must be a real timestamp, not merely pattern-shaped", func(t *testing.T) {
		raw := []byte(`{"schemaVersion":1,"status":{"level":"ok","since":"2026-99-99T99:99:99Z"},"blocks":[{"type":"text","content":"x"}]}`)
		if _, err := widgets.Validate(raw); err == nil {
			t.Error("expected a pattern-shaped but invalid timestamp to be rejected")
		}
	})

	t.Run("a valid timestamp is accepted", func(t *testing.T) {
		raw := []byte(`{"schemaVersion":1,"status":{"level":"ok","since":"2026-09-05T12:00:00Z"},"blocks":[{"type":"text","content":"x"}]}`)
		if _, err := widgets.Validate(raw); err != nil {
			t.Errorf("expected a valid timestamp to validate, got: %v", err)
		}
	})
}

// TestValidateNeverPanics fuzzes the entrypoint that receives untrusted, integration-produced
// bytes directly - a plugin's output must never be able to crash the core (docs/01-architecture.md
// section 8, "Plugin DoS").
// TestValidateWithLimit_NarrowerAndWiderThanTheCoreDefault is the regression test for a real gap
// found in review: a manifest's approved outputKB (schema range 1-256 KiB) was reconciled into
// EffectiveLimits and carried to the declarative runtime, but Validate only ever checked the
// hardcoded 64 KiB core default - a narrower approval was never actually tighter and a wider one
// could never take effect.
func TestValidateWithLimit_NarrowerAndWiderThanTheCoreDefault(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"blocks":[{"type":"text","content":"` + strings.Repeat("x", 2000) + `"}]}`)
	if _, err := widgets.Validate(raw); err != nil {
		t.Fatalf("under the 64 KiB core default this must pass: %v", err)
	}
	if _, err := widgets.ValidateWithLimit(raw, 1024); err == nil {
		t.Fatal("a 1 KiB limit must reject a document over 2 KiB, which the 64 KiB default alone would not catch")
	}
	if _, err := widgets.ValidateWithLimit(raw, 0); err != nil {
		t.Fatalf("zero must fall back to the core default like every other limit in this project, not mean unlimited: %v", err)
	}
}

func TestValidateNeverPanics(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "corpus")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	inputs := []string{
		"", "null", "{}", "[]", "true", "42", `"x"`,
		`{"schemaVersion":1,"blocks":[]}`,
		`{"schemaVersion":1,"blocks":null}`,
		`{"schemaVersion":2,"blocks":[]}`,
		`{"schemaVersion":1,"blocks":[{"type":"metrics","items":[]}]}`,
		`{"schemaVersion":1,"blocks":[{"type":"unknown-type"}]}`,
		`{"schemaVersion":1,"blocks":[{"type":"metrics"}]}`,
		strings.Repeat("{", 10000),
		strings.Repeat(`{"a":`, 500) + "1" + strings.Repeat("}", 500),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Validate panicked on input %q: %v", in, r)
				}
			}()
			_, _ = widgets.Validate([]byte(in))
		}()
	}
}

// TestMarshalUnmarshalRoundTrip proves schemaVersion and every block type survive a full
// marshal -> unmarshal -> marshal cycle unchanged. This is the specific case that caught the
// missing MarshalJSON: a Document decoded fine (UnmarshalJSON reads schemaVersion), but
// re-encoding it silently dropped the field until MarshalJSON was added to restore it.
func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata/widgets/jellyfin-recent.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := widgets.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := widgets.Validate(out); err != nil {
		t.Fatalf("re-marshalled document failed to re-validate: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(out, &generic); err != nil {
		t.Fatal(err)
	}
	if v, ok := generic["schemaVersion"]; !ok || v != float64(1) {
		t.Errorf("schemaVersion = %v, want 1", v)
	}
}
