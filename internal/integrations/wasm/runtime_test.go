// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/leb128"
	w "github.com/tetratelabs/wabin/wasm"

	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

const goodDocument = `{"schemaVersion":1,"blocks":[{"type":"text","content":"hello"}]}`

// Build actual Wasm binaries in Go: conformance tests require no external guest
// compiler, downloaded fixtures, or opaque checked-in executables.
func module(body []byte) *w.Module {
	return &w.Module{
		TypeSection: []*w.FunctionType{
			{Params: []byte{w.ValueTypeI64}, Results: []byte{w.ValueTypeI64}},
			{Params: []byte{w.ValueTypeI64, w.ValueTypeI32}},
			{Params: []byte{w.ValueTypeI64, w.ValueTypeI64}},
			{Results: []byte{w.ValueTypeI32}},
		},
		ImportSection: []*w.Import{
			{Type: w.ExternTypeFunc, Module: "extism:host/env", Name: "alloc", DescFunc: 0},
			{Type: w.ExternTypeFunc, Module: "extism:host/env", Name: "store_u8", DescFunc: 1},
			{Type: w.ExternTypeFunc, Module: "extism:host/env", Name: "output_set", DescFunc: 2},
		},
		FunctionSection: []uint32{3},
		ExportSection:   []*w.Export{{Type: w.ExternTypeFunc, Name: "invoke", Index: 3}},
		CodeSection:     []*w.Code{{LocalTypes: []byte{w.ValueTypeI64}, Body: body}},
	}
}
func const64(v int64) []byte { return append([]byte{w.OpcodeI64Const}, leb128.EncodeInt64(v)...) }
func const32(v int32) []byte { return append([]byte{w.OpcodeI32Const}, leb128.EncodeInt32(v)...) }
func outputBody(document string, size int64) []byte {
	body := append(const64(size), w.OpcodeCall, 0, w.OpcodeLocalSet, 0)
	for n, b := range []byte(document) {
		body = append(body, w.OpcodeLocalGet, 0)
		body = append(body, const64(int64(n))...)
		body = append(body, w.OpcodeI64Add)
		body = append(body, const32(int32(b))...)
		body = append(body, w.OpcodeCall, 1)
	}
	body = append(body, w.OpcodeLocalGet, 0)
	body = append(body, const64(size)...)
	body = append(body, w.OpcodeCall, 2, w.OpcodeI32Const, 0, w.OpcodeEnd)
	return body
}
func goodModule() *w.Module { return module(outputBody(goodDocument, int64(len(goodDocument)))) }

func installed(t *testing.T, code []byte) integrations.Installed {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), code, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(code)
	digest := hex.EncodeToString(sum[:])
	return integrations.Installed{
		Manifest: &manifestload.Manifest{Path: filepath.Join(dir, "manifest.yaml"), Digest: "approved", ID: "test", Version: "1.0.0", Runtime: "wasm", Module: "plugin.wasm", ModuleSHA256: digest,
			Operations: []manifestload.OperationDef{{ID: "stats"}}},
		Lock: &integrations.LockEntry{ManifestSHA256: "approved", ModuleSHA256: digest, Version: "1.0.0", Runtime: "wasm",
			EffectiveLimits: integrations.EffectiveLimits{MemoryMB: 64, TimeoutMs: 3000, OutputKB: 64, InputMB: 4}},
	}
}
func newRuntime(t *testing.T, dir string) *Runtime {
	t.Helper()
	r, err := New(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return r
}
func load(t *testing.T, r *Runtime, p integrations.Installed) integrations.Instance {
	t.Helper()
	i, err := r.Load(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return i
}
func invoke(i integrations.Instance) error {
	_, err := i.Invoke(context.Background(), integrations.InvokeRequest{Operation: "stats"})
	return err
}

func TestRoundTripIsolationAndLifecycle(t *testing.T) {
	r := newRuntime(t, t.TempDir())
	m := goodModule()
	// A mutable global traps if the same guest is used a second time.
	m.GlobalSection = []*w.Global{{Type: &w.GlobalType{ValType: w.ValueTypeI32, Mutable: true}, Init: &w.ConstantExpression{Opcode: w.OpcodeI32Const, Data: []byte{0}}}}
	prefix := []byte{w.OpcodeGlobalGet, 0, w.OpcodeIf, 0x40, w.OpcodeUnreachable, w.OpcodeEnd, w.OpcodeI32Const, 1, w.OpcodeGlobalSet, 0}
	m.CodeSection[0].Body = append(prefix, m.CodeSection[0].Body...)
	p := installed(t, binary.EncodeModule(m))
	i := load(t, r, p)
	// Mutating caller-owned operations must not alter the loaded instance.
	p.Manifest.Operations[0].ID = "mutated"
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Go(func() {
			if err := invoke(i); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := i.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := invoke(i); err == nil {
		t.Fatal("closed instance invoked")
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Load(context.Background(), p); err == nil {
		t.Fatal("closed runtime loaded module")
	}
}

func TestSandboxForbiddenImports(t *testing.T) {
	for _, name := range []string{"path_open", "environ_get", "args_get", "sock_open", "sock_connect", "fd_write", "http_request", "extism_http_request", "var_set", "config_get", "log_info"} {
		t.Run(name, func(t *testing.T) {
			m := goodModule()
			namespace := "wasi_snapshot_preview1"
			if strings.Contains(name, "http") || name == "var_set" || name == "config_get" || name == "log_info" {
				namespace = "extism:host/env"
			}
			m.ImportSection[0].Module = namespace
			m.ImportSection[0].Name = name
			_, err := newRuntime(t, "").Load(context.Background(), installed(t, binary.EncodeModule(m)))
			if err == nil || !strings.Contains(err.Error(), "forbidden import") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestPinsPathsAndCorruption(t *testing.T) {
	for _, name := range []string{"corrupt", "manifest", "module", "unapproved", "traversal", "absolute", "symlink", "oversize", "malformed", "export"} {
		t.Run(name, func(t *testing.T) {
			r := newRuntime(t, "")
			p := installed(t, binary.EncodeModule(goodModule()))
			switch name {
			case "corrupt":
				os.WriteFile(filepath.Join(filepath.Dir(p.Manifest.Path), p.Manifest.Module), []byte("not wasm"), 0600)
			case "manifest":
				p.Lock.ManifestSHA256 = "other"
			case "module":
				p.Lock.ModuleSHA256 = strings.Repeat("0", 64)
			case "unapproved":
				p.Lock = nil
			case "traversal":
				p.Manifest.Module = "../../etc/passwd"
			case "absolute":
				p.Manifest.Module = "/etc/passwd"
			case "symlink":
				os.Symlink("/etc/passwd", filepath.Join(filepath.Dir(p.Manifest.Path), "escape.wasm"))
				p.Manifest.Module = "escape.wasm"
			case "oversize":
				f, err := os.OpenFile(filepath.Join(filepath.Dir(p.Manifest.Path), p.Manifest.Module), os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				f.Truncate(MaxModuleBytes + 1)
				f.Close()
			case "malformed":
				p = installed(t, []byte("not wasm"))
			case "export":
				m := goodModule()
				m.ExportSection[0].Name = "wrong"
				p = installed(t, binary.EncodeModule(m))
			}
			_, err := r.Load(context.Background(), p)
			if err == nil {
				t.Fatal("unsafe module accepted")
			}
			if name == "corrupt" && !strings.Contains(err.Error(), "sha256 mismatch") {
				t.Fatalf("corruption reached compiler: %v", err)
			}
		})
	}
}

func TestDeadlineAndRecovery(t *testing.T) {
	r := newRuntime(t, "")
	loop := module([]byte{w.OpcodeLoop, 0x40, w.OpcodeBr, 0, w.OpcodeEnd, w.OpcodeI32Const, 0, w.OpcodeEnd})
	p := installed(t, binary.EncodeModule(loop))
	i := load(t, r, p)
	start := time.Now()
	_, err := i.Invoke(context.Background(), integrations.InvokeRequest{Operation: "stats", Deadline: time.Now().Add(25 * time.Millisecond)})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("deadline did not stop guest: %v", err)
	}
	// Cancellation also kills start sections during instantiation.
	loop.TypeSection[3].Results = nil
	loop.CodeSection[0].Body = []byte{w.OpcodeLoop, 0x40, w.OpcodeBr, 0, w.OpcodeEnd, w.OpcodeEnd}
	loop.FunctionSection = append(loop.FunctionSection, 3)
	loop.TypeSection = append(loop.TypeSection, &w.FunctionType{Results: []byte{w.ValueTypeI32}})
	loop.FunctionSection[1] = 4
	loop.CodeSection = append(loop.CodeSection, &w.Code{Body: []byte{w.OpcodeI32Const, 0, w.OpcodeEnd}})
	loop.ExportSection[0].Index = 4
	index := uint32(3)
	loop.StartSection = &index
	i = load(t, r, installed(t, binary.EncodeModule(loop)))
	_, err = i.Invoke(context.Background(), integrations.InvokeRequest{Operation: "stats", Deadline: time.Now().Add(25 * time.Millisecond)})
	if err == nil {
		t.Fatal("start loop survived")
	}
	if err := invoke(load(t, r, installed(t, binary.EncodeModule(goodModule())))); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryAndOutputCaps(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
		want string
	}{
		{"output", 10 << 20, "output exceeds"}, {"i64-output", 1 << 32, "output exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := outputBody("", tc.size)
			if tc.name == "i64-output" {
				body = append(const64(0), const64(tc.size)...)
				body = append(body, w.OpcodeCall, 2, w.OpcodeI32Const, 0, w.OpcodeEnd)
			}
			i := load(t, newRuntime(t, ""), installed(t, binary.EncodeModule(module(body))))
			err := invoke(i)
			if err == nil || (tc.want != "" && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("cap not enforced: %v", err)
			}
		})
	}
	// A guest-owned memory is limited independently of the Extism kernel memory.
	m := goodModule()
	m.MemorySection = &w.Memory{Min: 1600}
	if _, err := newRuntime(t, "").Load(context.Background(), installed(t, binary.EncodeModule(m))); err == nil {
		t.Fatal("oversized initial memory accepted")
	}
}

func TestParamsDocumentsAndSignals(t *testing.T) {
	r := newRuntime(t, "")
	p := installed(t, binary.EncodeModule(goodModule()))
	p.Manifest.Operations[0].Params = map[string]any{"type": "object", "properties": map[string]any{"count": map[string]any{"type": "integer", "default": 3}}, "required": []string{"count"}, "additionalProperties": false}
	i := load(t, r, p)
	if err := invoke(i); err != nil {
		t.Fatal(err)
	}
	for _, params := range []string{`null`, `[]`, `{} {}`, `{"count":"bad"}`, `{"extra":1}`} {
		if _, err := i.Invoke(context.Background(), integrations.InvokeRequest{Operation: "stats", Params: json.RawMessage(params)}); err == nil {
			t.Fatalf("accepted %s", params)
		}
	}
	for _, doc := range []string{`{}`, `{"schemaVersion":1,"blocks":[{"type":"text","content":"ok"}],"signals":{"x":{"value":true}}}`} {
		p := installed(t, binary.EncodeModule(module(outputBody(doc, int64(len(doc))))))
		if err := invoke(load(t, r, p)); err == nil {
			t.Fatalf("accepted invalid document %s", doc)
		}
	}
}

func TestMemoryAllocationTrap(t *testing.T) {
	for _, pages := range []bool{false, true} {
		t.Run(map[bool]string{false: "extism-allocation", true: "guest-memory-grow"}[pages], func(t *testing.T) {
			m := goodModule()
			// Both alloc and memory.grow report allocation failure. A conventional guest
			// allocator traps on that result. Prove the same code succeeds at 128 MiB.
			prefix := append(const64(100<<20), w.OpcodeCall, 0, w.OpcodeI64Eqz)
			if pages {
				m.MemorySection = &w.Memory{Min: 1}
				prefix = append(const32(1600), w.OpcodeMemoryGrow, 0)
				prefix = append(prefix, const32(-1)...)
				prefix = append(prefix, w.OpcodeI32Eq)
			}
			prefix = append(prefix, w.OpcodeIf, 0x40, w.OpcodeUnreachable, w.OpcodeEnd)
			m.CodeSection[0].Body = append(prefix, m.CodeSection[0].Body...)
			r := newRuntime(t, "")
			for _, mb := range []int{64, 128} {
				p := installed(t, binary.EncodeModule(m))
				p.Lock.EffectiveLimits.MemoryMB = mb
				p.Manifest.Limits.MemoryMB = mb
				err := invoke(load(t, r, p))
				if mb == 64 && (err == nil || !strings.Contains(err.Error(), "unreachable")) {
					t.Fatalf("memory cap did not trap: %v", err)
				}
				if mb == 128 && err != nil {
					t.Fatalf("allocation inside cap failed: %v", err)
				}
			}
		})
	}
}

func TestCachedOutputLimitAndTimeoutCeilings(t *testing.T) {
	dir := t.TempDir()
	document := strings.Repeat(" ", 1100) + goodDocument
	p := installed(t, binary.EncodeModule(module(outputBody(document, int64(len(document))))))
	for _, kb := range []int{64, 1, 64} {
		// Reopen the same on-disk cache, proving cached code cannot widen a later limit.
		r := newRuntime(t, dir)
		p.Lock.EffectiveLimits.OutputKB = kb
		err := invoke(load(t, r, p))
		if kb == 1 && (err == nil || !strings.Contains(err.Error(), "output exceeds")) {
			t.Fatalf("cached cap: %v", err)
		}
		if kb == 64 && err != nil {
			t.Fatal(err)
		}
		r.Close(context.Background())
	}
	r := newRuntime(t, "")
	loop := module([]byte{w.OpcodeLoop, 0x40, w.OpcodeBr, 0, w.OpcodeEnd, w.OpcodeI32Const, 0, w.OpcodeEnd})
	p = installed(t, binary.EncodeModule(loop))
	p.Lock.EffectiveLimits.TimeoutMs = 100
	i := load(t, r, p)
	start := time.Now()
	_, err := i.Invoke(context.Background(), integrations.InvokeRequest{Operation: "stats", Deadline: time.Now().Add(time.Hour)})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("caller widened approved timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.Invoke(ctx, integrations.InvokeRequest{Operation: "stats"}); err == nil {
		t.Fatal("cancelled invocation ran")
	}
}

func TestOperationErrorsAndSignalTypes(t *testing.T) {
	r := newRuntime(t, "")
	m := module([]byte{w.OpcodeI32Const, 7, w.OpcodeEnd})
	i := load(t, r, installed(t, binary.EncodeModule(m)))
	if err := invoke(i); err == nil || !strings.Contains(err.Error(), "status 7") {
		t.Fatalf("lost guest status: %v", err)
	}
	if _, err := i.Invoke(context.Background(), integrations.InvokeRequest{Operation: "missing"}); err == nil {
		t.Fatal("unknown operation accepted")
	}
	document := `{"schemaVersion":1,"blocks":[{"type":"text","content":"ok"}],"signals":{"x":{"value":true}}}`
	for _, want := range []string{"boolean", "number"} {
		p := installed(t, binary.EncodeModule(module(outputBody(document, int64(len(document))))))
		p.Manifest.Operations[0].Signals = []manifestload.SignalDecl{{Name: "x", Type: want}}
		i := load(t, r, p)
		operations := i.Operations()
		operations[0].Signals[0].Type = "string"
		err := invoke(i)
		if want == "boolean" && err != nil {
			t.Fatal(err)
		}
		if want == "number" && (err == nil || !strings.Contains(err.Error(), "wrong type")) {
			t.Fatalf("signal type not enforced: %v", err)
		}
	}
}
