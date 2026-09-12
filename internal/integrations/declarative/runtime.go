// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr/vm"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
	"veduta.dev/veduta/internal/widgets"
)

// Runtime is the declarative implementation of integrations.Runtime.
type Runtime struct{ Broker capabilities.Broker }

// New constructs a declarative runtime around the sole host-capability broker.
func New(b capabilities.Broker) *Runtime { return &Runtime{Broker: b} }

// Name identifies this runtime in manifests.
func (*Runtime) Name() string { return "declarative" }

// Close releases runtime-wide resources; the declarative runtime owns none.
func (*Runtime) Close(context.Context) error { return nil }

type instance struct {
	broker   capabilities.Broker
	manifest *manifestload.Manifest
	programs map[*manifestload.Expression]*vm.Program
	params   map[string]*jsonschema.Schema
}

// Load verifies approval and compiles every expression once.
func (r *Runtime) Load(_ context.Context, p integrations.Installed) (integrations.Instance, error) {
	if p.Manifest == nil || p.Lock == nil {
		return nil, errors.New("declarative: manifest is not approved")
	}
	if p.Manifest.Runtime != "declarative" {
		return nil, fmt.Errorf("declarative: runtime is %q", p.Manifest.Runtime)
	}
	if p.Lock.ManifestSHA256 != p.Manifest.Digest {
		return nil, errors.New("declarative: approved manifest digest does not match")
	}
	approved := *p.Manifest
	e := p.Lock.EffectiveLimits
	approved.Limits = manifestload.Limits{MemoryMB: e.MemoryMB, TimeoutMs: e.TimeoutMs, OutputKB: e.OutputKB, HTTPRequests: e.HTTPRequests, ResponseMB: e.ResponseMB, CacheEntries: e.CacheEntries, InputMB: e.InputMB, JSONDepth: e.JSONDepth, JSONNodes: e.JSONNodes, ExprNodes: e.ExprNodes, Iterations: e.Iterations, RequestBodyKB: e.RequestBodyKB, HostCalls: e.HostCalls, CacheBytesKB: e.CacheBytesKB}.WithDefaults()
	in := &instance{r.Broker, &approved, map[*manifestload.Expression]*vm.Program{}, map[string]*jsonschema.Schema{}}
	for _, op := range p.Manifest.Operations {
		if err := in.compileTemplate(op.Output); err != nil {
			return nil, err
		}
		for _, s := range op.Pipeline {
			if s.When != nil {
				if err := in.compileExpression(s.When); err != nil {
					return nil, err
				}
			}
			if err := in.compileTemplate(s.Request.Path); err != nil {
				return nil, err
			}
			for _, t := range s.Request.Query {
				if err := in.compileTemplate(t); err != nil {
					return nil, err
				}
			}
			for _, t := range s.Request.Headers {
				if err := in.compileTemplate(t); err != nil {
					return nil, err
				}
			}
			if s.Request.Body != nil {
				if err := in.compileTemplate(s.Request.Body.JSON); err != nil {
					return nil, err
				}
				for _, t := range s.Request.Body.Form {
					if err := in.compileTemplate(t); err != nil {
						return nil, err
					}
				}
			}
		}
		if op.Params != nil {
			body, _ := json.Marshal(op.Params)
			doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(body))
			if e != nil {
				return nil, e
			}
			c := jsonschema.NewCompiler()
			id := "urn:veduta:params:" + op.ID
			if e = c.AddResource(id, doc); e != nil {
				return nil, e
			}
			in.params[op.ID], e = c.Compile(id)
			if e != nil {
				return nil, e
			}
		}
	}
	return in, nil
}
func (i *instance) compileExpression(e *manifestload.Expression) error {
	p, err := compile(e.Source, i.manifest.Limits.ExprNodes)
	if err != nil {
		return fmt.Errorf("%s:%d:%d: %w", i.manifest.Path, e.Line, e.Column, err)
	}
	i.programs[e] = p
	return nil
}
func (i *instance) compileTemplate(t *manifestload.Template) error {
	if t == nil {
		return nil
	}
	if t.Expr != nil {
		if e := i.compileExpression(t.Expr); e != nil {
			return e
		}
	}
	if t.Asset != nil {
		if e := i.compileTemplate(t.Asset.Path); e != nil {
			return e
		}
		for _, x := range t.Asset.Query {
			if e := i.compileTemplate(x); e != nil {
				return e
			}
		}
	}
	if t.Each != nil {
		if e := i.compileExpression(t.Each.Expr); e != nil {
			return e
		}
		return i.compileTemplate(t.Each.Item)
	}
	if t.Cond != nil {
		if e := i.compileExpression(t.Cond.If); e != nil {
			return e
		}
		return i.compileTemplate(t.Cond.Then)
	}
	for _, x := range t.Object {
		if e := i.compileTemplate(x); e != nil {
			return e
		}
	}
	for _, x := range t.Array {
		if e := i.compileTemplate(x); e != nil {
			return e
		}
	}
	return nil
}

// Operations returns the immutable operation surface.
func (i *instance) Operations() []integrations.OperationSpec {
	out := make([]integrations.OperationSpec, 0, len(i.manifest.Operations))
	for _, o := range i.manifest.Operations {
		out = append(out, integrations.OperationSpec{ID: o.ID, Name: o.Name, DefaultRefresh: o.DefaultRefresh, Signals: o.Signals})
	}
	return out
}

// Close releases runtime resources; declarative instances own none.
func (i *instance) Close(context.Context) error { return nil }

// Invoke evaluates one operation inside its shared resource budget.
func (i *instance) Invoke(ctx context.Context, req integrations.InvokeRequest) (integrations.InvokeResponse, error) {
	var op *manifestload.OperationDef
	for x := range i.manifest.Operations {
		if i.manifest.Operations[x].ID == req.Operation {
			op = &i.manifest.Operations[x]
			break
		}
	}
	if op == nil {
		return integrations.InvokeResponse{}, fmt.Errorf("declarative: unknown operation %q", req.Operation)
	}
	params := map[string]any{}
	if len(req.Params) > 0 {
		d := json.NewDecoder(bytes.NewReader(req.Params))
		d.UseNumber()
		if e := d.Decode(&params); e != nil {
			return integrations.InvokeResponse{}, fmt.Errorf("params: %w", e)
		}
	}
	applyParamDefaults(op.Params, params)
	if s := i.params[op.ID]; s != nil {
		if e := s.Validate(params); e != nil {
			return integrations.InvokeResponse{}, fmt.Errorf("params: %w", e)
		}
	}
	// The manifest's own timeoutMs is a ceiling a caller-supplied deadline may only narrow, never
	// replace with something later - found in review: this used to let any non-zero
	// req.Deadline override the approved timeoutMs outright, the wrong direction for an
	// "effective limit" (docs/01-architecture.md's own effective(k) = min(...), never max). The
	// resulting deadline is then actually applied to ctx (previously computed but never attached
	// to anything - a three-second-approved integration could still block for the connection's
	// own ten-second HTTP client timeout), so every broker call and expression evaluation below
	// shares one real, enforced deadline instead of the budget's wall-clock check being the only
	// thing that ever looked at it.
	deadline := time.Now().Add(time.Duration(i.manifest.Limits.TimeoutMs) * time.Millisecond)
	if !req.Deadline.IsZero() && req.Deadline.Before(deadline) {
		deadline = req.Deadline
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithDeadline(ctx, deadline)
	defer cancel()
	b := &budget{ctx: ctx, deadline: deadline, iterations: i.manifest.Limits.Iterations, maxBytes: i.manifest.Limits.InputMB << 20, maxNodes: i.manifest.Limits.JSONNodes}
	env := map[string]any{"params": params, "now": time.Now().UTC(), "__grant": req.Grant}
	for _, step := range op.Pipeline {
		if e := b.check(); e != nil {
			return integrations.InvokeResponse{}, e
		}
		if step.When != nil {
			v, e := run(ctx, i.programs[step.When], env, b)
			if e != nil {
				return integrations.InvokeResponse{}, e
			}
			ok, _ := v.(bool)
			if !ok {
				continue
			}
		}
		path, e := i.evalString(ctx, step.Request.Path, env, b)
		if e != nil {
			return integrations.InvokeResponse{}, e
		}
		q, e := i.evalStrings(ctx, step.Request.Query, env, b)
		if e != nil {
			return integrations.InvokeResponse{}, e
		}
		h, e := i.evalStrings(ctx, step.Request.Headers, env, b)
		if e != nil {
			return integrations.InvokeResponse{}, e
		}
		var body []byte
		if step.Request.Body != nil {
			if step.Request.Body.JSON != nil {
				v, x := i.eval(ctx, step.Request.Body.JSON, env, b)
				if x != nil {
					return integrations.InvokeResponse{}, x
				}
				body, x = json.Marshal(v)
				if x != nil {
					return integrations.InvokeResponse{}, x
				}
				h["Content-Type"] = "application/json"
			} else {
				form, x := i.evalStrings(ctx, step.Request.Body.Form, env, b)
				if x != nil {
					return integrations.InvokeResponse{}, x
				}
				vals := url.Values{}
				for k, v := range form {
					vals.Set(k, v)
				}
				body = []byte(vals.Encode())
				h["Content-Type"] = "application/x-www-form-urlencoded"
			}
		}
		resp, e := i.broker.HTTP(ctx, req.Grant, capabilities.HTTPRequest{Slot: step.Request.Slot, Method: step.Request.Method, Path: path, Query: q, Header: h, Body: body})
		if e != nil {
			return integrations.InvokeResponse{}, e
		}
		// An upstream error is an error, not data. Without this the body was decoded regardless of
		// status, so a 401, 403 or 500 whose body happens to be JSON was folded into the document
		// as though it were the answer, and one whose body is HTML surfaced as a JSON decode
		// failure naming the step rather than the status. The wasm side has always rejected
		// non-2xx explicitly; this makes the two runtimes agree.
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return integrations.InvokeResponse{}, fmt.Errorf("pipeline %s: %s %s returned HTTP %d", step.As, step.Request.Method, path, resp.StatusCode)
		}
		decoded, e := decodeJSON(resp.Body, b, i.manifest.Limits.JSONDepth)
		if e != nil {
			return integrations.InvokeResponse{}, fmt.Errorf("pipeline %s: %w", step.As, e)
		}
		env[step.As] = decoded
	}
	rawDoc, e := i.eval(ctx, op.Output, env, b)
	if e != nil {
		return integrations.InvokeResponse{}, e
	}
	if object, ok := rawDoc.(map[string]any); ok {
		object["schemaVersion"] = 1
	}
	body, e := json.Marshal(rawDoc)
	if e != nil {
		return integrations.InvokeResponse{}, e
	}
	// The approved outputKB, not the package's own hardcoded default - found missing in review:
	// EffectiveLimits.OutputKB was reconciled and carried this far but nothing downstream ever
	// read it, so a narrower or wider approval than the 64 KiB core default had no effect either
	// way.
	doc, e := widgets.ValidateWithLimit(body, i.manifest.Limits.OutputKB<<10)
	if e != nil {
		return integrations.InvokeResponse{}, e
	}
	declared := make(map[string]bool, len(op.Signals))
	for _, s := range op.Signals {
		declared[s.Name] = true
		sig, ok := doc.Signals[s.Name]
		if !ok {
			continue
		}
		if !signalType(sig.Value, s.Type) {
			return integrations.InvokeResponse{}, fmt.Errorf("signal %s is not %s", s.Name, s.Type)
		}
	}
	// manifestload's own load-time check already rejects any signals key not in op.Signals - a
	// YAML mapping's keys are always static (there is no construct in this grammar that produces
	// one at evaluation time), so this can only fire if that invariant is ever broken by a future
	// grammar change. Kept anyway as a second, independent check on the *evaluated* map, raised in
	// review: cheap, and it means adding a dynamic-key construct later fails loudly here rather
	// than silently reintroducing an undeclared-signal hole.
	for name := range doc.Signals {
		if !declared[name] {
			return integrations.InvokeResponse{}, fmt.Errorf("signal %s is not declared", name)
		}
	}
	return integrations.InvokeResponse{Document: doc}, nil
}

func applyParamDefaults(schema any, value map[string]any) {
	s, ok := schema.(map[string]any)
	if !ok {
		return
	}
	props, ok := s["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, p := range props {
		rule, ok := p.(map[string]any)
		if !ok {
			continue
		}
		current, exists := value[name]
		if !exists {
			if d, ok := rule["default"]; ok {
				value[name] = d
				current = d
				exists = true
			}
		}
		if exists {
			if child, ok := current.(map[string]any); ok {
				applyParamDefaults(rule, child)
			}
		}
	}
}

// omitted is what a false `if` node evaluates to: the enclosing object key or array element is
// dropped rather than set to null. It is a private sentinel, so no expression, upstream response
// or literal can produce a value equal to it, and anywhere other than an object or array it
// survives to the document and is rejected there rather than silently meaning something.
var omitted = &struct{ name string }{"omitted"}

func (i *instance) eval(ctx context.Context, t *manifestload.Template, env map[string]any, b *budget) (any, error) {
	if t == nil {
		return nil, nil
	}
	if e := b.template(); e != nil {
		return nil, e
	}
	switch t.Kind {
	case "literal":
		return t.Literal, nil
	case "expr":
		return run(ctx, i.programs[t.Expr], env, b)
	case "array":
		out := make([]any, 0, len(t.Array))
		for _, x := range t.Array {
			v, e := i.eval(ctx, x, env, b)
			if e != nil {
				return nil, e
			}
			if v == omitted {
				continue
			}
			out = append(out, v)
		}
		return out, nil
	case "object":
		out := map[string]any{}
		for k, x := range t.Object {
			v, e := i.eval(ctx, x, env, b)
			if e != nil {
				return nil, e
			}
			if v == omitted {
				continue
			}
			out[k] = v
		}
		return out, nil
	case "cond":
		v, e := run(ctx, i.programs[t.Cond.If], env, b)
		if e != nil {
			return nil, e
		}
		keep, ok := v.(bool)
		if !ok {
			// Deliberately not truthiness: `if: {expr: item.name}` silently keeping every
			// non-empty name is the kind of near-miss a closed grammar should refuse outright.
			return nil, fmt.Errorf("if condition evaluated to %T, want a boolean", v)
		}
		if !keep {
			return omitted, nil
		}
		return i.eval(ctx, t.Cond.Then, env, b)
	case "each":
		v, e := run(ctx, i.programs[t.Each.Expr], env, b)
		if e != nil {
			return nil, e
		}
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
			return nil, errors.New("each expression did not return a collection")
		}
		out := make([]any, 0, rv.Len())
		for n := 0; n < rv.Len(); n++ {
			old, had := env[t.Each.As]
			env[t.Each.As] = rv.Index(n).Interface()
			x, e := i.eval(ctx, t.Each.Item, env, b)
			if had {
				env[t.Each.As] = old
			} else {
				delete(env, t.Each.As)
			}
			if e != nil {
				return nil, e
			}
			if x == omitted {
				continue
			}
			out = append(out, x)
		}
		return out, nil
	case "asset":
		path, e := i.evalString(ctx, t.Asset.Path, env, b)
		if e != nil {
			return nil, e
		}
		q, e := i.evalStrings(ctx, t.Asset.Query, env, b)
		if e != nil {
			return nil, e
		}
		vals := url.Values{}
		for k, v := range q {
			vals.Set(k, v)
		}
		tr := capabilities.Transform{}
		for _, part := range strings.Split(t.Asset.Transform, ",") {
			if strings.HasPrefix(part, "w=") {
				tr.Width, _ = strconv.Atoi(strings.TrimPrefix(part, "w="))
			}
			if strings.HasPrefix(part, "f=") {
				tr.Format = strings.TrimPrefix(part, "f=")
			}
		}
		ref, e := i.broker.AssetRef(ctx, envGrant(env), t.Asset.Slot, path, vals, tr)
		if e != nil {
			return nil, e
		}
		// A value, not a fragment of one. plugin-manifest.v1 already types an asset node as a
		// valueNode alongside literals and {expr}, and returning {"ref": …} contradicted that:
		// it forced the node to be an entire `image`, which made alt text and aspect ratio
		// unreachable for every declarative integration. Write `ref: { asset: … }` instead.
		return ref, nil
	}
	return nil, fmt.Errorf("unknown template kind %q", t.Kind)
}
func envGrant(env map[string]any) capabilities.Grant {
	g, _ := env["__grant"].(capabilities.Grant)
	return g
}
func (i *instance) evalString(ctx context.Context, t *manifestload.Template, env map[string]any, b *budget) (string, error) {
	v, e := i.eval(ctx, t, env, b)
	if e != nil {
		return "", e
	}
	if v == omitted {
		return "", errors.New("an if node cannot omit a request path, query value, header or asset path - use expr's ?: for a fallback value")
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("template value is %T, want string", v)
	}
	return s, nil
}
func (i *instance) evalStrings(ctx context.Context, m map[string]*manifestload.Template, env map[string]any, b *budget) (map[string]string, error) {
	out := map[string]string{}
	for k, t := range m {
		s, e := i.evalString(ctx, t, env, b)
		if e != nil {
			return nil, e
		}
		out[k] = s
	}
	return out, nil
}
func signalType(v any, want string) bool {
	switch want {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		switch v.(type) {
		case float64, float32, int, int64, json.Number:
			return true
		}
	}
	return false
}
