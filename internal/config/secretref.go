// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// secretRefPattern recognises the compact whole-value form; secretRefOccurrencePattern also
// supports composed credentials such as Jellyfin's `MediaBrowser Token="${secret:KEY}"`.
var secretRefPattern = regexp.MustCompile(`^\$\{secret:([A-Za-z0-9_]+)\}$`)
var secretRefOccurrencePattern = regexp.MustCompile(`\$\{secret:([A-Za-z0-9_]+)\}`)

// SecretRef is a literal, a whole-value ${secret:NAME}, or a template containing references.
// Resolving names to actual values is milestone C2's job
// (internal/secrets) - this package only recognises the syntax and carries it through
// unresolved, per docs/01-architecture.md section 2 ("values NOT inlined").
type SecretRef struct {
	Literal  string   // set when the scalar was not a ${secret:...} reference
	Name     string   // set when it was; Literal is empty in that case
	Template string   // set when one or more references are embedded in a larger value
	Names    []string // referenced names in occurrence order for Template
}

// IsSecret reports whether this reference names a secret rather than carrying a literal value.
func (r SecretRef) IsSecret() bool { return r.Name != "" || len(r.Names) > 0 }

// UnmarshalYAML recognises whole or embedded references in any SecretRef scalar; everything else
// is carried through as a literal. A non-scalar node (a mapping or sequence where a string
// was expected) is a decode error, same as decoding a bare string field would give.
func (r *SecretRef) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: column %d: expected a string, got %s", value.Line, value.Column, kindName(value.Kind))
	}
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	if m := secretRefPattern.FindStringSubmatch(s); m != nil {
		*r = SecretRef{Name: m[1]}
		return nil
	}
	matches := secretRefOccurrencePattern.FindAllStringSubmatch(s, -1)
	if len(matches) > 0 {
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match[1])
		}
		*r = SecretRef{Template: s, Names: names}
		return nil
	}
	*r = SecretRef{Literal: s}
	return nil
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "a document"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a sequence"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "a value"
	}
}
