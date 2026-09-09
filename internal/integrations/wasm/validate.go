// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"

	"veduta.dev/veduta/internal/widgets"
)

// ValidateFile runs a third-party module through the production import policy,
// resource limits, instantiation path, and a bounded ABI smoke invocation.
func ValidateFile(ctx context.Context, path string) error {
	file, err := os.Open(path) // #nosec G304 -- path is explicitly selected by the CLI user.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	code, err := io.ReadAll(io.LimitReader(file, MaxModuleBytes+1))
	if err != nil {
		return err
	}
	if len(code) > MaxModuleBytes {
		return errors.New("wasm: module exceeds byte limit")
	}
	// Compilation under the race detector can legitimately take several seconds. Keep it
	// bounded independently so compile time cannot consume the guest invocation's security
	// deadline and make validation depend on host load.
	compileCtx, cancelCompile := context.WithTimeout(ctx, 15*time.Second)
	defer cancelCompile()
	cache := wazero.NewCompilationCache()
	defer func() { _ = cache.Close(context.Background()) }()
	config := wazero.NewRuntimeConfig().WithCompilationCache(cache).
		WithMemoryLimitPages(64 * 16).WithCloseOnContextDone(true)
	if preflightErr := preflight(compileCtx, code, config); preflightErr != nil {
		return preflightErr
	}
	compileCtx = experimental.WithFunctionListenerFactory(compileCtx, outputLimiter{})
	compiled, err := extism.NewCompiledPlugin(compileCtx, extism.Manifest{
		Wasm: []extism.Wasm{extism.WasmData{Data: code}},
		Memory: &extism.ManifestMemory{
			MaxPages:    64 * 16,
			MaxVarBytes: 0,
		},
		AllowedHosts: []string{},
	}, extism.PluginConfig{RuntimeConfig: config, EnableWasi: false}, hostFunctions())
	if err != nil {
		return fmt.Errorf("wasm: compile: %w", err)
	}
	defer func() { _ = compiled.Close(context.Background()) }()
	invokeCtx, cancelInvoke := context.WithTimeout(ctx, 3*time.Second)
	defer cancelInvoke()
	invokeCtx = experimental.WithFunctionListenerFactory(invokeCtx, outputLimiter{})
	invokeCtx = context.WithValue(invokeCtx, invocationKey{}, invocationContext{})
	invokeCtx = context.WithValue(invokeCtx, outputLimitKey{}, uint64(64<<10))
	guest, err := compiled.Instance(invokeCtx, extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig()})
	if err != nil {
		return fmt.Errorf("wasm: instantiate: %w", err)
	}
	defer func() { _ = guest.Close(context.Background()) }()
	status, output, err := guest.CallWithContext(invokeCtx, "invoke",
		[]byte(`{"operation":"__veduta_validate__","params":{},"now":"2000-01-01T00:00:00Z"}`))
	if err != nil {
		return fmt.Errorf("wasm: validation invocation: %w", err)
	}
	// A plugin may reject the synthetic operation. A successful invocation must
	// still prove that it emits a valid bounded Widget Document.
	if status == 0 {
		if _, err := widgets.ValidateWithLimit(output, 64<<10); err != nil {
			return fmt.Errorf("wasm: validation output: %w", err)
		}
	}
	return nil
}
