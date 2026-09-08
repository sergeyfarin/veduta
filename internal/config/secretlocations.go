// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import "gopkg.in/yaml.v3"

// SecretLocation is one place in the merged config where a ${secret:NAME} reference was found.
// It exists for milestone C2 (internal/secrets): "a missing secret is a diagnostic naming the
// config location, not a panic" needs a real file:line:col, and SecretRef itself does not carry
// one - a decoded Go struct field has nowhere natural to keep it, and the same name can
// legitimately appear more than once. Locating occurrences by NAME (scanning the merged tree for
// the ${secret:...} syntax directly, independent of which schema field holds it) sidesteps both
// problems: every occurrence of a given name is recorded, so a caller resolving that name once
// can still report every place a failure affects, and no per-field bookkeeping is needed - a new
// secretRef-typed field anywhere in the schema is covered automatically, by content, not by a
// maintained list of paths.
type SecretLocation struct {
	Name   string
	File   string
	Line   int
	Column int
}

// secretLocations walks the merged tree (Content only, per yamlmerge.go's own reasoning about
// Alias-safety) collecting every occurrence of the ${secret:NAME} syntax.
func secretLocations(root *yaml.Node, m *merger) []SecretLocation {
	var out []SecretLocation
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.ScalarNode {
			for _, match := range secretRefOccurrencePattern.FindAllStringSubmatch(n.Value, -1) {
				out = append(out, SecretLocation{
					Name: match[1], File: m.fileOf(n), Line: n.Line, Column: n.Column,
				})
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
	return out
}
