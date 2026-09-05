// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets_test

import (
	"os"
	"path/filepath"
	"testing"

	"veduta.dev/veduta/internal/secrets"
)

func TestEnvProvider(t *testing.T) {
	t.Setenv("VEDUTA_TEST_SECRET", "hello-env")
	p := secrets.EnvProvider{}

	v, ok, err := p.Resolve("VEDUTA_TEST_SECRET")
	if err != nil || !ok || v != "hello-env" {
		t.Fatalf("Resolve = %q, %v, %v", v, ok, err)
	}

	_, ok, err = p.Resolve("VEDUTA_TEST_SECRET_DOES_NOT_EXIST")
	if err != nil || ok {
		t.Fatalf("want ok=false, err=nil for an unset variable; got ok=%v err=%v", ok, err)
	}
}

func TestFileProvider(t *testing.T) {
	dir := t.TempDir()
	// Trailing newline, as `echo secret > file` would produce - must be trimmed.
	if err := os.WriteFile(filepath.Join(dir, "TOKEN"), []byte("hello-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := secrets.FileProvider{Dir: dir}

	v, ok, err := p.Resolve("TOKEN")
	if err != nil || !ok || v != "hello-file" {
		t.Fatalf("Resolve = %q, %v, %v", v, ok, err)
	}

	_, ok, err = p.Resolve("MISSING")
	if err != nil || ok {
		t.Fatalf("want ok=false, err=nil for a missing file; got ok=%v err=%v", ok, err)
	}
}

// TestFileProvider_RejectsPathTraversal is a defence-in-depth test, not merely a documentation
// one: FileProvider validates the name itself rather than trusting every future caller to have
// pre-validated it the way internal/config's decoder happens to.
func TestFileProvider_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.WriteFile(outside, []byte("should-not-be-readable"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := secrets.FileProvider{Dir: dir}
	for _, name := range []string{"../outside-secret", "a/b", "a\\b", ""} {
		if _, ok, err := p.Resolve(name); err == nil {
			t.Errorf("Resolve(%q) = ok=%v err=%v, want a rejection error", name, ok, err)
		}
	}
}

func TestFileProvider_PermissionErrorIsReturned(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not apply")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "SECRET")
	if err := os.WriteFile(path, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	p := secrets.FileProvider{Dir: dir}
	_, ok, err := p.Resolve("SECRET")
	if err == nil {
		t.Fatal("want a real error for a permission-denied file, not a silent ok=false")
	}
	if ok {
		t.Fatal("ok should be false alongside an error")
	}
}
