// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
)

// TestCheckConfig_Success exercises the non-exiting path directly: a valid config returns no
// error and run() need not fork a process to observe that.
func TestCheckConfig_Success(t *testing.T) {
	if err := run([]string{"--check-config", "--config", "../../examples/veduta.yaml"}); err != nil {
		t.Fatalf("run() = %v, want nil for a valid config", err)
	}
}

// TestCheckConfig_ExitsNonZeroOnError is milestone C1's own acceptance criterion, verified for
// real: checkConfig calls os.Exit(1) directly (see main.go's comment on why), which only a
// subprocess can observe without terminating the test binary itself - the standard Go pattern for
// testing os.Exit-calling code.
func TestCheckConfig_ExitsNonZeroOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nauth:\n  mode: none\nnotAField: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if os.Getenv("VEDUTA_TEST_CHECK_CONFIG_SUBPROCESS") == "1" {
		os.Args = []string{"veduta", "--check-config", "--config", path}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestCheckConfig_ExitsNonZeroOnError")
	cmd.Env = append(os.Environ(), "VEDUTA_TEST_CHECK_CONFIG_SUBPROCESS=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("want an *exec.ExitError, got %T: %v (stderr: %s)", err, err, stderr.String())
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %s)", exitErr.ExitCode(), stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("notAField")) {
		t.Fatalf("stderr should name the offending field, got: %s", stderr.String())
	}
}

func TestAuthNoneWithSecretsRequiresExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	body := []byte(`version: 1
server: {listen: "127.0.0.1:8099"}
auth: {mode: none}
dashboard: {title: Home, theme: auto, layout: {columns: 4, gap: normal}, groupBy: section}
connections:
  service:
    kind: http
    baseUrl: http://service:8080
    auth: {type: bearer, value: "${secret:TOKEN}"}
integrations: []
sections: []
rules: []
notifications: {channels: {}}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, diags := config.LoadPath(path)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	if err := validateAuthNone(snapshot, false); err == nil {
		t.Fatal("auth none with a secret should require the explicit override")
	}
	if err := validateAuthNone(snapshot, true); err != nil {
		t.Fatalf("explicit override was rejected: %v", err)
	}
}

// TestConfigLoader_RejectsAuthNoneOnHotReloadNotOnlyAtStartup is the regression test for a real
// gap found in review: validateAuthNone previously ran once, right after the initial
// config.Open, and never again - a config edited live from a safe auth mode to auth: none while
// still referencing secrets would reload successfully and go live unguarded. configLoader now
// carries the same check into every load, including a reload triggered by config.Store.Watch, so
// the bad edit is refused as an ordinary diagnostic and the last-good snapshot stays live.
func TestConfigLoader_RejectsAuthNoneOnHotReloadNotOnlyAtStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	safe := `version: 1
auth: {mode: password, admin: {username: a, passwordHash: x}}
connections:
  service: {kind: http, baseUrl: "http://service:8080", auth: {type: bearer, value: "${secret:TOKEN}"}}
`
	unsafe := `version: 1
auth: {mode: none}
connections:
  service: {kind: http, baseUrl: "http://service:8080", auth: {type: bearer, value: "${secret:TOKEN}"}}
`
	t.Setenv("TOKEN", "sometoken")
	if err := os.WriteFile(path, []byte(safe), 0o644); err != nil {
		t.Fatal(err)
	}
	store, diags := config.Open(path, nil, configLoader(false))
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	good := store.Snapshot()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = store.Watch(ctx) }()
	time.Sleep(50 * time.Millisecond) // let the watcher attach before the edit

	if err := os.WriteFile(path, []byte(unsafe), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := store.Status(); !s.OK && len(s.Diagnostics) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	status := store.Status()
	if status.OK {
		t.Fatal("the unsafe reload should have been refused, but Status().OK is true")
	}
	if store.Snapshot() != good {
		t.Fatal("the unsafe reload replaced the last-good snapshot instead of being refused")
	}
}
