// SPDX-License-Identifier: AGPL-3.0-or-later

package config_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"veduta.dev/veduta/internal/config"
)

func decodeSecretRef(t *testing.T, yamlSrc string) config.SecretRef {
	t.Helper()
	var r config.SecretRef
	if err := yaml.Unmarshal([]byte(yamlSrc), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return r
}

func TestSecretRef_Literal(t *testing.T) {
	r := decodeSecretRef(t, `"plain value"`)
	if r.IsSecret() {
		t.Fatal("a plain string must not be treated as a secret reference")
	}
	if r.Literal != "plain value" {
		t.Fatalf("Literal = %q", r.Literal)
	}
}

func TestSecretRef_Reference(t *testing.T) {
	r := decodeSecretRef(t, `"${secret:VEDUTA_ADMIN_HASH}"`)
	if !r.IsSecret() {
		t.Fatal("want a secret reference")
	}
	if r.Name != "VEDUTA_ADMIN_HASH" {
		t.Fatalf("Name = %q", r.Name)
	}
	if r.Literal != "" {
		t.Fatalf("Literal should be empty for a secret reference, got %q", r.Literal)
	}
}

func TestSecretRef_EmbeddedTemplate(t *testing.T) {
	r := decodeSecretRef(t, `"prefix ${secret:X} suffix"`)
	if !r.IsSecret() || r.Template != "prefix ${secret:X} suffix" || len(r.Names) != 1 || r.Names[0] != "X" {
		t.Fatalf("template = %#v", r)
	}
}

func TestSecretRef_NonScalarIsError(t *testing.T) {
	var r config.SecretRef
	err := yaml.Unmarshal([]byte("a: 1\n"), &r)
	if err == nil {
		t.Fatal("want an error decoding a mapping into a SecretRef")
	}
}

// TestSecretRef_EmptyNameNotMatched: the pattern requires at least one character - ${secret:}
// is a literal, not a reference to a secret with an empty name.
func TestSecretRef_EmptyNameNotMatched(t *testing.T) {
	r := decodeSecretRef(t, `"${secret:}"`)
	if r.IsSecret() {
		t.Fatal("${secret:} must not parse as a reference")
	}
}
