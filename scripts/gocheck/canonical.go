package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"gopkg.in/yaml.v3"
)

// setLikePaths lists the EXACT locations whose arrays are sorted and de-duplicated before
// hashing. Normalising by property name alone was a hole: `params` accepts arbitrary JSON
// Schema, so a manifest could carry `params.default.capabilities` and have two semantically
// different documents hash the same. `*` matches one path element (array index or map key).
// JSON safe integers. JCS interoperability is defined in terms of IEEE 754 doubles, so a value
// outside this range canonicalises differently depending on the reader. Both implementations
// reject rather than silently disagree.
const (
	maxSafeInt = 1<<53 - 1
	minSafeInt = -(1<<53 - 1)
)

var setLikePaths = [][]string{
	{"spec", "capabilities"},
	{"spec", "operations", "*", "routes", "*", "queryKeys"},
}

func isSetLike(path []string) bool {
	for _, p := range setLikePaths {
		if len(p) != len(path) {
			continue
		}
		match := true
		for i := range p {
			if p[i] != "*" && p[i] != path[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func normalise(v any, path []string) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			n, err := normalise(e, append(path, k))
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(t))
		for i, e := range t {
			n, err := normalise(e, append(path, strconv.Itoa(i)))
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		if isSetLike(path) {
			strs := make([]string, 0, len(out))
			for _, e := range out {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("/%s: set-like array must contain strings", strings.Join(path, "/"))
				}
				strs = append(strs, s)
			}
			sort.Strings(strs)
			ded := strs[:0]
			for i, s := range strs {
				if i == 0 || s != strs[i-1] {
					ded = append(ded, s)
				}
			}
			res := make([]any, len(ded))
			for i, s := range ded {
				res[i] = s
			}
			return res, nil
		}
		return out, nil
	case json.Number:
		if strings.ContainsAny(t.String(), ".eE") {
			return nil, fmt.Errorf("/%s: floating-point value %q; manifests must use integers only",
				strings.Join(path, "/"), t)
		}
		i, err := t.Int64()
		if err != nil || i < minSafeInt || i > maxSafeInt {
			return nil, fmt.Errorf("/%s: integer %q outside the JSON safe range ±(2^53-1); "+
				"the canonical form must round-trip through any JSON implementation",
				strings.Join(path, "/"), t)
		}
		return i, nil
	case float64:
		return nil, fmt.Errorf("/%s: floating-point value; manifests must use integers only",
			strings.Join(path, "/"))
	case int:
		if int64(t) < minSafeInt || int64(t) > maxSafeInt {
			return nil, fmt.Errorf("/%s: integer %d outside the JSON safe range ±(2^53-1)",
				strings.Join(path, "/"), t)
		}
		return int64(t), nil
	default:
		return v, nil
	}
}

func escape(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func sortKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
}

func serialise(v any, b *strings.Builder) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case string:
		b.WriteString(escape(t))
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			serialise(e, b)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sortKeys(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(escape(k))
			b.WriteByte(':')
			serialise(t[k], b)
		}
		b.WriteByte('}')
	default:
		panic(fmt.Sprintf("unexpected type %T", v))
	}
}

// yamlToAny decodes YAML the way the production loader must: duplicate mapping keys are an
// error, every scalar keeps the type YAML gave it (so "1.0" stays a string, 1.0 is a float and
// is then rejected as a non-integer), and alias expansion is bounded so an alias bomb cannot
// exhaust memory here any more than it can in the loader.
type yamlBudget struct {
	depth   int
	nodes   int
	aliases int
}

const (
	maxYAMLDepth   = 32
	maxYAMLNodes   = 20000
	maxYAMLAliases = 100
)

func yamlToAny(n *yaml.Node) (any, error) {
	return yamlToAnyBounded(n, &yamlBudget{})
}

func yamlToAnyBounded(n *yaml.Node, b *yamlBudget) (any, error) {
	b.nodes++
	b.depth++
	defer func() { b.depth-- }()
	if b.depth > maxYAMLDepth {
		return nil, fmt.Errorf("YAML nesting deeper than %d", maxYAMLDepth)
	}
	if b.nodes > maxYAMLNodes {
		return nil, fmt.Errorf("YAML node budget of %d exhausted (alias expansion?)", maxYAMLNodes)
	}
	return yamlNode(n, b)
}

func yamlNode(n *yaml.Node, b *yamlBudget) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		return yamlToAnyBounded(n.Content[0], b)
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if _, dup := out[k]; dup {
				return nil, fmt.Errorf("line %d: duplicate mapping key %q", n.Content[i].Line, k)
			}
			v, err := yamlToAnyBounded(n.Content[i+1], b)
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlToAnyBounded(c, b)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.AliasNode:
		b.aliases++
		if b.aliases > maxYAMLAliases {
			return nil, fmt.Errorf("more than %d alias expansions", maxYAMLAliases)
		}
		return yamlToAnyBounded(n.Alias, b)
	case yaml.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		if f, ok := v.(float64); ok {
			return nil, fmt.Errorf("line %d: floating-point value %v; manifests must use integers only", n.Line, f)
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
}

func loadDoc(path string) (any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if ext := filepath.Ext(path); ext == ".yaml" || ext == ".yml" {
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			return nil, err
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("expected exactly one YAML document")
		}
		return yamlToAny(&n)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the first JSON document")
	}
	return doc, nil
}

func digestFile(path string) (string, error) {
	doc, err := loadDoc(path)
	if err != nil {
		return "", err
	}
	n, err := normalise(doc, nil)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	serialise(n, &b)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String()))), nil
}
