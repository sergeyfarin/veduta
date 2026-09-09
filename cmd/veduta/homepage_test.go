// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportHomepageCommand(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "veduta.yaml")
	configBody := "version: 1\nauth: {mode: none}\nconnections: {}\nintegrations: []\nsections: []\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := importHomepage([]string{"--dir", filepath.Join("..", "..", "testdata", "homepage"), "--config", configPath}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Imported 20 services · 19 complete · 1 without widgets · 0 need manual configuration") {
		t.Fatalf("summary = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "HOMEPAGE_JELLYFIN_KEY") || !strings.Contains(stderr.String(), "future-weather") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
