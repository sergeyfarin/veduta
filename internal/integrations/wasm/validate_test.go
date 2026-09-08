// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wabin/binary"
	w "github.com/tetratelabs/wabin/wasm"
)

func writeValidationModule(t *testing.T, module *w.Module) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.WriteFile(path, binary.EncodeModule(module), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateFile(t *testing.T) {
	if err := ValidateFile(context.Background(), writeValidationModule(t, goodModule())); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFileRejectsForbiddenImportAndLoop(t *testing.T) {
	for name, module := range map[string]*w.Module{
		"forbidden": func() *w.Module {
			module := goodModule()
			module.ImportSection[0].Module = "wasi_snapshot_preview1"
			module.ImportSection[0].Name = "path_open"
			return module
		}(),
		"loop": module([]byte{w.OpcodeLoop, 0x40, w.OpcodeBr, 0, w.OpcodeEnd, w.OpcodeI32Const, 0, w.OpcodeEnd}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if name == "loop" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 25*time.Millisecond)
				defer cancel()
			}
			err := ValidateFile(ctx, writeValidationModule(t, module))
			if err == nil || (name == "forbidden" && !strings.Contains(err.Error(), "forbidden import")) {
				t.Fatalf("unsafe module accepted: %v", err)
			}
		})
	}
}
