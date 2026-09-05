// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"gopkg.in/yaml.v3"
)

// LoadPath loads the conventional on-disk layout: primary, plus every *.yaml file in a sibling
// conf.d directory, applied in lexical filename order (docs/01-architecture.md section 2).
// A missing conf.d directory is not an error - most installations will not have one.
func LoadPath(primary string) (*Snapshot, Diagnostics) {
	dir := filepath.Join(filepath.Dir(primary), "conf.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Load(primary)
		}
		// Anything other than "does not exist" (permissions, a conf.d that is a plain file, ...)
		// is a real, surprising problem worth its own diagnostic rather than a silent skip.
		return nil, Diagnostics{{Severity: SeverityError, File: dir, Message: err.Error()}}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	paths := make([]string, 0, len(names)+1)
	paths = append(paths, primary)
	for _, n := range names {
		paths = append(paths, filepath.Join(dir, n))
	}
	return Load(paths...)
}

// Load reads and merges paths in order (later paths override earlier ones - see merger.merge),
// validates the result against schemas/config.v1.schema.json, decodes it into typed structs and
// runs semantic validation. It never returns a non-nil *Snapshot alongside Diagnostics that
// HasErrors - a caller cannot accidentally act on a partially-valid config.
func Load(paths ...string) (*Snapshot, Diagnostics) {
	if len(paths) == 0 {
		return nil, Diagnostics{{Severity: SeverityError, Message: "config: at least one path is required"}}
	}

	m := newMerger()
	var merged *yaml.Node
	var diags Diagnostics

	for _, path := range paths {
		// #nosec G304 -- path is an operator-supplied config location (a CLI flag or a
		// conf.d directory this same package just listed), the entire point of a config loader,
		// not attacker-controlled request input.
		body, err := os.ReadFile(path)
		if err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, File: path, Message: err.Error()})
			continue
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(body, &doc); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, File: path, Message: "parsing YAML: " + err.Error()})
			continue
		}
		if len(doc.Content) == 0 {
			diags = append(diags, Diagnostic{Severity: SeverityError, File: path, Message: "empty document"})
			continue
		}
		root := doc.Content[0]
		if root.Kind != yaml.MappingNode {
			diags = append(diags, Diagnostic{
				Severity: SeverityError, File: path, Line: root.Line, Column: root.Column,
				Message: "top level must be a mapping",
			})
			continue
		}

		diags = append(diags, duplicateKeys(root, path)...)
		m.tag(root, path)
		merged = m.merge(merged, root, path)
	}

	if diags.HasErrors() {
		return nil, diags
	}

	var generic any
	if err := merged.Decode(&generic); err != nil {
		return nil, append(diags, Diagnostic{Severity: SeverityError, Message: "decoding merged document: " + err.Error()})
	}

	sch, err := configSchema()
	if err != nil {
		return nil, append(diags, Diagnostic{Severity: SeverityError, Message: err.Error()})
	}
	if err := sch.Validate(generic); err != nil {
		diags = append(diags, schemaDiagnostics(err, merged, m)...)
		return nil, diags
	}

	var cfg Config
	if err := merged.Decode(&cfg); err != nil {
		// Schema validation already passed, so reaching here means this package's Go types
		// disagree with the schema - an internal bug, not a config author's mistake.
		return nil, append(diags, Diagnostic{
			Severity: SeverityError,
			Message:  "internal: schema-valid config failed to decode: " + err.Error(),
		})
	}

	diags = append(diags, validateSemantics(&cfg, merged, m)...)
	diags = append(diags, suspiciousSecretRefs(merged, m)...)
	if diags.HasErrors() {
		return nil, diags
	}

	return newSnapshot(cfg, secretLocations(merged, m)), diags
}

// schemaDiagnostics flattens a jsonschema validation error tree into Diagnostics, recovering a
// file:line:col for each leaf cause by walking its InstanceLocation through the same merged Node
// tree that was validated, then asking the merger which file that node came from.
func schemaDiagnostics(err error, root *yaml.Node, m *merger) Diagnostics {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return Diagnostics{{Severity: SeverityError, Message: err.Error()}}
	}
	var out Diagnostics
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			path := e.InstanceLocation
			// additionalProperties reports the CONTAINING object's location, not the offending
			// key's - kind.AdditionalProperties names it, so extend the path one more segment to
			// point at the actual key rather than the object's opening brace.
			if extra, ok := e.ErrorKind.(*kind.AdditionalProperties); ok && len(extra.Properties) > 0 {
				path = append(append([]string{}, path...), extra.Properties[0])
			}
			n := nodeAtPath(root, path)
			out = append(out, Diagnostic{
				Severity: SeverityError,
				File:     m.fileOf(n),
				Line:     n.Line,
				Column:   n.Column,
				Message:  e.Error(),
			})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}
