// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
