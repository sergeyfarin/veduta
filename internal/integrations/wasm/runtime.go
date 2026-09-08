// SPDX-License-Identifier: AGPL-3.0-or-later

// Package wasm runs approved Extism modules in isolated, bounded wazero instances.
package wasm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/widgets"
)

// MaxModuleBytes bounds the read before hashing or compiling untrusted code.
const MaxModuleBytes = 32 << 20

// Runtime owns a compilation cache and the instances loaded through it. Close it
// after the scheduler stops. A loaded instance owns compiled code, never guest state.
type Runtime struct {
	mu        sync.RWMutex
	cache     wazero.CompilationCache
	broker    capabilities.Broker
	instances []*instance
	closed    bool
}

// New opens a sandbox without broker host access. It is useful for validation and
// G1 conformance checks. Production integrations use NewWithBroker.
func New(ctx context.Context, cacheDir string) (*Runtime, error) {
	return openRuntime(ctx, cacheDir, nil)
}

// NewWithBroker opens a sandbox whose host functions delegate to broker.
func NewWithBroker(ctx context.Context, cacheDir string, broker capabilities.Broker) (*Runtime, error) {
	if broker == nil {
		return nil, errors.New("wasm: capability broker is required")
	}
	return openRuntime(ctx, cacheDir, broker)
}

func openRuntime(ctx context.Context, cacheDir string, broker capabilities.Broker) (*Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cache wazero.CompilationCache
	if cacheDir != "" {
		var err error
		cache, err = wazero.NewCompilationCacheWithDir(cacheDir)
		if err != nil {
			return nil, err
		}
	} else {
		cache = wazero.NewCompilationCache()
	}
	return &Runtime{cache: cache, broker: broker}, nil
}

// Name identifies the manifest runtime.
func (*Runtime) Name() string { return "wasm" }

// Close waits for bounded invocations, releases compiled modules, then closes the cache.
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	for _, i := range r.instances {
		errs = append(errs, i.Close(ctx))
	}
	r.instances = nil
	errs = append(errs, r.cache.Close(ctx))
	return errors.Join(errs...)
}

type operation struct {
	spec     integrations.OperationSpec
	schema   *jsonschema.Schema
	defaults map[string]any
}

type instance struct {
	mu         sync.RWMutex
	compiled   *extism.CompiledPlugin
	operations []operation
	limits     integrations.EffectiveLimits
	broker     capabilities.Broker
	closed     bool
}

// Load verifies both approval pins before compilation. Opening through os.Root
// confines relative paths and symlinks to the installed manifest's directory.
func (r *Runtime) Load(ctx context.Context, p integrations.Installed) (integrations.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("wasm: runtime closed")
	}
	m, lock := p.Manifest, p.Lock
	if m == nil || lock == nil || m.Runtime != "wasm" || lock.Runtime != "wasm" ||
		m.Digest == "" || lock.ManifestSHA256 != m.Digest || lock.ModuleSHA256 != m.ModuleSHA256 ||
		lock.Version != m.Version {
		return nil, errors.New("wasm: manifest/module is not approved")
	}
	expected, err := hex.DecodeString(m.ModuleSHA256)
	if err != nil || len(expected) != sha256.Size {
		return nil, errors.New("wasm: invalid module sha256")
	}
	l := lock.EffectiveLimits
	if l.MemoryMB < 1 || l.MemoryMB > 256 || l.TimeoutMs < 1 || l.TimeoutMs > 30000 ||
		l.OutputKB < 1 || l.OutputKB > 256 || l.InputMB < 1 || l.InputMB > 16 {
		return nil, errors.New("wasm: invalid approved resource limits")
	}
	// The manifest is also a ceiling, even for a caller constructing Installed directly.
	ml := m.Limits.WithDefaults()
	l.MemoryMB = min(l.MemoryMB, ml.MemoryMB)
	l.TimeoutMs = min(l.TimeoutMs, ml.TimeoutMs)
	l.OutputKB = min(l.OutputKB, ml.OutputKB)
	l.InputMB = min(l.InputMB, ml.InputMB)
	if l.MemoryMB < 1 || l.TimeoutMs < 1 || l.OutputKB < 1 || l.InputMB < 1 {
		return nil, errors.New("wasm: invalid manifest resource limits")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(l.TimeoutMs)*time.Millisecond)
	defer cancel()
	if !filepath.IsLocal(m.Module) {
		return nil, errors.New("wasm: module must be a local relative path")
	}
	root, err := os.OpenRoot(filepath.Dir(m.Path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(m.Module)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, errors.New("wasm: module must be a regular file")
	}
	code, err := io.ReadAll(io.LimitReader(f, MaxModuleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(code) > MaxModuleBytes {
		return nil, errors.New("wasm: module exceeds byte limit")
	}
	digest := sha256.Sum256(code)
	if !bytes.Equal(digest[:], expected) {
		return nil, errors.New("wasm: module sha256 mismatch")
	}
	in := &instance{limits: l, broker: r.broker}
	for _, op := range m.Operations {
		o := operation{spec: integrations.OperationSpec{ID: op.ID, Name: op.Name, DefaultRefresh: op.DefaultRefresh, Signals: slices.Clone(op.Signals)}}
		if op.Params != nil {
			body, marshalErr := json.Marshal(op.Params)
			if marshalErr != nil {
				return nil, marshalErr
			}
			doc, decodeErr := jsonschema.UnmarshalJSON(bytes.NewReader(body))
			if decodeErr != nil {
				return nil, decodeErr
			}
			c := jsonschema.NewCompiler()
			if err = c.AddResource("urn:veduta:params", doc); err != nil {
				return nil, err
			}
			o.schema, err = c.Compile("urn:veduta:params")
			if err != nil {
				return nil, err
			}
			o.defaults, _ = doc.(map[string]any)
		}
		in.operations = append(in.operations, o)
	}
	config := wazero.NewRuntimeConfig().WithCompilationCache(r.cache).
		WithMemoryLimitPages(uint32(l.MemoryMB) * 16).WithCloseOnContextDone(true)
	// Preflight imports and the ABI without executing a start function. Reusing the
	// cache lets Extism compile the same verified bytes without recompiling machine code.
	if err = preflight(ctx, code, config); err != nil {
		return nil, err
	}
	ctx = experimental.WithFunctionListenerFactory(ctx, outputLimiter{})
	in.compiled, err = extism.NewCompiledPlugin(ctx, extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmData{Data: code}},
		AllowedHosts: []string{},
		Memory:       &extism.ManifestMemory{MaxPages: uint32(l.MemoryMB) * 16, MaxVarBytes: 0},
	}, extism.PluginConfig{RuntimeConfig: config, EnableWasi: false}, hostFunctions())
	if err != nil {
		return nil, fmt.Errorf("wasm: compile: %w", err)
	}
	r.instances = append(r.instances, in)
	return in, nil
}

func preflight(ctx context.Context, code []byte, config wazero.RuntimeConfig) error {
	rt := wazero.NewRuntimeWithConfig(ctx, config)
	defer func() { _ = rt.Close(context.Background()) }()
	m, err := rt.CompileModule(ctx, code)
	if err != nil {
		return fmt.Errorf("wasm: compile: %w", err)
	}
	// Only Extism's memory/input/output ABI is available in G1. In particular,
	// native HTTP, WASI, config, variables and native logging cannot bypass G2's broker.
	allowed := map[string]bool{"alloc": true, "free": true, "length": true, "length_unsafe": true,
		"load_u8": true, "load_u64": true, "store_u8": true, "store_u64": true,
		"input_length": true, "input_load_u8": true, "input_load_u64": true,
		"output_set": true, "error_set": true}
	var forbidden []string
	for _, f := range m.ImportedFunctions() {
		module, name, _ := f.Import()
		if module == hostNamespace && hostFunctionNames[name] {
			continue
		}
		if module != "extism:host/env" || !allowed[name] {
			forbidden = append(forbidden, module+"."+name)
		}
	}
	if len(forbidden) > 0 {
		sort.Strings(forbidden)
		return fmt.Errorf("wasm: forbidden imports: %s", strings.Join(forbidden, ", "))
	}
	if len(m.ImportedMemories()) != 0 {
		return errors.New("wasm: imported memory is forbidden")
	}
	f := m.ExportedFunctions()["invoke"]
	if f == nil || len(f.ParamTypes()) != 0 || len(f.ResultTypes()) != 1 || f.ResultTypes()[0] != api.ValueTypeI32 {
		return errors.New("wasm: requires invoke() -> i32 export")
	}
	return nil
}

// Operations returns detached operation specifications.
func (i *instance) Operations() []integrations.OperationSpec {
	out := make([]integrations.OperationSpec, len(i.operations))
	for n, o := range i.operations {
		out[n] = o.spec
		out[n].Signals = slices.Clone(o.spec.Signals)
	}
	return out
}

// Close waits for active invocations and releases this instance's compiled code.
func (i *instance) Close(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	return i.compiled.Close(ctx)
}

// Invoke validates input, runs a fresh guest, and validates its Widget Document.
func (i *instance) Invoke(ctx context.Context, req integrations.InvokeRequest) (integrations.InvokeResponse, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	var empty integrations.InvokeResponse
	if i.closed {
		return empty, errors.New("wasm: instance closed")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(i.limits.TimeoutMs)*time.Millisecond)
	defer cancel()
	if !req.Deadline.IsZero() {
		var cancelDeadline context.CancelFunc
		ctx, cancelDeadline = context.WithDeadline(ctx, req.Deadline)
		defer cancelDeadline()
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var op *operation
	for n := range i.operations {
		if i.operations[n].spec.ID == req.Operation {
			op = &i.operations[n]
			break
		}
	}
	if op == nil {
		return empty, fmt.Errorf("wasm: unknown operation %q", req.Operation)
	}
	if len(req.Params) > i.limits.InputMB<<20 {
		return empty, errors.New("wasm: input exceeds byte limit")
	}
	params := map[string]any{}
	if len(req.Params) != 0 {
		if !json.Valid(req.Params) {
			return empty, errors.New("wasm: invalid params JSON")
		}
		d := json.NewDecoder(bytes.NewReader(req.Params))
		d.UseNumber()
		if err := d.Decode(&params); err != nil {
			return empty, err
		}
		if params == nil {
			return empty, errors.New("wasm: params must be an object")
		}
	}
	applyDefaults(op.defaults, params)
	if op.schema != nil {
		if err := op.schema.Validate(params); err != nil {
			return empty, fmt.Errorf("wasm: params: %w", err)
		}
	}
	input, err := json.Marshal(struct {
		Operation string         `json:"operation"`
		Params    map[string]any `json:"params"`
		Now       time.Time      `json:"now"`
	}{req.Operation, params, time.Now().UTC()})
	if err != nil {
		return empty, err
	}
	if len(input) > i.limits.InputMB<<20 {
		return empty, errors.New("wasm: input exceeds byte limit")
	}
	guest, err := i.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig()})
	if err != nil {
		return empty, fmt.Errorf("wasm: instantiate: %w", err)
	}
	defer func() { _ = guest.Close(context.Background()) }()
	guest.SetLogger(func(extism.LogLevel, string) {})
	// #nosec G115 -- Load requires OutputKB in [1, 256]; limits are private and immutable.
	ctx = context.WithValue(ctx, outputLimitKey{}, uint64(i.limits.OutputKB)<<10)
	ctx = context.WithValue(ctx, invocationKey{}, invocationContext{broker: i.broker, grant: req.Grant})
	status, output, err := guest.CallWithContext(ctx, "invoke", input)
	if err != nil {
		return empty, fmt.Errorf("wasm: invoke: %w", err)
	}
	if status != 0 {
		return empty, fmt.Errorf("wasm: plugin returned status %d", status)
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	doc, err := widgets.ValidateWithLimit(output, i.limits.OutputKB<<10)
	if err != nil {
		return empty, err
	}
	declared := make(map[string]string, len(op.spec.Signals))
	for _, s := range op.spec.Signals {
		declared[s.Name] = s.Type
	}
	for name, signal := range doc.Signals {
		if !signalType(signal.Value, declared[name]) {
			return empty, fmt.Errorf("wasm: signal %s is undeclared or has the wrong type", name)
		}
	}
	return integrations.InvokeResponse{Document: doc}, nil
}

func applyDefaults(schema map[string]any, params map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	for name, value := range props {
		rule, _ := value.(map[string]any)
		if _, exists := params[name]; !exists {
			if d, ok := rule["default"]; ok {
				// Copy composite defaults so concurrent calls never share mutable maps.
				raw, _ := json.Marshal(d)
				var copyValue any
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				_ = decoder.Decode(&copyValue)
				params[name] = copyValue
			}
		}
		if child, ok := params[name].(map[string]any); ok {
			applyDefaults(rule, child)
		}
	}
}

func signalType(value any, want string) bool {
	switch want {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		switch value.(type) {
		case float64, json.Number:
			return true
		}
	}
	return false
}

var _ integrations.Runtime = (*Runtime)(nil)
var _ integrations.Instance = (*instance)(nil)

// Check the full i64 length before Extism narrows it or copies guest output into
// Go memory. Read the limit from the invocation, never from cached compiled code.
type outputLimitKey struct{}
type outputLimiter struct{}

// NewFunctionListener guards the host output function before the SDK sees it.
func (outputLimiter) NewFunctionListener(f api.FunctionDefinition) experimental.FunctionListener {
	if f.ModuleName() != "extism:host/env" || !slices.Contains(f.ExportNames(), "output_set") {
		return nil
	}
	return experimental.FunctionListenerFunc(func(ctx context.Context, _ api.Module, _ api.FunctionDefinition, params []uint64, _ experimental.StackIterator) {
		limit, ok := ctx.Value(outputLimitKey{}).(uint64)
		if !ok || params[1] > limit {
			panic("wasm: output exceeds byte limit")
		}
	})
}
