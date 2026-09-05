// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/canonical"
	"veduta.dev/veduta/internal/contracts"
)

// documents loads the real examples: three manifests, the lock and the configuration.
func documents(t *testing.T) contracts.Documents {
	t.Helper()
	root := repoRoot(t)
	manifestDirs, err := filepath.Glob(filepath.Join(root, "plugins", "*", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifests := map[string]any{}
	for _, p := range manifestDirs {
		m := loadYAML(t, p)
		id, _ := m["metadata"].(map[string]any)["id"].(string)
		if id == "" {
			t.Fatalf("%s: manifest has no metadata.id", p)
		}
		manifests[id] = m
	}
	limits := loadJSON(t, filepath.Join(root, "schemas", "plugin-manifest.v1.schema.json"))
	defs, _ := limits.(map[string]any)["$defs"].(map[string]any)
	limitsDef, _ := defs["limits"].(map[string]any)

	return contracts.Documents{
		Manifests: manifests,
		Lock:      loadYAML(t, filepath.Join(root, "examples", "veduta.lock.yaml")),
		Config:    loadYAML(t, filepath.Join(root, "examples", "veduta.yaml")),
		Limits:    limitsDef,
		Digest:    canonical.Digest,
	}
}

// TestRealExamplesAreClean is the no-skip rule: an integration referenced by the examples with no
// manifest is an error, not a pass, so this covers every reference in the shipped configuration.
func TestRealExamplesAreClean(t *testing.T) {
	d := documents(t)
	if len(d.Manifests) < 3 {
		t.Fatalf("expected the full example set, found %d manifests", len(d.Manifests))
	}
	for _, issue := range contracts.Issues(d) {
		t.Errorf("%s", issue)
	}
	root := repoRoot(t)
	cardState := loadJSON(t, filepath.Join(root, "testdata/widgets/jellyfin-recent.cardstate.json"))
	for _, issue := range contracts.CardStateIssues("cardstate fixture", cardState.(map[string]any)) {
		t.Errorf("%s", issue)
	}
}

// TestSemanticFixtures runs every checked-in fixture through the SAME code that validates the
// real examples. That is the point: deleting a check makes its fixture fail, rather than
// silently reducing coverage.
func TestSemanticFixtures(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "testdata", "semantic-cases", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 20 {
		t.Fatalf("only %d semantic fixtures found; they are the guard on the guards", len(files))
	}
	base := documents(t)

	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		t.Run(name, func(t *testing.T) {
			raw := loadYAML(t, file)
			expect, _ := raw["expect"].(string)
			if expect == "" {
				t.Fatal("fixture has no expect field")
			}

			d := base
			d.Manifests = cloneMap(base.Manifests)
			for id, patch := range asMapMap(raw["manifests"]) {
				d.Manifests[id] = deepMerge(d.Manifests[id], patch)
			}
			d.Lock = deepMerge(base.Lock, raw["lock"]).(map[string]any)
			d.Config = deepMerge(base.Config, raw["config"]).(map[string]any)

			found := contracts.Issues(d)
			for _, cs := range asSliceMap(raw["cardstates"]) {
				found = append(found, contracts.CardStateIssues("cardstate", cs)...)
			}

			if expect == "__NONE__" {
				if len(found) > 0 {
					t.Errorf("positive fixture must produce no issues, got %v", found)
				}
				return
			}
			for _, issue := range found {
				if strings.Contains(issue, expect) {
					return
				}
			}
			if len(found) == 0 {
				t.Fatalf("expected an issue containing %q, got NO ISSUES - the check it guards may "+
					"have been deleted", expect)
			}
			t.Fatalf("expected an issue containing %q, got %v", expect, found)
		})
	}
}

// deepMerge applies a fixture's sparse overlay. A nil value deletes a key, a list replaces
// wholesale, and __replace__: true makes a map replace instead of merge - which a fixture needs
// when the defect it describes is an ABSENCE, such as a partial effectiveLimits map.
func deepMerge(base, patch any) any {
	if patch == nil {
		return base
	}
	pm, patchIsMap := patch.(map[string]any)
	bm, baseIsMap := base.(map[string]any)
	if !patchIsMap || !baseIsMap {
		return patch
	}
	if r, ok := pm["__replace__"].(bool); ok && r {
		out := map[string]any{}
		for k, v := range pm {
			if k != "__replace__" {
				out[k] = v
			}
		}
		return out
	}
	out := make(map[string]any, len(bm))
	for k, v := range bm {
		out[k] = v
	}
	for k, v := range pm {
		if v == nil { // an explicit null in a fixture deletes the key
			delete(out, k)
			continue
		}
		out[k] = deepMerge(out[k], v)
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func asMapMap(v any) map[string]map[string]any {
	out := map[string]map[string]any{}
	m, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, e := range m {
		if em, ok := e.(map[string]any); ok {
			out[k] = em
		}
	}
	return out
}

func asSliceMap(v any) []map[string]any {
	var out []map[string]any
	s, ok := v.([]any)
	if !ok {
		return out
	}
	for _, e := range s {
		if em, ok := e.(map[string]any); ok {
			out = append(out, em)
		}
	}
	return out
}
