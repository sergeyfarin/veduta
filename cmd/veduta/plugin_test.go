// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"path/filepath"
	"testing"
)

func TestPluginValidateUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"other"}, {"validate"}, {"validate", "one", "two"}} {
		if err := pluginCmd(args); err == nil {
			t.Fatalf("pluginCmd(%q) accepted invalid usage", args)
		}
	}
}

func TestPluginValidateExample(t *testing.T) {
	path := filepath.Join("..", "..", "plugins", "examples", "hello", "hello.wasm")
	if err := pluginCmd([]string{"validate", path}); err != nil {
		t.Fatal(err)
	}
}
