// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"strconv"

	"gopkg.in/yaml.v3"
)

// merger merges conf.d layers over the primary file and remembers, for every node reachable in
// the result, which source file to blame it on - the whole reason file:line:col is possible once
// several files have been combined into one tree.
//
// Walking here only ever follows Content, never Alias: an AliasNode's Content is empty, so a
// document with a self-referential anchor (a: &x {b: *x}) cannot make this walk recurse forever -
// confirmed separately that yaml.v3's own Node.Decode already rejects such a document outright
// ("yaml: anchor ... value contains itself") before this package ever sees a decoded value, so
// alias-cycle safety does not depend on this walker being careful; it is written carefully anyway,
// since a provenance walker over the tree is exactly the kind of code a future change could grow
// an Alias-following branch into without anyone noticing why that would be dangerous.
type merger struct {
	file map[*yaml.Node]string
}

func newMerger() *merger { return &merger{file: map[*yaml.Node]string{}} }

func (m *merger) tag(root *yaml.Node, file string) {
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if _, seen := m.file[n]; seen {
			return
		}
		m.file[n] = file
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
}

// fileOf returns the file a node should be blamed on, or "" if merge() never tagged it (should
// not happen for any node reachable from the tree merge() returns).
func (m *merger) fileOf(n *yaml.Node) string { return m.file[n] }

// merge combines base (the accumulated result so far, nil for the first file) with override (the
// next file in priority order), per docs/01-architecture.md section 2: "later files override,
// arrays replace." Only mappings recurse key-by-key; anything else at a shared key is replaced
// wholesale by override, including arrays and scalars - a later file cannot append to an earlier
// file's array, only replace it outright, which is a deliberate, documented simplicity: partial
// list edits belong to the 0.3 config editor's Patch API, not conf.d layering.
func (m *merger) merge(base, override *yaml.Node, overrideFile string) *yaml.Node {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}
	if base.Kind == yaml.MappingNode && override.Kind == yaml.MappingNode {
		merged := m.mergeMappings(base, override, overrideFile)
		// The merged node itself is synthetic - it belongs to neither file verbatim. Blaming it
		// on the higher-priority file is the more useful guess for someone troubleshooting a
		// merged config: it is the file that most recently touched this level.
		m.file[merged] = overrideFile
		return merged
	}
	return override
}

type kv struct{ key, val *yaml.Node }

func pairsOf(n *yaml.Node) []kv {
	out := make([]kv, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, kv{n.Content[i], n.Content[i+1]})
	}
	return out
}

func (m *merger) mergeMappings(base, override *yaml.Node, overrideFile string) *yaml.Node {
	basePairs := pairsOf(base)
	overridePairs := pairsOf(override)
	overrideByKey := make(map[string]kv, len(overridePairs))
	for _, p := range overridePairs {
		overrideByKey[p.key.Value] = p
	}

	merged := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := make(map[string]bool, len(basePairs))
	for _, bp := range basePairs {
		seen[bp.key.Value] = true
		if op, ok := overrideByKey[bp.key.Value]; ok {
			merged.Content = append(merged.Content, op.key, m.merge(bp.val, op.val, overrideFile))
			continue
		}
		merged.Content = append(merged.Content, bp.key, bp.val)
	}
	for _, op := range overridePairs {
		if seen[op.key.Value] {
			continue
		}
		merged.Content = append(merged.Content, op.key, op.val)
	}
	return merged
}

// duplicateKeys finds every mapping in the tree (following only Content, per the type comment
// above) that declares the same key twice - a real YAML footgun: the decoder silently keeps only
// the last one, with no error, so a config author who pastes a block twice and forgets to rename
// one key gets no warning at all without this check. Checked per file, before merging - conf.d
// overriding a key in an earlier file is the entire point of conf.d, not this bug.
func duplicateKeys(root *yaml.Node, file string) Diagnostics {
	var out Diagnostics
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.MappingNode {
			seen := make(map[string]*yaml.Node, len(n.Content)/2)
			for _, p := range pairsOf(n) {
				if prior, ok := seen[p.key.Value]; ok {
					out = append(out, Diagnostic{
						Severity: SeverityError,
						File:     file,
						Line:     p.key.Line,
						Column:   p.key.Column,
						Message: "duplicate key " + strconv.Quote(p.key.Value) +
							" (first seen at line " + strconv.Itoa(prior.Line) + ")",
					})
					continue
				}
				seen[p.key.Value] = p.key
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
	return out
}

// nodeAtPath walks a JSON-Schema InstanceLocation (object keys, or decimal array indices) through
// root, returning the deepest node reached. It never returns nil for a non-nil root: an empty
// path returns root, and a path that runs out of structure partway (a missing required key has
// no node of its own to point at) returns the last real node found, which is the nearest useful
// position - the containing object.
func nodeAtPath(root *yaml.Node, path []string) *yaml.Node {
	cur := root
	for _, seg := range path {
		switch cur.Kind {
		case yaml.MappingNode:
			next := (*yaml.Node)(nil)
			for _, p := range pairsOf(cur) {
				if p.key.Value == seg {
					next = p.val
					break
				}
			}
			if next == nil {
				return cur
			}
			cur = next
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(cur.Content) {
				return cur
			}
			cur = cur.Content[idx]
		default:
			return cur
		}
	}
	return cur
}
