// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// semanticCtx carries what validateSemantics needs beyond the decoded Config: the merged Node
// tree and its provenance, so a semantic diagnostic gets the same real file:line:col a schema
// diagnostic does, by looking up the same JSON-Schema-style path the decoded value came from.
type semanticCtx struct {
	root *yaml.Node
	m    *merger
}

func (c semanticCtx) at(path ...string) Diagnostic {
	n := nodeAtPath(c.root, path)
	return Diagnostic{Severity: SeverityError, File: c.m.fileOf(n), Line: n.Line, Column: n.Column}
}

func (c semanticCtx) errf(path []string, format string, a ...any) Diagnostic {
	d := c.at(path...)
	d.Message = fmt.Sprintf(format, a...)
	return d
}

// validateSemantics checks everything true structural JSON Schema validation cannot express:
// duplicate ids across array items (a mapping's keys can't collide - the schema and
// duplicateKeys in yamlmerge.go already cover connections and notification channels), and
// dangling references between parts of the document a schema $ref cannot see across.
//
// Two categories are deliberately NOT checked here, though the schema's own field descriptions
// mention them: whether a rule's signal name is declared by the integration operation it names,
// and whether a card's required slots are all bound. Both need an integration manifest, which
// does not exist until Phase D loads one - checking them here would mean either silently
// skipping unloadable manifests or making config.Load depend on the connections/broker package,
// which does not yet exist and should not be an upward dependency of configuration loading.
func validateSemantics(cfg *Config, root *yaml.Node, m *merger) Diagnostics {
	c := semanticCtx{root: root, m: m}
	var out Diagnostics

	integrationIDs := make(map[string]bool, len(cfg.Integrations))
	for i, in := range cfg.Integrations {
		p := []string{"integrations", strconv.Itoa(i), "id"}
		if integrationIDs[in.ID] {
			out = append(out, c.errf(p, "duplicate integration id %q", in.ID))
		}
		integrationIDs[in.ID] = true
	}

	cardIDs := make(map[string]bool)
	for si, sec := range cfg.Sections {
		for ci, card := range sec.Cards {
			cardPath := []string{"sections", strconv.Itoa(si), "cards", strconv.Itoa(ci)}
			idPath := append(append([]string{}, cardPath...), "id")
			if cardIDs[card.ID] {
				out = append(out, c.errf(idPath, "duplicate card id %q", card.ID))
			}
			cardIDs[card.ID] = true

			if card.Integration != "" && !integrationIDs[card.Integration] {
				out = append(out, c.errf(append(append([]string{}, cardPath...), "integration"),
					"card %q: integration %q is not declared in the integrations list", card.ID, card.Integration))
			}
			for slot, conn := range card.Slots {
				if _, ok := cfg.Connections[conn]; !ok {
					out = append(out, c.errf(append(append([]string{}, cardPath...), "slots", slot),
						"card %q: slot %q is bound to undeclared connection %q", card.ID, slot, conn))
				}
			}
		}
	}

	channelIDs := make(map[string]bool, len(cfg.Notifications.Channels))
	for id := range cfg.Notifications.Channels {
		channelIDs[id] = true
	}

	ruleIDs := make(map[string]bool, len(cfg.Rules))
	for ri, rule := range cfg.Rules {
		rulePath := []string{"rules", strconv.Itoa(ri)}
		if ruleIDs[rule.ID] {
			out = append(out, c.errf(append(append([]string{}, rulePath...), "id"), "duplicate rule id %q", rule.ID))
		}
		ruleIDs[rule.ID] = true

		for ni, ch := range rule.Notify {
			if !channelIDs[ch] {
				out = append(out, c.errf(append(append([]string{}, rulePath...), "notify", strconv.Itoa(ni)),
					"rule %q: notifies undeclared channel %q", rule.ID, ch))
			}
		}
		for _, cardID := range referencedCardIDs(rule.When) {
			if !cardIDs[cardID] {
				out = append(out, c.errf(append(append([]string{}, rulePath...), "when"),
					"rule %q: references unknown card %q", rule.ID, cardID))
			}
		}
	}

	out = append(out, validateAuth(cfg.Auth, c)...)
	return out
}

// validateAuth re-checks, at the Go level, the same exactly-one-shape-per-mode invariant the
// schema's own if/then already enforces (types.go's comment on Auth) - defence in depth for the
// same reason internal/state.CardState.Validate re-checks its own schema-enforced invariants:
// a future caller constructing a Config by hand, bypassing Load, should not be able to produce
// an inconsistent one silently.
func validateAuth(a Auth, c semanticCtx) Diagnostics {
	var out Diagnostics
	path := []string{"auth", "mode"}
	switch a.Mode {
	case AuthPassword:
		if a.Admin == nil {
			out = append(out, c.errf(path, "auth.mode is %q but auth.admin is not set", a.Mode))
		}
		if a.Forward != nil {
			out = append(out, c.errf(path, "auth.mode is %q but auth.forward is also set", a.Mode))
		}
	case AuthForward:
		if a.Forward == nil {
			out = append(out, c.errf(path, "auth.mode is %q but auth.forward is not set", a.Mode))
		}
		if a.Admin != nil {
			out = append(out, c.errf(path, "auth.mode is %q but auth.admin is also set", a.Mode))
		}
	case AuthNone:
		if a.Admin != nil || a.Forward != nil {
			out = append(out, c.errf(path, "auth.mode is %q but auth.admin or auth.forward is set", a.Mode))
		}
	}
	return out
}

// cardRefPattern extracts the first string-literal argument to state(...) or signal(...) - the
// only two functions a rule's `when` expression may call against a card id (schemas/config.v1's
// own description for `rule.when`). This is a narrow, load-bearing-for-now stand-in for real expr
// AST parsing: which expression language rules use at all is milestone S3/D7's decision, not
// C1's, so this package does not try to distinguish a call from a same-shaped string elsewhere in
// the expression - internal/contracts.CodeSpans already does that more carefully for the
// adversarial fixture corpus, and is not imported here to keep config.Load's own dependency
// surface independent of that package.
var cardRefPattern = regexp.MustCompile(`\b(?:state|signal)\(\s*"([^"]*)"`)

func referencedCardIDs(when string) []string {
	matches := cardRefPattern.FindAllStringSubmatch(when, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}
