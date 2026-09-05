// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// suspiciousSecretRefs warns about a scalar that contains the ${secret: syntax without matching
// secretRefPattern as a whole value - almost always a real mistake, not an unusual literal. This
// was found for real, not hypothesised: examples/veduta.yaml's Jellyfin connection needs
// `Authorization: MediaBrowser Token="${secret:JELLYFIN_KEY}"` (docs/spikes/s2-upstream-reality-
// check.md's F6), which the current secretRef design cannot express - `${secret:NAME}` is
// recognised only as an ENTIRE scalar value, per secretRefPattern's own comment ("there is no
// defined way to redact half a string"). Written as it stands today, that value is a literal
// string containing the placeholder text verbatim, resolved to nothing, and would silently send
// the wrong Authorization header once a real HTTP client exists (Phase D) - with no error
// anywhere before now. This is a warning, not an error: a value containing that substring could
// legitimately be someone's actual literal string (unlikely, but not impossible), so Load does
// not fail the config over it - see docs/03-backlog.md for the real fix (a template-aware
// SecretRef), which this warning stands in for until milestone D1 needs to decide that shape.
func suspiciousSecretRefs(root *yaml.Node, m *merger) Diagnostics {
	var out Diagnostics
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "${secret:") && !secretRefPattern.MatchString(n.Value) {
			out = append(out, Diagnostic{
				Severity: SeverityWarning,
				File:     m.fileOf(n),
				Line:     n.Line,
				Column:   n.Column,
				Message: fmt.Sprintf("value %q contains ${secret:...} but is not recognised as a "+
					"secret reference (the syntax must be the ENTIRE value, not embedded in a "+
					"larger string) - it will be used as a literal string, placeholder text included",
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
