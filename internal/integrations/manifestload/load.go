// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"veduta.dev/veduta/internal/canonical"
	"veduta.dev/veduta/internal/connections/routepath"
	"veduta.dev/veduta/schemas"
)

const maxManifestBytes = 256 << 10

type rawManifest struct {
	Metadata struct{ ID, Name, Version string } `yaml:"metadata"`
	Spec     struct {
		Runtime      string   `yaml:"runtime"`
		Module       string   `yaml:"module"`
		SHA256       string   `yaml:"sha256"`
		Capabilities []string `yaml:"capabilities"`
		Slots        []struct {
			Name, Kind string
			Required   *bool `yaml:"required"`
		} `yaml:"slots"`
		Limits     Limits `yaml:"limits"`
		Operations []struct {
			ID             string `yaml:"id"`
			Name           string `yaml:"name"`
			DefaultRefresh string `yaml:"defaultRefresh"`
			Params         any
			Routes         []Route
			Signals        []SignalDecl
			Pipeline       []struct {
				As      string
				When    *nodeValue
				Request struct {
					Slot, Method   string
					Path           nodeValue
					Query, Headers map[string]nodeValue
					Body           *struct {
						JSON *nodeValue
						Form map[string]nodeValue
					}
				}
			}
			Output nodeValue
		} `yaml:"operations"`
	} `yaml:"spec"`
}

type nodeValue struct{ Node *yaml.Node }

func (v *nodeValue) UnmarshalYAML(n *yaml.Node) error { //nolint:revive // yaml.Unmarshaler requires this exported method.
	v.Node = n
	return nil
}

// readBoundedFile reads path, refusing to allocate more than maxBytes+1 bytes regardless of the
// file's actual size - a plain os.ReadFile followed by a length check reads (and allocates) the
// entire file first, so a multi-gigabyte manifest would be fully read into memory before being
// rejected, which is the exact unbounded-read a byte cap exists to prevent (found in a second
// review pass). The +1 is so a file of exactly maxBytes+1 or more is detected as oversized rather
// than silently truncated to a validly-sized prefix - io.LimitReader stops supplying bytes at the
// limit, it does not error, so the caller must compare the returned length against maxBytes itself.
func readBoundedFile(path string, maxBytes int) ([]byte, error) {
	// #nosec G304 -- path is the operator-selected integration manifest location.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("%s: manifest exceeds %d bytes", path, maxBytes)
	}
	return raw, nil
}

// Load parses, bounds, schema-validates, and compiles the template grammar at path. The file is
// read exactly once, via readBoundedFile: found in review that this used to read the same path
// three separate times (once streaming for the YAML depth/node walk, once more for the exact
// byte-size check, and a third time inside canonical.DigestFile) - a real TOCTOU window on
// exactly the guarantee digest-bound approval exists to provide. A concurrent replacement between
// any of those reads could load one version's structure while digesting another's bytes, silently
// decoupling the approval record from what actually gets executed. Every check below - byte cap,
// YAML depth/node/alias/duplicate-key limits, schema validation, structural decode, and the
// canonical digest - now runs against the one immutable byte slice read here.
func Load(path string) (*Manifest, error) {
	raw, err := readBoundedFile(path, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	return loadBytes(path, raw)
}

// loadBytes contains every parser and validator step after the single bounded file read. Keeping
// it separate lets the fuzz target exercise arbitrary bytes without creating a file per input.
func loadBytes(path string, raw []byte) (*Manifest, error) {
	if len(raw) > maxManifestBytes {
		return nil, fmt.Errorf("%s: manifest exceeds %d bytes", path, maxManifestBytes)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if root.Content == nil {
		return nil, fmt.Errorf("%s: empty manifest", path)
	}
	stats := yamlStats{}
	if err := walkYAML(root.Content[0], 1, &stats); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if stats.nodes > 20000 || stats.depth > 32 {
		return nil, fmt.Errorf("%s: YAML exceeds node/depth limit", path)
	}
	var generic any
	if err := root.Content[0].Decode(&generic); err != nil {
		return nil, err
	}
	if err := validateSchema(generic); err != nil {
		return nil, fmt.Errorf("%s does not match manifest schema: %w", path, err)
	}
	var doc rawManifest
	if err := root.Content[0].Decode(&doc); err != nil {
		return nil, err
	}
	digest, err := canonical.DigestBytes(raw, filepath.Ext(path))
	if err != nil {
		return nil, err
	}
	m := &Manifest{Path: path, Digest: digest, ID: doc.Metadata.ID, Name: doc.Metadata.Name, Version: doc.Metadata.Version, Runtime: doc.Spec.Runtime, Capabilities: doc.Spec.Capabilities}
	m.Module, m.ModuleSHA256 = doc.Spec.Module, doc.Spec.SHA256
	for _, s := range doc.Spec.Slots {
		req := true
		if s.Required != nil {
			req = *s.Required
		}
		m.Slots = append(m.Slots, SlotSpec{s.Name, s.Kind, req})
	}
	m.Limits = doc.Spec.Limits.WithDefaults()
	manifestExpr := 0
	literalBytes := 0
	for oi, o := range doc.Spec.Operations {
		op := OperationDef{ID: o.ID, Name: o.Name, DefaultRefresh: o.DefaultRefresh, Params: o.Params, Routes: o.Routes, Signals: o.Signals}
		totalExpr := 0
		for si, s := range o.Pipeline {
			ps := PipelineStep{As: s.As}
			if s.When != nil {
				ps.When, err = compileExprNode(s.When.Node, m.Limits.ExprNodes)
				if err != nil {
					return nil, pathErr(path, s.When.Node, err)
				}
				totalExpr += ps.When.Nodes
			}
			ps.Request.Slot = s.Request.Slot
			ps.Request.Method = s.Request.Method
			if ps.Request.Method == "" {
				ps.Request.Method = "GET"
			}
			ps.Request.Path, err = compileTemplate(s.Request.Path.Node, &totalExpr, m.Limits.ExprNodes)
			if err != nil {
				return nil, pathErr(path, s.Request.Path.Node, err)
			}
			ps.Request.Query, err = compileMap(s.Request.Query, &totalExpr, m.Limits.ExprNodes)
			if err != nil {
				return nil, err
			}
			ps.Request.Headers, err = compileMap(s.Request.Headers, &totalExpr, m.Limits.ExprNodes)
			if err != nil {
				return nil, err
			}
			if s.Request.Body != nil {
				b := &BodyDef{}
				if s.Request.Body.JSON != nil {
					b.JSON, err = compileTemplate(s.Request.Body.JSON.Node, &totalExpr, m.Limits.ExprNodes)
				} else {
					b.Form, err = compileMap(s.Request.Body.Form, &totalExpr, m.Limits.ExprNodes)
				}
				if err != nil {
					return nil, err
				}
				ps.Request.Body = b
			}
			if err = validateRequest(m, &op, &ps.Request); err != nil {
				return nil, fmt.Errorf("operation %s pipeline %d: %w", o.ID, si, err)
			}
			op.Pipeline = append(op.Pipeline, ps)
		}
		op.Output, err = compileTemplate(o.Output.Node, &totalExpr, m.Limits.ExprNodes)
		if err != nil {
			return nil, pathErr(path, o.Output.Node, err)
		}
		if totalExpr > 4096 {
			return nil, fmt.Errorf("operation %s: aggregate expression nodes %d exceed 4096", o.ID, totalExpr)
		}
		manifestExpr += totalExpr
		ts := templateStats{}
		countTemplate(op.Output, 1, &ts)
		literalBytes += ts.literalBytes
		if ts.nodes > 4096 || ts.depth > 24 {
			return nil, fmt.Errorf("operation %s: output template exceeds node/depth limit", op.ID)
		}
		if err = validateOutput(m, &op, op.Output); err != nil {
			return nil, fmt.Errorf("operation %s output: %w", op.ID, err)
		}
		m.Operations = append(m.Operations, op)
		_ = oi
	}
	if manifestExpr > 32768 {
		return nil, fmt.Errorf("manifest aggregate expression nodes %d exceed 32768", manifestExpr)
	}
	if literalBytes > 64<<10 {
		return nil, fmt.Errorf("manifest literal strings exceed 64 KiB")
	}
	return m, nil
}

type templateStats struct{ nodes, depth, literalBytes int }

func countTemplate(t *Template, d int, s *templateStats) {
	if t == nil {
		return
	}
	s.nodes++
	if d > s.depth {
		s.depth = d
	}
	if x, ok := t.Literal.(string); ok {
		s.literalBytes += len(x)
	}
	for _, v := range t.Object {
		countTemplate(v, d+1, s)
	}
	for _, v := range t.Array {
		countTemplate(v, d+1, s)
	}
	if t.Each != nil {
		countTemplate(t.Each.Item, d+1, s)
	}
	if t.Asset != nil {
		countTemplate(t.Asset.Path, d+1, s)
		for _, v := range t.Asset.Query {
			countTemplate(v, d+1, s)
		}
	}
}

type yamlStats struct{ nodes, depth int }

func walkYAML(n *yaml.Node, d int, s *yamlStats) error {
	s.nodes++
	if d > s.depth {
		s.depth = d
	}
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("YAML aliases are forbidden at %d:%d", n.Line, n.Column)
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if seen[k] {
				return fmt.Errorf("duplicate key %q at %d:%d", k, n.Content[i].Line, n.Content[i].Column)
			}
			seen[k] = true
		}
	}
	for _, c := range n.Content {
		if err := walkYAML(c, d+1, s); err != nil {
			return err
		}
	}
	return nil
}
func validateSchema(v any) error {
	body, e := schemas.FS.ReadFile("plugin-manifest.v1.schema.json")
	if e != nil {
		return e
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if e != nil {
		return e
	}
	c := jsonschema.NewCompiler()
	const id = "https://veduta.dev/schemas/plugin-manifest.v1.schema.json"
	if e = c.AddResource(id, doc); e != nil {
		return e
	}
	s, e := c.Compile(id)
	if e != nil {
		return e
	}
	return s.Validate(v)
}

func compileMap(in map[string]nodeValue, total *int, max int) (map[string]*Template, error) {
	if in == nil {
		return nil, nil
	}
	out := map[string]*Template{}
	for k, n := range in {
		t, e := compileTemplate(n.Node, total, max)
		if e != nil {
			return nil, e
		}
		out[k] = t
	}
	return out, nil
}
func compileTemplate(n *yaml.Node, total *int, max int) (*Template, error) {
	if n == nil {
		return nil, nil
	}
	t := &Template{Node: n}
	switch n.Kind {
	case yaml.ScalarNode:
		var v any
		if e := n.Decode(&v); e != nil {
			return nil, e
		}
		t.Kind = "literal"
		t.Literal = v
	case yaml.SequenceNode:
		t.Kind = "array"
		for _, c := range n.Content {
			x, e := compileTemplate(c, total, max)
			if e != nil {
				return nil, e
			}
			t.Array = append(t.Array, x)
		}
	case yaml.MappingNode:
		keys := map[string]*yaml.Node{}
		for i := 0; i < len(n.Content); i += 2 {
			keys[n.Content[i].Value] = n.Content[i+1]
		}
		if e, ok := keys["expr"]; ok && len(keys) == 1 {
			t.Kind = "expr"
			x, er := compileExprNode(n, max)
			if er != nil {
				return nil, er
			}
			t.Expr = x
			*total += x.Nodes
			_ = e
		} else if a, ok := keys["asset"]; ok && len(keys) == 1 {
			t.Kind = "asset"
			var raw struct {
				Slot, Transform string
				Path            nodeValue
				Query           map[string]nodeValue
			}
			if er := a.Decode(&raw); er != nil {
				return nil, er
			}
			p, er := compileTemplate(raw.Path.Node, total, max)
			if er != nil {
				return nil, er
			}
			q, er := compileMap(raw.Query, total, max)
			if er != nil {
				return nil, er
			}
			t.Asset = &AssetDef{raw.Slot, p, q, raw.Transform}
		} else if _, ok := keys["each"]; ok && keys["as"] != nil && keys["item"] != nil && len(keys) == 3 {
			t.Kind = "each"
			x, er := compileExprNode(keys["each"], max)
			if er != nil {
				return nil, er
			}
			*total += x.Nodes
			item, er := compileTemplate(keys["item"], total, max)
			if er != nil {
				return nil, er
			}
			t.Each = &EachDef{x, keys["as"].Value, item}
		} else {
			t.Kind = "object"
			t.Object = map[string]*Template{}
			for i := 0; i < len(n.Content); i += 2 {
				x, er := compileTemplate(n.Content[i+1], total, max)
				if er != nil {
					return nil, er
				}
				t.Object[n.Content[i].Value] = x
			}
		}
	default:
		return nil, fmt.Errorf("unsupported YAML node at %d:%d", n.Line, n.Column)
	}
	return t, nil
}
func compileExprNode(n *yaml.Node, max int) (*Expression, error) {
	var raw struct{ Expr string }
	if e := n.Decode(&raw); e != nil {
		return nil, e
	}
	return compileExprSource(raw.Expr, n.Line, n.Column, max)
}

// compileExprSource is compileExprNode's core, factored out for callers with no backing
// *yaml.Node - http-json's synthesised, card-configured expressions (httpjson.go) go through
// this exact same AST-size and $env/matches/__-prefix/custom-call validation, not a parallel,
// less-scrutinised check: docs/01-architecture.md is explicit that http-json gets "the same
// expression and resource budgets as any declarative integration."
func compileExprSource(src string, line, col, max int) (*Expression, error) {
	tree, e := parser.Parse(src)
	if e != nil {
		return nil, e
	}
	v := &exprVisitor{}
	ast.Walk(&tree.Node, v)
	if v.err != nil {
		return nil, v.err
	}
	if v.count > max {
		return nil, fmt.Errorf("expression has %d AST nodes, limit %d", v.count, max)
	}
	return &Expression{src, line, col, v.count}, nil
}

type exprVisitor struct {
	count int
	err   error
}

func (v *exprVisitor) Visit(p *ast.Node) { //nolint:revive // ast.Visitor requires this exported method.
	v.count++
	switch n := (*p).(type) {
	case *ast.IdentifierNode:
		// "$env" exposes the whole environment; "__"-prefixed names are the declarative
		// runtime's own internal wiring (the invocation context, the capability grant) threaded
		// through the same env map expressions evaluate against, per necessity of expr's
		// WithContext mechanism (which requires a named, addressable variable) - never data a
		// manifest is allowed to introspect. Rejecting the whole prefix, not just today's known
		// names, means a future internal identifier is closed off by construction rather than
		// requiring this list to be extended in lockstep.
		if n.Value == "$env" || strings.HasPrefix(n.Value, "__") {
			v.err = fmt.Errorf("%q is forbidden", n.Value)
		}
	case *ast.BinaryNode:
		if n.Operator == "matches" {
			v.err = errors.New("matches is not supported")
		}
	case *ast.CallNode:
		v.err = errors.New("custom function calls are not supported")
	case *ast.VariableDeclaratorNode, *ast.SequenceNode, *ast.SliceNode, *ast.ChainNode, *ast.BytesNode:
		v.err = fmt.Errorf("expression node %T is not supported", n)
	case *ast.NilNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.StringNode, *ast.ConstantNode,
		*ast.UnaryNode, *ast.MemberNode, *ast.BuiltinNode, *ast.PredicateNode, *ast.PointerNode,
		*ast.ConditionalNode, *ast.ArrayNode, *ast.MapNode, *ast.PairNode:
		// Explicitly supported AST surface.
	default:
		v.err = fmt.Errorf("expression node %T is not supported", n)
	}
}
func validateRequest(m *Manifest, op *OperationDef, r *RequestDef) error {
	if !hasCapability(m, "http") {
		return errors.New("pipeline request requires undeclared http capability")
	}
	slot := false
	for _, s := range m.Slots {
		slot = slot || s.Name == r.Slot
	}
	if !slot {
		return fmt.Errorf("undeclared slot %q", r.Slot)
	}
	if r.Path == nil || r.Path.Kind != "literal" {
		return nil
	}
	p, ok := r.Path.Literal.(string)
	if !ok {
		return errors.New("request path must evaluate to string")
	}
	cp, e := routepath.Canonicalise(p)
	if e != nil {
		return e
	}
	for _, rt := range op.Routes {
		rp, x := routepath.Canonicalise(rt.Path)
		if x == nil && rt.Slot == r.Slot && rt.Method == r.Method && (rt.Use == "" || rt.Use == "data") && routepath.Match(rp, cp) && routeCoversRequest(rt, r) {
			return nil
		}
	}
	return fmt.Errorf("no declared route covers %s %s", r.Method, p)
}

func hasCapability(m *Manifest, want string) bool {
	for _, c := range m.Capabilities {
		if c == want {
			return true
		}
	}
	return false
}
func routeCoversRequest(r Route, req *RequestDef) bool {
	if r.QueryKeys != nil {
		allowed := map[string]bool{}
		for _, k := range r.QueryKeys {
			allowed[k] = true
		}
		for k := range req.Query {
			if !allowed[k] {
				return false
			}
		}
	}
	if req.Body != nil {
		want := "application/json"
		if req.Body.Form != nil {
			want = "application/x-www-form-urlencoded"
		}
		if r.ContentType != "" && !strings.HasPrefix(strings.ToLower(r.ContentType), want) {
			return false
		}
	}
	return true
}

func validateOutput(m *Manifest, op *OperationDef, t *Template) error {
	if t == nil {
		return nil
	}
	if t.Asset != nil {
		if !hasCapability(m, "assets") {
			return errors.New("asset node requires undeclared assets capability")
		}
		slot := false
		for _, s := range m.Slots {
			slot = slot || s.Name == t.Asset.Slot
		}
		if !slot {
			return fmt.Errorf("asset uses undeclared slot %q", t.Asset.Slot)
		}
		if t.Asset.Path != nil && t.Asset.Path.Kind == "literal" {
			p, ok := t.Asset.Path.Literal.(string)
			if !ok {
				return errors.New("asset path must be a string")
			}
			cp, e := routepath.Canonicalise(p)
			if e != nil {
				return e
			}
			matched := false
			for _, r := range op.Routes {
				rp, x := routepath.Canonicalise(r.Path)
				matched = matched || (x == nil && r.Slot == t.Asset.Slot && r.Use == "asset" && r.Method == http.MethodGet && routepath.Match(rp, cp))
			}
			if !matched {
				return fmt.Errorf("no asset route covers GET %s", p)
			}
		}
	}
	if t.Each != nil {
		if e := validateOutput(m, op, t.Each.Item); e != nil {
			return e
		}
	}
	for _, x := range t.Object {
		if e := validateOutput(m, op, x); e != nil {
			return e
		}
	}
	for _, x := range t.Array {
		if e := validateOutput(m, op, x); e != nil {
			return e
		}
	}
	if t == op.Output && t.Kind == "object" {
		decl := map[string]bool{}
		for _, s := range op.Signals {
			decl[s.Name] = true
		}
		if signals := t.Object["signals"]; signals != nil && signals.Kind == "object" {
			for name := range signals.Object {
				if !decl[name] {
					return fmt.Errorf("signal %q is not declared", name)
				}
			}
		}
	}
	return nil
}
func pathErr(path string, n *yaml.Node, e error) error {
	if n == nil {
		return fmt.Errorf("%s: %w", path, e)
	}
	return fmt.Errorf("%s:%d:%d: %w", path, n.Line, n.Column, e)
}
