// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadShippedDeclarativeManifests(t *testing.T) {
	for _, name := range []string{"glances", "immich"} {
		t.Run(name, func(t *testing.T) {
			m, err := Load(filepath.Join("..", "..", "..", "plugins", name, "manifest.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if m.ID != name || len(m.Operations) == 0 {
				t.Fatalf("unexpected manifest: %#v", m)
			}
			for _, op := range m.Operations {
				if op.Output == nil {
					t.Fatalf("operation %s has no output", op.ID)
				}
			}
		})
	}
}

func TestLoadRejectsAliasAndDuplicate(t *testing.T) {
	for name, body := range map[string]string{
		"alias":     "a: &x 1\nb: *x\n",
		"duplicate": "a: 1\na: 2\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestLoadWASMModulePin(t *testing.T) {
	m, err := Load(filepath.Join("..", "..", "..", "plugins", "jellyfin", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Runtime != "wasm" || m.Module != "jellyfin.wasm" || len(m.ModuleSHA256) != 64 {
		t.Fatalf("missing executable module identity: %#v", m)
	}
}
