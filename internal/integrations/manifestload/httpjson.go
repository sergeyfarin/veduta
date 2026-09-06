// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import (
	"fmt"
	"net/http"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"

	"veduta.dev/veduta/internal/connections/routepath"
)

// HTTPJSONDigest is the sentinel "digest" a synthesised http-json manifest and its self-approving
// lock entry both carry. A real manifest's digest detects drift between what was approved and
// what is now on disk; http-json has no file to drift from - it is synthesised fresh from the
// same card configuration every invocation, so there is nothing external for a digest to compare
// against. The sentinel exists so callers construct an explicit, matching pair rather than
// relying on both sides happening to default to the empty string.
const HTTPJSONDigest = "http-json"

// httpJSONExprMax is the per-expression AST-node ceiling for a card's `view:` block. http-json
// has no manifest of its own to declare a limit, so it inherits the core default rather than
// being unbounded - "the same expression and resource budgets as any declarative integration"
// (docs/01-architecture.md's "http-json escape hatch").
const httpJSONExprMax = 512

// httpJSONAggregateExprMax mirrors a single operation's aggregate expression-node ceiling.
const httpJSONAggregateExprMax = 4096

// SynthesizeHTTPJSON builds a single-operation, in-memory Manifest for one http-json card - the
// generic HTTP/JSON escape hatch (docs/01-architecture.md's "http-json escape hatch", milestone
// D4). It reuses the declarative runtime wholesale rather than a second, less-scrutinised
// template engine: the resulting Manifest is loaded and invoked through the exact same
// `internal/integrations/declarative` code path a real manifest is, so it gets the identical
// charged-builtin expression sandbox, budgets, and `$env`/`__`-prefix/`matches`/custom-call
// rejection for free.
//
// slot/method/path/query describe the one upstream request the card names - http-json's fixed
// surface (GET or POST only, no custom headers, no redirects - the latter two are already the
// broker's and the connection's own defaults, not something this function has to enforce again).
// view is the card's own inline `view:` block; http-json has no signals, being presentation-only.
func SynthesizeHTTPJSON(slot, method, path string, query map[string]string, view map[string]any) (*Manifest, error) {
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return nil, fmt.Errorf("http-json: method %q is not GET or POST", method)
	}
	cp, err := routepath.Canonicalise(path)
	if err != nil {
		return nil, fmt.Errorf("http-json: path %q: %w", path, err)
	}

	m := &Manifest{
		Digest: HTTPJSONDigest, ID: "http-json", Name: "Generic HTTP/JSON", Runtime: "declarative",
		Capabilities: []string{"http"},
		Slots:        []SlotSpec{{Name: slot, Kind: "http", Required: true}},
		Limits:       Limits{}.WithDefaults(),
	}

	var queryTemplates map[string]*Template
	if len(query) > 0 {
		queryTemplates = make(map[string]*Template, len(query))
		for k, v := range query {
			queryTemplates[k] = &Template{Kind: "literal", Literal: v}
		}
	}
	op := OperationDef{
		ID:     "request",
		Routes: []Route{{Slot: slot, Method: method, Path: cp}},
		Pipeline: []PipelineStep{{
			As: "data",
			Request: RequestDef{
				Slot: slot, Method: method,
				Path:  &Template{Kind: "literal", Literal: cp},
				Query: queryTemplates,
			},
		}},
	}
	if err = validateRequest(m, &op, &op.Pipeline[0].Request); err != nil {
		return nil, err
	}

	total := 0
	op.Output, err = compileView(view, &total, httpJSONExprMax)
	if err != nil {
		return nil, err
	}
	if total > httpJSONAggregateExprMax {
		return nil, fmt.Errorf("http-json: view expression nodes %d exceed %d", total, httpJSONAggregateExprMax)
	}
	m.Operations = []OperationDef{op}
	return m, nil
}

// compileView translates a card's inline `view:` block into the same Template grammar a real
// manifest's output uses, with one deliberate departure from "no name-based inference": http-json
// is explicitly a simpler, Homepage-migration-shaped convention (docs/01-architecture.md's
// "http-json escape hatch"), not the general-purpose extension mechanism that rule protects, so a
// string value under the key "value" or "progress" - MetricItem.Value and ProgressItem.Progress/
// Value, the two dynamic fields the view blocks actually populate - is compiled as an expression,
// exactly as `num_blocked_filtering / num_dns_queries` is written in examples/veduta.yaml with no
// `{expr: ...}` wrapping. Every other key is literal structure, recursed unchanged.
func compileView(v any, total *int, max int) (*Template, error) {
	switch x := v.(type) {
	case map[string]any:
		obj := make(map[string]*Template, len(x))
		for k, val := range x {
			if s, ok := val.(string); ok && (k == "value" || k == "progress") {
				bound, err := bindBareFieldsToData(s)
				if err != nil {
					return nil, fmt.Errorf("view.%s: %w", k, err)
				}
				e, err := compileExprSource(bound, 0, 0, max)
				if err != nil {
					return nil, fmt.Errorf("view.%s: %w", k, err)
				}
				*total += e.Nodes
				obj[k] = &Template{Kind: "expr", Expr: e}
				continue
			}
			t, err := compileView(val, total, max)
			if err != nil {
				return nil, err
			}
			obj[k] = t
		}
		return &Template{Kind: "object", Object: obj}, nil
	case []any:
		arr := make([]*Template, 0, len(x))
		for _, val := range x {
			t, err := compileView(val, total, max)
			if err != nil {
				return nil, err
			}
			arr = append(arr, t)
		}
		return &Template{Kind: "array", Array: arr}, nil
	default:
		return &Template{Kind: "literal", Literal: x}, nil
	}
}

// httpJSONReservedNames are the only real top-level bindings an http-json view expression ever
// sees (declarative.instance.Invoke's own env: "params", "now", plus the internal "__grant"/
// "__ctx" already rejected by exprVisitor) - every other bare identifier is a JSON field name
// from the single pipeline response bound under "data" (SynthesizeHTTPJSON's own PipelineStep.As),
// so it is rewritten to member access on "data" here rather than left to resolve against an env
// key that was never bound at the top level.
var httpJSONReservedNames = map[string]bool{"params": true, "now": true}

// bindBareFieldsToData rewrites every bare identifier in src (except the reserved names above)
// into member access on "data" - `num_dns_queries` becomes `data.num_dns_queries` - so a card
// author can write the same bare-field expressions Homepage's customapi mapping already used,
// matching examples/veduta.yaml's own http-json card, without D3's declarative runtime (unchanged
// by this milestone) needing to special-case how its pipeline results are bound into the
// environment. The rewritten form, not the original text, is what actually gets compiled and
// validated (compileExprSource re-parses it), so what runs is exactly what was checked.
func bindBareFieldsToData(src string) (string, error) {
	tree, err := parser.Parse(src)
	if err != nil {
		return "", err
	}
	rewritten := bindNode(tree.Node)
	return rewritten.String(), nil
}

func bindNode(n ast.Node) ast.Node {
	switch x := n.(type) {
	case *ast.IdentifierNode:
		if httpJSONReservedNames[x.Value] {
			return x
		}
		return &ast.MemberNode{Node: &ast.IdentifierNode{Value: "data"}, Property: &ast.StringNode{Value: x.Value}}
	case *ast.MemberNode:
		x.Node = bindNode(x.Node)
		x.Property = bindNode(x.Property)
		return x
	case *ast.BinaryNode:
		x.Left = bindNode(x.Left)
		x.Right = bindNode(x.Right)
		return x
	case *ast.UnaryNode:
		x.Node = bindNode(x.Node)
		return x
	case *ast.ConditionalNode:
		x.Cond = bindNode(x.Cond)
		x.Exp1 = bindNode(x.Exp1)
		x.Exp2 = bindNode(x.Exp2)
		return x
	case *ast.ArrayNode:
		for i := range x.Nodes {
			x.Nodes[i] = bindNode(x.Nodes[i])
		}
		return x
	case *ast.MapNode:
		for i := range x.Pairs {
			x.Pairs[i] = bindNode(x.Pairs[i])
		}
		return x
	case *ast.PairNode:
		x.Key = bindNode(x.Key)
		x.Value = bindNode(x.Value)
		return x
	default:
		// Constants, "#"/pointer nodes, and anything else exprVisitor's own allowlist governs
		// pass through unchanged; compileExprSource still validates the rewritten whole.
		return x
	}
}
