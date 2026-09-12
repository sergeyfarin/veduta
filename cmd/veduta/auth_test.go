// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stdinFile is the only way to exercise readPassword honestly: it stats its input to decide
// whether to prompt, so an in-memory reader would skip the branch that matters.
func stdinFile(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestReadPasswordAcceptsOneLineHoweverItIsTerminated(t *testing.T) {
	cases := map[string]string{
		"no terminator":     "hunter2",
		"unix newline":      "hunter2\n",
		"windows newline":   "hunter2\r\n",
		"trailing blank":    "hunter2\n\n",
		"trailing newlines": "hunter2\n\n\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := readPassword(stdinFile(t, content), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if got != "hunter2" {
				t.Fatalf("got %q", got)
			}
		})
	}
}

// Hashing only the first line of a file that holds more would produce a verifier for a password
// the operator never chose, discovered at the login screen rather than here.
func TestReadPasswordRefusesAmbiguousInput(t *testing.T) {
	for name, content := range map[string]string{
		"empty":       "",
		"newline":     "\n",
		"two lines":   "first\nsecond\n",
		"second word": "first\n  second  \n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readPassword(stdinFile(t, content), io.Discard); err == nil {
				t.Fatal("accepted ambiguous stdin")
			}
		})
	}
}

func TestAuthCmdUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"other"}, {"hash", "a-password"}} {
		if err := authCmd(args); err == nil {
			t.Fatalf("authCmd(%q) accepted invalid usage", args)
		}
	}
}

// A password on the command line lands in shell history and in every other user's `ps`. The
// refusal must name the reason, not just fail.
func TestAuthCmdRefusesAPasswordArgument(t *testing.T) {
	err := authCmd([]string{"hash", "hunter2"})
	if err == nil || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthCmdRejectsCostOutsideTheSupportedRange(t *testing.T) {
	for _, args := range [][]string{
		{"hash", "--memory", "1024"},
		{"hash", "--memory", "999999"},
		{"hash", "--iterations", "0"},
		{"hash", "--parallelism", "99"},
	} {
		// Asserting on the message, not merely on failure: an unusable cost must be refused
		// before stdin is read, and "no password on stdin" would otherwise pass this test
		// while the operator was still prompted for a password the command could not use.
		err := authCmd(args)
		if err == nil || !strings.Contains(err.Error(), "supported range") {
			t.Fatalf("authCmd(%q): got %v", args, err)
		}
	}
}
