// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/integrations"
)

// captureStdout redirects os.Stdout for the duration of fn and returns everything written to it.
// Every integration subcommand prints via fmt.Printf(os.Stdout) directly, matching this file's
// existing manifestCmd/printVersion convention rather than threading a Writer through every
// function - this is the same technique main_test.go uses via a subprocess for os.Exit paths,
// applied in-process here since none of these paths call os.Exit.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// glancesFixture builds a temp directory with a minimal valid config declaring the real
// plugins/glances integration (copied in, since `path:` sources are relative to the config
// file's own directory) and returns the config path.
func glancesFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	src, err := filepath.Abs("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "plugins", "glances")
	if err = os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(src, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dst, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "veduta.yaml")
	body := "version: 1\nauth: {mode: none}\nintegrations:\n  - id: glances\n    source: path:./plugins/glances\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func TestIntegrationList_UnapprovedByDefault(t *testing.T) {
	configPath := glancesFixture(t)
	out := captureStdout(t, func() {
		if err := integrationCmd([]string{"list", "--config", configPath}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "glances") || !strings.Contains(out, string(integrations.StatusUnapproved)) {
		t.Fatalf("output = %q, want glances listed as unapproved", out)
	}
}

func TestIntegrationDiff_UnapprovedShowsFullRequest(t *testing.T) {
	configPath := glancesFixture(t)
	out := captureStdout(t, func() {
		if err := integrationCmd([]string{"diff", "--config", configPath, "glances"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "not yet approved") || !strings.Contains(out, "/api/4/cpu") {
		t.Fatalf("output = %q, want the full unapproved request listed", out)
	}
}

func TestIntegrationDiff_UnknownID(t *testing.T) {
	configPath := glancesFixture(t)
	if err := integrationCmd([]string{"diff", "--config", configPath, "not-declared"}); err == nil {
		t.Fatal("expected an error for an undeclared integration id")
	}
}

func TestIntegrationDiff_BuiltinIsExempt(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "veduta.yaml")
	body := "version: 1\nauth: {mode: none}\nintegrations:\n  - id: docker\n    source: builtin\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := integrationCmd([]string{"diff", "--config", configPath, "docker"})
	if err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("got %v, want an error naming the builtin exemption", err)
	}
}

func TestIntegrationApprove_YesFlagWritesLockAndDiffThenShowsNothingOutstanding(t *testing.T) {
	configPath := glancesFixture(t)
	dir := filepath.Dir(configPath)

	out := captureStdout(t, func() {
		if err := integrationCmd([]string{"approve", "--config", configPath, "--yes", "glances"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	if !strings.Contains(out, "approved") {
		t.Fatalf("output = %q, want confirmation of approval", out)
	}

	lockPath := filepath.Join(dir, integrations.LockFileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file was not written: %v", err)
	}
	lock, err := integrations.ReadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Integrations["glances"] == nil {
		t.Fatal("glances not recorded in the written lock")
	}

	after := captureStdout(t, func() {
		if err := integrationCmd([]string{"diff", "--config", configPath, "glances"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(after, "already approved") {
		t.Fatalf("post-approval diff = %q, want nothing outstanding", after)
	}
}

func TestIntegrationApprove_DeclinedConfirmationWritesNothing(t *testing.T) {
	configPath := glancesFixture(t)
	dir := filepath.Dir(configPath)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WriteString("n\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	err = integrationCmd([]string{"approve", "--config", configPath, "glances"})
	if err == nil {
		t.Fatal("expected an error when the confirmation prompt is declined")
	}
	if _, statErr := os.Stat(filepath.Join(dir, integrations.LockFileName)); statErr == nil {
		t.Fatal("a declined approval must not write a lock file")
	}
}

func TestIntegrationApprove_AlreadyApprovedIsANoOp(t *testing.T) {
	configPath := glancesFixture(t)
	captureStdout(t, func() {
		if err := integrationCmd([]string{"approve", "--config", configPath, "--yes", "glances"}); err != nil {
			t.Fatal(err)
		}
	})
	// Second approve: diff is empty, so no confirmation is needed even without --yes and stdin
	// closed - this proves the "nothing to do" short-circuit actually short-circuits.
	origStdin := os.Stdin
	os.Stdin = nil
	defer func() { os.Stdin = origStdin }()
	if err := integrationCmd([]string{"approve", "--config", configPath, "glances"}); err != nil {
		t.Fatalf("re-approving an already-approved integration should be a no-op, got: %v", err)
	}
}

func TestIntegrationCmd_UnknownSubcommand(t *testing.T) {
	if err := integrationCmd([]string{"frobnicate"}); err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
}
