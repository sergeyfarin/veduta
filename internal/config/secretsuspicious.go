// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// suspiciousSecretRefs warns about malformed placeholder-looking syntax. Valid references may
// now be embedded in a larger value and are composed into one opaque secrets.Value.
func suspiciousSecretRefs(root *yaml.Node, m *merger) Diagnostics {
	var out Diagnostics
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "${secret:") && len(secretRefOccurrencePattern.FindAllStringSubmatch(n.Value, -1)) == 0 {
			out = append(out, Diagnostic{
				Severity: SeverityWarning,
				File:     m.fileOf(n),
				Line:     n.Line,
				Column:   n.Column,
				Message: fmt.Sprintf("value %q contains malformed ${secret:...} syntax and will be used as a literal string",
					n.Value),
			})
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
	return out
}
