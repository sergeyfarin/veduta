// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

var predicates = map[string]bool{"filter": true, "map": true, "all": true, "none": true, "any": true, "one": true, "count": true, "sum": true, "find": true, "findIndex": true, "findLast": true, "findLastIndex": true, "groupBy": true, "sortBy": true, "reduce": true}
var scalarBuiltins = []string{"len", "abs", "ceil", "floor", "round", "int", "float", "string", "trim", "upper", "lower", "split", "replace", "hasPrefix", "hasSuffix", "date", "duration", "now"}
var replacedBuiltins = map[string]bool{"first": true, "last": true, "take": true}
var deniedBuiltins = map[string]bool{
	"type": true, "trimPrefix": true, "trimSuffix": true, "splitAfter": true, "repeat": true, "join": true,
	"indexOf": true, "lastIndexOf": true, "max": true, "min": true, "mean": true, "median": true,
	"toJSON": true, "fromJSON": true, "toBase64": true, "fromBase64": true, "timezone": true, "get": true,
	"keys": true, "values": true, "toPairs": true, "fromPairs": true, "reverse": true, "uniq": true,
	"concat": true, "flatten": true, "sort": true, "bitnot": true,
	"bitand": true, "bitor": true, "bitxor": true, "bitnand": true, "bitshl": true, "bitshr": true, "bitushr": true,
}

type chargePatcher struct{}

func (chargePatcher) Visit(node *ast.Node) { //nolint:revive // ast.Visitor requires this exported method.
	n, ok := (*node).(*ast.BuiltinNode)
	if !ok || !predicates[n.Name] || len(n.Arguments) == 0 {
		return
	}
	n.Arguments[0] = &ast.CallNode{Callee: &ast.IdentifierNode{Value: "__d3_charge"}, Arguments: []ast.Node{n.Arguments[0], &ast.StringNode{Value: n.Name}}}
}

type budgetKey struct{}

func compile(source string, max int) (*vm.Program, error) {
	opts := []expr.Option{expr.AllowUndefinedVariables(), expr.AsAny(), expr.MaxNodes(uint(max)), expr.DisableAllBuiltins(), expr.Patch(chargePatcher{}), expr.Function("__d3_charge", func(p ...any) (any, error) {
		ctx, ok := p[0].(context.Context)
		if !ok {
			return nil, fmt.Errorf("missing invocation context")
		}
		b, ok := ctx.Value(budgetKey{}).(*budget)
		if !ok {
			return nil, fmt.Errorf("missing invocation budget")
		}
		return b.charge(p[1], p[2].(string))
	}, new(func(context.Context, any, string) any)), expr.WithContext("__ctx")}
	opts = append(opts,
		expr.Function("first", func(p ...any) (any, error) { return endpoint(p[1], false) }, new(func(context.Context, any) any)),
		expr.Function("last", func(p ...any) (any, error) { return endpoint(p[1], true) }, new(func(context.Context, any) any)),
		expr.Function("take", func(p ...any) (any, error) {
			b := p[0].(context.Context).Value(budgetKey{}).(*budget)
			n, ok := integer(p[2])
			if !ok {
				return nil, fmt.Errorf("take count must be an integer")
			}
			rv := reflect.ValueOf(p[1])
			if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
				return nil, fmt.Errorf("take requires a collection")
			}
			if n < 0 {
				n = 0
			}
			if n > rv.Len() {
				n = rv.Len()
			}
			b.mu.Lock()
			if n > b.iterations {
				b.mu.Unlock()
				return nil, errBudgetExceeded
			}
			b.iterations -= n
			b.mu.Unlock()
			out := make([]any, n)
			for x := range n {
				out[x] = rv.Index(x).Interface()
			}
			return out, nil
		}, new(func(context.Context, any, any) any)),
		expr.Function("fromUnix", func(p ...any) (any, error) { return fromUnix(p[0]) }, new(func(any) string)),
		expr.Function("unix", func(p ...any) (any, error) { return unix(p[0]) }, new(func(any) int)),
	)
	for n := range predicates {
		opts = append(opts, expr.EnableBuiltin(n))
	}
	for _, n := range scalarBuiltins {
		opts = append(opts, expr.EnableBuiltin(n))
	}
	return expr.Compile(source, opts...)
}

func integer(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	case json.Number:
		x, e := n.Int64()
		return int(x), e == nil
	}
	return 0, false
}

func endpoint(v any, last bool) (any, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
		return nil, fmt.Errorf("collection required")
	}
	if rv.Len() == 0 {
		return nil, nil
	}
	n := 0
	if last {
		n = rv.Len() - 1
	}
	return rv.Index(n).Interface(), nil
}

// maxUnixSeconds is 9999-12-31T23:59:59Z, the last instant a four-digit-year timestamp - the only
// kind a widget document accepts - can name.
const maxUnixSeconds = 253402300799

// fromUnix turns Unix seconds, as Prometheus and most metrics APIs send them, into the RFC 3339
// text a series point needs. A fraction is kept to the millisecond: finer would only carry float
// noise, and coarser could collapse two sub-second samples into one timestamp the series
// validator then refuses as not increasing.
func fromUnix(v any) (string, error) {
	var f float64
	switch n := v.(type) {
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	case float64:
		f = n
	case json.Number:
		x, err := n.Float64()
		if err != nil {
			return "", fmt.Errorf("fromUnix: %q is not a number", n)
		}
		f = x
	default:
		return "", fmt.Errorf("fromUnix requires a number of seconds, got %T", v)
	}
	if math.IsNaN(f) || f < 0 || f > maxUnixSeconds {
		return "", fmt.Errorf("fromUnix: %v is not a timestamp between 1970 and 9999", f)
	}
	return time.UnixMilli(int64(math.Round(f * 1000))).UTC().Format(time.RFC3339Nano), nil
}

// unix is fromUnix's inverse for request values: whole Unix seconds from now(), a time derived
// from it (now() - duration("24h")), or an RFC 3339 string read from an upstream.
func unix(v any) (int, error) {
	switch t := v.(type) {
	case time.Time:
		return int(t.Unix()), nil
	case string:
		x, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			return 0, fmt.Errorf("unix: %q is not an RFC 3339 timestamp", t)
		}
		return int(x.Unix()), nil
	default:
		return 0, fmt.Errorf("unix requires a time or an RFC 3339 string, got %T", v)
	}
}

func run(ctx context.Context, p *vm.Program, env map[string]any, b *budget) (any, error) {
	env["__ctx"] = context.WithValue(ctx, budgetKey{}, b)
	return expr.Run(p, env)
}
