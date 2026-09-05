// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets_test

import (
	"errors"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/secrets"
)

// stubProvider lets tests control Resolve's outcome directly, without touching the real
// environment or filesystem.
type stubProvider struct {
	values map[string]string
	errs   map[string]error
}

func (p stubProvider) Resolve(name string) (string, bool, error) {
	if err, ok := p.errs[name]; ok {
		return "", false, err
	}
	v, ok := p.values[name]
	return v, ok, nil
}

func TestResolver_TriesProvidersInOrder(t *testing.T) {
	first := stubProvider{values: map[string]string{"A": "from-first"}}
	second := stubProvider{values: map[string]string{"A": "from-second", "B": "from-second"}}
	r := secrets.Resolver{Providers: []secrets.Provider{first, second}}

	v, ok, err := r.Resolve("A")
	if err != nil || !ok || v.Reveal() != "from-first" {
		t.Fatalf("A: got %q, %v, %v - want the first provider to win", v.Reveal(), ok, err)
	}
	v, ok, err = r.Resolve("B")
	if err != nil || !ok || v.Reveal() != "from-second" {
		t.Fatalf("B: got %q, %v, %v - want the second provider to answer what the first can't", v.Reveal(), ok, err)
	}
	_, ok, err = r.Resolve("C")
	if err != nil || ok {
		t.Fatalf("C: got ok=%v err=%v, want ok=false, err=nil when no provider has it", ok, err)
	}
}

func TestResolver_ProviderErrorStopsTheSearch(t *testing.T) {
	boom := errors.New("permission denied")
	first := stubProvider{errs: map[string]error{"A": boom}}
	second := stubProvider{values: map[string]string{"A": "should-not-be-reached"}}
	r := secrets.Resolver{Providers: []secrets.Provider{first, second}}

	_, ok, err := r.Resolve("A")
	if ok {
		t.Fatal("want ok=false when a provider errors")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v (a real provider error must not fall through to the next provider)", err, boom)
	}
}

func TestResolveAll(t *testing.T) {
	refs := []config.SecretLocation{
		{Name: "GOOD", File: "veduta.yaml", Line: 3, Column: 5},
		{Name: "MISSING", File: "veduta.yaml", Line: 7, Column: 9},
		{Name: "MISSING", File: "conf.d/x.yaml", Line: 2, Column: 4},
	}
	r := secrets.Resolver{Providers: []secrets.Provider{
		stubProvider{values: map[string]string{"GOOD": "value"}},
	}}

	resolved, diags := secrets.ResolveAll(refs, r)

	if v, ok := resolved["GOOD"]; !ok || v.Reveal() != "value" {
		t.Fatalf("GOOD not resolved correctly: %v %v", v, ok)
	}
	if _, ok := resolved["MISSING"]; ok {
		t.Fatal("MISSING should not appear in resolved")
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want one per occurrence of MISSING (2): %s", len(diags), diags)
	}
	lines := map[int]bool{}
	for _, d := range diags {
		if d.Line == 0 || d.File == "" {
			t.Errorf("diagnostic missing position: %s", d)
		}
		lines[d.Line] = true
	}
	if !lines[7] || !lines[2] {
		t.Fatalf("want diagnostics at lines 7 and 2, got %v", diags)
	}
}

func TestResolveAll_ProviderErrorIsNamedInDiagnostic(t *testing.T) {
	refs := []config.SecretLocation{{Name: "BAD", File: "veduta.yaml", Line: 1, Column: 1}}
	r := secrets.Resolver{Providers: []secrets.Provider{
		stubProvider{errs: map[string]error{"BAD": errors.New("disk on fire")}},
	}}
	_, diags := secrets.ResolveAll(refs, r)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if got := diags[0].Message; !strings.Contains(got, "disk on fire") {
		t.Fatalf("diagnostic %q does not name the underlying error", got)
	}
}
