// SPDX-License-Identifier: AGPL-3.0-or-later

package storage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/storage"
)

func TestCheckDataDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("an existing writable directory passes, resolved to absolute", func(t *testing.T) {
		got, err := storage.CheckDataDir(root, false)
		if err != nil || !filepath.IsAbs(got) {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a file is named as one", func(t *testing.T) {
		_, err := storage.CheckDataDir(file, true)
		if err == nil || !strings.Contains(err.Error(), "is a file, not a directory") || !strings.Contains(err.Error(), file) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a directory under a file cannot be created", func(t *testing.T) {
		for _, create := range []bool{false, true} {
			_, err := storage.CheckDataDir(filepath.Join(file, "data"), create)
			if err == nil || !strings.Contains(err.Error(), file+" is a file") {
				t.Fatalf("create=%v: err = %v, want it to name %s as the file in the way", create, err, file)
			}
		}
	})
	t.Run("checking does not create; creating does", func(t *testing.T) {
		missing := filepath.Join(root, "new", "data")
		if _, err := storage.CheckDataDir(missing, false); err != nil {
			t.Fatalf("a creatable directory was refused: %v", err)
		}
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatal("a check-only call created the directory")
		}
		if _, err := storage.CheckDataDir(missing, true); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(missing); err != nil || !info.IsDir() {
			t.Fatalf("create did not create it: %v", err)
		}
	})
	t.Run("an unwritable directory names the path and the uid", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root writes anywhere")
		}
		locked := filepath.Join(root, "locked")
		if err := os.Mkdir(locked, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		_, err := storage.CheckDataDir(locked, true)
		if err == nil || !strings.Contains(err.Error(), "not writable") || !strings.Contains(err.Error(), "uid") {
			t.Fatalf("err = %v", err)
		}
		if _, err := storage.CheckDataDir(filepath.Join(locked, "data"), false); err == nil {
			t.Fatal("accepted a directory that could not be created in an unwritable parent")
		}
	})
	t.Run("a relative value resolves against the working directory", func(t *testing.T) {
		t.Chdir(root)
		got, err := storage.CheckDataDir("./data", false)
		if err != nil || got != filepath.Join(root, "data") {
			t.Fatalf("got %q, %v; want %s", got, err, filepath.Join(root, "data"))
		}
	})
}
