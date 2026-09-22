// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"

	"veduta.dev/veduta/internal/config"
)

// verifyArgon2ID recomputes the hash from the PHC string rather than calling into internal/auth,
// whose verifier is unexported. Recomputing independently is also the stronger test: it proves
// the written hash is a standard Argon2id verifier for the printed password, not merely that
// Veduta agrees with itself.
func verifyArgon2ID(t *testing.T, password, encoded string) bool {
	t.Helper()
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		t.Fatalf("not an Argon2id PHC string: %q", encoded)
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		t.Fatalf("unparseable cost %q: %v", parts[3], err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("unparseable salt: %v", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		t.Fatalf("unparseable hash: %v", err)
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// runInitForTest exercises the command the way compose does - writing a configuration into a
// directory that does not yet hold one - and returns what the operator would read.
func runInitForTest(t *testing.T, configPath string) (stdout, stderr string) {
	t.Helper()
	var out, errs bytes.Buffer
	if err := runInit(&out, &errs, configPath, "0.0.0.0:8099", "/data", "Home", "admin", false); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	return out.String(), errs.String()
}

// TestInitWritesALoadableConfigWithAWorkingPassword is the whole point of the command: what it
// writes must load, and the password it prints must verify against the hash it stored. Checking
// the two separately would miss the case that matters, which is them disagreeing.
func TestInitWritesALoadableConfigWithAWorkingPassword(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "veduta.yaml")
	_, stderr := runInitForTest(t, configPath)

	password := extractPassword(t, stderr)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyArgon2ID(t, password, extractHash(t, string(raw))) {
		t.Fatal("the password veduta init printed does not verify against the hash it wrote")
	}

	if _, diags := config.Load(configPath); diags.HasErrors() {
		t.Fatalf("veduta init wrote a configuration that does not load:\n%s", diags.String())
	}
}

// TestInitDoesNotPrintTheHashOrStoreThePassword guards the two directions the secret can leak:
// the plaintext must not reach the file, and the verifier must not be the thing announced.
func TestInitDoesNotPrintTheHashOrStoreThePassword(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "veduta.yaml")
	_, stderr := runInitForTest(t, configPath)

	password := extractPassword(t, stderr)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), password) {
		t.Fatal("the plaintext password was written into veduta.yaml")
	}
	if strings.Contains(stderr, "$argon2id$") {
		t.Fatal("the password hash was printed to the operator")
	}
}

// TestInitIsIdempotent covers the compose contract: the init service runs on every `up`, so a
// second run must leave an edited configuration exactly as it found it. Rewriting it would
// invalidate the password the operator saved and discard their edits at once.
func TestInitIsIdempotent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "veduta.yaml")
	runInitForTest(t, configPath)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	var out, errs bytes.Buffer
	if err = runInit(&out, &errs, configPath, "0.0.0.0:8099", "/data", "Home", "admin", false); err != nil {
		t.Fatalf("second runInit: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a second veduta init rewrote an existing configuration")
	}
	if !strings.Contains(errs.String(), "already exists") {
		t.Fatalf("a second run should say it left the file alone, got: %q", errs.String())
	}
	if strings.Contains(errs.String(), "Sign in as") {
		t.Fatal("a second run announced a password that was never written")
	}
}

// TestInitHonoursASuppliedPassword covers the scripted path, where the operator already knows the
// credential. The plaintext must still never be written - only the verifier for it.
func TestInitHonoursASuppliedPassword(t *testing.T) {
	const supplied = "a-password-chosen-by-the-operator"
	t.Setenv("VEDUTA_ADMIN_PASSWORD", supplied)
	configPath := filepath.Join(t.TempDir(), "veduta.yaml")
	_, stderr := runInitForTest(t, configPath)

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), supplied) {
		t.Fatal("the supplied plaintext password was written into veduta.yaml")
	}
	if !verifyArgon2ID(t, supplied, extractHash(t, string(raw))) {
		t.Fatal("the supplied password does not verify against the written hash")
	}
	if strings.Contains(stderr, supplied) {
		t.Fatal("the supplied password was echoed back to the operator")
	}
}

// TestInitRefusesAnUnwritableDirectory checks the message rather than the failure: a bare
// "permission denied" is what sent operators to the documentation in the first place.
func TestInitRefusesAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, which is the case this test is about")
	}
	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	err := runInit(&out, &errs, filepath.Join(dir, "veduta.yaml"), "0.0.0.0:8099", "/data", "Home", "admin", false)
	if err == nil {
		t.Fatal("expected a refusal writing into an unwritable directory")
	}
	if !strings.Contains(err.Error(), "writable") {
		t.Fatalf("the error should point at directory permissions, got: %v", err)
	}
}

// TestGeneratedPasswordsAreDistinctAndWellFormed: the alphabet divides 256 evenly so that
// indexing a random byte into it stays uniform, and two runs must not collide.
func TestGeneratedPasswordsAreDistinctAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		password, err := generatePassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(password) != passwordLength {
			t.Fatalf("length %d, want %d", len(password), passwordLength)
		}
		if strings.ContainsAny(password, "0Ol1I") {
			t.Fatalf("%q contains a character the alphabet excludes as misreadable", password)
		}
		for _, r := range password {
			if !strings.ContainsRune(passwordAlphabet, r) {
				t.Fatalf("%q contains %q, which is outside the alphabet", password, r)
			}
		}
		if seen[password] {
			t.Fatalf("generated the same password twice: %q", password)
		}
		seen[password] = true
	}
}

func extractPassword(t *testing.T, stderr string) string {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		candidate := strings.TrimSpace(line)
		if len(candidate) == passwordLength && strings.Trim(candidate, passwordAlphabet) == "" {
			return candidate
		}
	}
	t.Fatalf("no password found in the announcement:\n%s", stderr)
	return ""
}

func extractHash(t *testing.T, contents string) string {
	t.Helper()
	for _, line := range strings.Split(contents, "\n") {
		_, value, found := strings.Cut(line, "passwordHash:")
		if !found {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	t.Fatalf("no passwordHash in the written configuration:\n%s", contents)
	return ""
}
