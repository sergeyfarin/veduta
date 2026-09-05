// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/secrets"
)

const sample = "supersecretvalue123"

func TestValue_NeverPrintsRaw(t *testing.T) {
	v := secrets.New(sample)

	cases := map[string]string{
		"%v":  fmt.Sprintf("%v", v),
		"%+v": fmt.Sprintf("%+v", v),
		"%#v": fmt.Sprintf("%#v", v),
		"%s":  v.String(),
	}
	for verb, got := range cases {
		if got == sample || containsRaw(got) {
			t.Errorf("%s leaked the raw value: %q", verb, got)
		}
	}
}

// TestValue_NeverPrintsRaw_Nested proves redaction survives being embedded in another struct -
// exactly the shape internal/config.ConnectionAuth.Value / SecretRef-typed fields take.
func TestValue_NeverPrintsRaw_Nested(t *testing.T) {
	type wrapper struct {
		Name string
		Val  secrets.Value
	}
	w := wrapper{Name: "x", Val: secrets.New(sample)}

	for verb, got := range map[string]string{
		"%v":  fmt.Sprintf("%v", w),
		"%+v": fmt.Sprintf("%+v", w),
		"%#v": fmt.Sprintf("%#v", w),
	} {
		if containsRaw(got) {
			t.Errorf("%s leaked the raw value when nested: %q", verb, got)
		}
	}
}

func TestValue_JSON(t *testing.T) {
	type wrapper struct {
		Val secrets.Value `json:"val"`
	}
	b, err := json.Marshal(wrapper{Val: secrets.New(sample)})
	if err != nil {
		t.Fatal(err)
	}
	if containsRaw(string(b)) {
		t.Fatalf("JSON leaked the raw value: %s", b)
	}
	if string(b) != `{"val":"***"}` {
		t.Fatalf("got %s", b)
	}
}

func TestValue_Reveal(t *testing.T) {
	v := secrets.New(sample)
	if v.Reveal() != sample {
		t.Fatalf("Reveal() = %q, want the real value", v.Reveal())
	}
}

func TestValue_IsZero(t *testing.T) {
	if !secrets.New("").IsZero() {
		t.Error("empty value should be IsZero")
	}
	if secrets.New(sample).IsZero() {
		t.Error("non-empty value should not be IsZero")
	}
}

func containsRaw(s string) bool {
	return strings.Contains(s, sample)
}
