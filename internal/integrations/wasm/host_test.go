// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wabin/binary"
	w "github.com/tetratelabs/wabin/wasm"
	"github.com/tetratelabs/wazero"

	"veduta.dev/veduta/internal/capabilities"
)

type hostBroker struct {
	err      error
	requests int
	cache    map[string][]byte
}

func (b *hostBroker) HTTP(context.Context, capabilities.Grant, capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	b.requests++
	return capabilities.HTTPResponse{StatusCode: 200, Body: []byte(`{"answer":42}`)}, b.err
}
func (b *hostBroker) CacheGet(_ context.Context, _ capabilities.Grant, key string) ([]byte, bool, error) {
	b.requests++
	value, found := b.cache[key]
	return value, found, b.err
}
func (b *hostBroker) CachePut(_ context.Context, _ capabilities.Grant, key string, value []byte, _ time.Duration) error {
	b.requests++
	if b.cache == nil {
		b.cache = map[string][]byte{}
	}
	b.cache[key] = value
	return b.err
}
func (b *hostBroker) AssetRef(context.Context, capabilities.Grant, string, string, url.Values, capabilities.Transform) (string, error) {
	b.requests++
	return "v1.asset.token", b.err
}
func (b *hostBroker) Log(capabilities.Grant, string, string, map[string]any) error {
	b.requests++
	return b.err
}
func (*hostBroker) Emit(context.Context, capabilities.Grant, capabilities.Event) error { return nil }

func hostCallModule(name string, input []byte, calls int) *w.Module {
	m := module(nil)
	m.TypeSection = append(m.TypeSection, &w.FunctionType{Params: []byte{w.ValueTypeI64}, Results: []byte{w.ValueTypeI64}})
	m.ImportSection = append(m.ImportSection,
		&w.Import{Type: w.ExternTypeFunc, Module: "extism:host/env", Name: "length", DescFunc: 0},
		&w.Import{Type: w.ExternTypeFunc, Module: hostNamespace, Name: name, DescFunc: 4})
	m.ExportSection[0].Index = 5
	body := []byte{}
	for call := 0; call < calls; call++ {
		body = append(body, const64(int64(len(input)))...)
		body = append(body, w.OpcodeCall, 0, w.OpcodeLocalSet, 0)
		for offset, value := range input {
			body = append(body, w.OpcodeLocalGet, 0)
			body = append(body, const64(int64(offset))...)
			body = append(body, w.OpcodeI64Add)
			body = append(body, const32(int32(value))...)
			body = append(body, w.OpcodeCall, 1)
		}
		body = append(body, w.OpcodeLocalGet, 0, w.OpcodeCall, 4)
		if call+1 < calls {
			body = append(body, w.OpcodeDrop)
		} else {
			body = append(body, w.OpcodeLocalSet, 0)
		}
	}
	body = append(body, w.OpcodeLocalGet, 0, w.OpcodeLocalGet, 0, w.OpcodeCall, 3, w.OpcodeCall, 2,
		w.OpcodeI32Const, 0, w.OpcodeEnd)
	m.CodeSection[0] = &w.Code{LocalTypes: []byte{w.ValueTypeI64}, Body: body}
	return m
}

func callHost(t *testing.T, broker capabilities.Broker, name, input string, calls int) hostResult {
	t.Helper()
	r := newRuntimeWithBroker(t, broker)
	loaded := load(t, r, installed(t, binary.EncodeModule(hostCallModule(name, []byte(input), calls))))
	in := loaded.(*instance)
	ctx := context.WithValue(context.Background(), invocationKey{}, invocationContext{broker: broker, grant: capabilities.Grant{}})
	ctx = context.WithValue(ctx, outputLimitKey{}, uint64(64<<10))
	guest, err := in.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guest.Close(context.Background()) }()
	status, output, err := guest.CallWithContext(ctx, "invoke", nil)
	if err != nil || status != 0 {
		t.Fatalf("call failed: status=%d err=%v", status, err)
	}
	var result hostResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("invalid result %q: %v", output, err)
	}
	return result
}

func newRuntimeWithBroker(t *testing.T, broker capabilities.Broker) *Runtime {
	t.Helper()
	r, err := NewWithBroker(context.Background(), "", broker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func TestHostFunctionDenialsAreValues(t *testing.T) {
	requests := map[string]string{
		"veduta_http":      `{"slot":"server","method":"GET","path":"/api"}`,
		"veduta_cache_get": `k`,
		"veduta_cache_put": `{"key":"k","value":{"x":1},"ttlSeconds":30}`,
		"veduta_asset_ref": `{"slot":"server","path":"/poster/1"}`,
		"veduta_log":       `{"level":"info","msg":"hello"}`,
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			broker := &hostBroker{err: capabilities.ErrCapDenied}
			result := callHost(t, broker, name, request, 1)
			if result.OK || result.Error == nil || result.Error.Code != "capability_denied" {
				t.Fatalf("denial trapped or was misencoded: %#v", result)
			}
		})
	}
}

func TestHostFunctionSuccessValuesAndValidation(t *testing.T) {
	broker := &hostBroker{cache: map[string][]byte{"k": []byte(`{"cached":true}`)}}
	cases := []struct {
		name, input string
	}{
		{"veduta_http", `{"slot":"server","method":"GET","path":"/api"}`},
		{"veduta_cache_get", `k`},
		{"veduta_cache_put", `{"key":"other","value":[1,2],"ttlSeconds":30}`},
		{"veduta_asset_ref", `{"slot":"server","path":"/poster/1","transform":{"width":320,"format":"webp"}}`},
		{"veduta_log", `{"level":"warn","msg":"hello","fields":{"item":1}}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if result := callHost(t, broker, test.name, test.input, 1); !result.OK || result.Error != nil {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
	for _, input := range []string{`{}`, `{"level":"verbose","msg":"x"}`, `{"key":"x","unknown":true}`} {
		result := callHost(t, broker, "veduta_log", input, 1)
		if result.OK || result.Error == nil || result.Error.Code != "invalid_request" {
			t.Fatalf("accepted invalid request %s: %#v", input, result)
		}
	}
}

func TestPluginIgnoringDenialsIsBudgetCapped(t *testing.T) {
	broker := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	grant := capabilities.NewGrant("retry", "1", "instance", nil, capabilities.NewCapSet("cache"), nil, nil, nil,
		capabilities.Limits{HostCalls: 2}, capabilities.ExecutionIdentity{})
	r := newRuntimeWithBroker(t, broker)
	loaded := load(t, r, installed(t, binary.EncodeModule(hostCallModule("veduta_cache_get", []byte(`missing`), 3))))
	in := loaded.(*instance)
	ctx := context.WithValue(context.Background(), invocationKey{}, invocationContext{broker: broker, grant: grant})
	ctx = context.WithValue(ctx, outputLimitKey{}, uint64(64<<10))
	guest, err := in.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guest.Close(context.Background()) }()
	_, output, err := guest.CallWithContext(ctx, "invoke", nil)
	if err != nil {
		t.Fatal(err)
	}
	var result hostResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Error == nil || result.Error.Code != "budget_exceeded" {
		t.Fatalf("third ignored call was not budget-capped: %s", output)
	}
}

func TestHostErrorsRemainTyped(t *testing.T) {
	for err, code := range map[error]string{
		capabilities.ErrSlotDenied:     "slot_denied",
		capabilities.ErrRouteDenied:    "route_denied",
		capabilities.ErrBudgetExceeded: "budget_exceeded",
		context.DeadlineExceeded:       "deadline_exceeded",
		context.Canceled:               "cancelled",
	} {
		if got := hostErrorCode(errors.Join(errors.New("wrapped"), err)); got != code {
			t.Errorf("hostErrorCode(%v) = %q, want %q", err, got, code)
		}
	}
	if got := hostErrorCode(errors.New(strings.Repeat("x", 1))); got != "invalid_request" {
		t.Fatalf("unexpected fallback code %q", got)
	}
}
