// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	extism "github.com/extism/go-sdk"

	"veduta.dev/veduta/internal/capabilities"
)

const (
	hostNamespace = "extism:host/user"
	maxHostInput  = 1 << 20
)

var hostFunctionNames = map[string]bool{
	"veduta_http":      true,
	"veduta_cache_get": true,
	"veduta_cache_put": true,
	"veduta_asset_ref": true,
	"veduta_log":       true,
}

type invocationKey struct{}
type invocationContext struct {
	broker capabilities.Broker
	grant  capabilities.Grant
}

type hostResult struct {
	OK    bool       `json:"ok"`
	Value any        `json:"value,omitempty"`
	Error *hostError `json:"error,omitempty"`
}

type hostError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type httpRequest struct {
	Slot    string            `json:"slot"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type cachePutRequest struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value"`
	TTLSeconds int             `json:"ttlSeconds"`
}

type assetRefRequest struct {
	Slot      string            `json:"slot"`
	Path      string            `json:"path"`
	Query     map[string]string `json:"query,omitempty"`
	Transform struct {
		Width  int    `json:"width,omitempty"`
		Format string `json:"format,omitempty"`
	} `json:"transform,omitempty"`
}

type logRequest struct {
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

func hostFunctions() []extism.HostFunction {
	build := func(name string, call func(context.Context, invocationContext, []byte) (any, error)) extism.HostFunction {
		return extism.NewHostFunctionWithStack(name, func(ctx context.Context, plugin *extism.CurrentPlugin, stack []uint64) {
			input, err := readHostInput(ctx, plugin, stack[0])
			invocation, ok := ctx.Value(invocationKey{}).(invocationContext)
			if err == nil && (!ok || invocation.broker == nil) {
				err = errors.New("wasm: capability broker unavailable")
			}
			var value any
			if err == nil {
				value, err = call(ctx, invocation, input)
			}
			result := hostResult{OK: err == nil, Value: value}
			if err != nil {
				result.Error = &hostError{Code: hostErrorCode(err), Message: err.Error()}
			}
			stack[0] = writeHostResult(ctx, plugin, result)
		}, []extism.ValueType{extism.ValueTypePTR}, []extism.ValueType{extism.ValueTypePTR})
	}
	return []extism.HostFunction{
		build("veduta_http", hostHTTP),
		build("veduta_cache_get", hostCacheGet),
		build("veduta_cache_put", hostCachePut),
		build("veduta_asset_ref", hostAssetRef),
		build("veduta_log", hostLog),
	}
}

func readHostInput(ctx context.Context, plugin *extism.CurrentPlugin, offset uint64) ([]byte, error) {
	if offset > math.MaxUint32 {
		return nil, errors.New("wasm: invalid host-function input pointer")
	}
	length, err := plugin.LengthWithContext(ctx, offset)
	if err != nil {
		return nil, fmt.Errorf("wasm: reading host-function input length: %w", err)
	}
	if length > maxHostInput || offset+length > math.MaxUint32 {
		return nil, errors.New("wasm: host-function input exceeds byte limit")
	}
	input, ok := plugin.Memory().Read(uint32(offset), uint32(length))
	if !ok {
		return nil, errors.New("wasm: invalid host-function input memory")
	}
	return bytes.Clone(input), nil
}

func writeHostResult(ctx context.Context, plugin *extism.CurrentPlugin, result hostResult) uint64 {
	body, err := json.Marshal(result)
	if err != nil {
		body = []byte(`{"ok":false,"error":{"code":"internal","message":"could not encode host response"}}`)
	}
	offset, err := plugin.AllocWithContext(ctx, uint64(len(body)))
	if err != nil || offset > math.MaxUint32 || !plugin.Memory().Write(uint32(offset), body) {
		return 0
	}
	return offset
}

func decodeHostRequest(input []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("wasm: invalid host request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("wasm: invalid host request: multiple JSON values")
	}
	return nil
}

func hostHTTP(ctx context.Context, invocation invocationContext, input []byte) (any, error) {
	var request httpRequest
	if err := decodeHostRequest(input, &request); err != nil {
		return nil, err
	}
	response, err := invocation.broker.HTTP(ctx, invocation.grant, capabilities.HTTPRequest{
		Slot: request.Slot, Method: request.Method, Path: request.Path, Query: request.Query,
		Header: request.Headers, Body: bytes.Clone(request.Body),
	})
	if err != nil {
		return nil, err
	}
	headers := response.Header
	if headers == nil {
		headers = http.Header{}
	}
	value := map[string]any{"status": response.StatusCode, "headers": headers}
	if json.Valid(response.Body) {
		value["body"] = json.RawMessage(response.Body)
	} else {
		value["bodyBase64"] = base64.StdEncoding.EncodeToString(response.Body)
	}
	return value, nil
}

func hostCacheGet(ctx context.Context, invocation invocationContext, input []byte) (any, error) {
	key := string(input)
	if key == "" {
		return nil, errors.New("wasm: cache key must not be empty")
	}
	value, found, err := invocation.broker.CacheGet(ctx, invocation.grant, key)
	if err != nil || !found {
		return nil, err
	}
	if !json.Valid(value) {
		return nil, errors.New("wasm: cached value is not JSON")
	}
	return json.RawMessage(value), nil
}

func hostCachePut(ctx context.Context, invocation invocationContext, input []byte) (any, error) {
	var request cachePutRequest
	if err := decodeHostRequest(input, &request); err != nil {
		return nil, err
	}
	if !json.Valid(request.Value) {
		return nil, errors.New("wasm: cache value must be JSON")
	}
	if request.TTLSeconds < 0 {
		return nil, errors.New("wasm: cache ttlSeconds must not be negative")
	}
	err := invocation.broker.CachePut(ctx, invocation.grant, request.Key, bytes.Clone(request.Value), time.Duration(request.TTLSeconds)*time.Second)
	return nil, err
}

func hostAssetRef(ctx context.Context, invocation invocationContext, input []byte) (any, error) {
	var request assetRefRequest
	if err := decodeHostRequest(input, &request); err != nil {
		return nil, err
	}
	query := make(url.Values, len(request.Query))
	for key, value := range request.Query {
		query.Set(key, value)
	}
	return invocation.broker.AssetRef(ctx, invocation.grant, request.Slot, request.Path, query,
		capabilities.Transform{Width: request.Transform.Width, Format: request.Transform.Format})
}

func hostLog(_ context.Context, invocation invocationContext, input []byte) (any, error) {
	var request logRequest
	if err := decodeHostRequest(input, &request); err != nil {
		return nil, err
	}
	switch request.Level {
	case "debug", "info", "warn", "error":
	default:
		return nil, errors.New("wasm: invalid log level")
	}
	return nil, invocation.broker.Log(invocation.grant, request.Level, request.Msg, request.Fields)
}

func hostErrorCode(err error) string {
	switch {
	case errors.Is(err, capabilities.ErrCapDenied):
		return "capability_denied"
	case errors.Is(err, capabilities.ErrSlotDenied):
		return "slot_denied"
	case errors.Is(err, capabilities.ErrRouteDenied):
		return "route_denied"
	case errors.Is(err, capabilities.ErrBudgetExceeded):
		return "budget_exceeded"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "invalid_request"
	}
}
