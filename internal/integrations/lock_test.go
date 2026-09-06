// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"veduta.dev/veduta/internal/integrations"
)

func TestReadLock_MissingFileIsEmptyNotError(t *testing.T) {
	lock, err := integrations.ReadLock(filepath.Join(t.TempDir(), "veduta.lock.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lock.Version != 1 || len(lock.Integrations) != 0 {
		t.Fatalf("got %+v, want an empty v1 lock", lock)
	}
}

func TestReadLock_RealExampleFile(t *testing.T) {
	lock, err := integrations.ReadLock("../../examples/veduta.lock.yaml")
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	for _, id := range []string{"immich", "jellyfin", "glances"} {
		if lock.Integrations[id] == nil {
			t.Errorf("missing entry for %q", id)
		}
	}
}

func TestReadLock_RejectsDocumentThatFailsSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.lock.yaml")
	// Missing the required "effectiveLimits" - the schema must catch this, not just a Go decode.
	bad := []byte("version: 1\nintegrations:\n  x:\n    manifestSha256: " +
		"'0000000000000000000000000000000000000000000000000000000000000000'\n" +
		"    approvedAt: \"2026-01-01T00:00:00Z\"\n    capabilities: []\n    routes: []\n")
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := integrations.ReadLock(path); err == nil {
		t.Fatal("expected a schema validation error")
	}
}

func TestWriteLockThenReadLock_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.lock.yaml")

	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       m.Routes,
	}, "test-admin", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}

	lock := &integrations.Lock{Version: 1, Integrations: map[string]*integrations.LockEntry{"glances": entry}}
	if err = integrations.WriteLock(path, lock); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}

	got, err := integrations.ReadLock(path)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	back := got.Integrations["glances"]
	if back == nil {
		t.Fatal("glances entry missing after round trip")
	}
	if back.ManifestSHA256 != entry.ManifestSHA256 {
		t.Fatalf("manifestSha256 = %q, want %q", back.ManifestSHA256, entry.ManifestSHA256)
	}
	if back.ApprovedBy != "test-admin" {
		t.Fatalf("approvedBy = %q, want test-admin", back.ApprovedBy)
	}
	if len(back.Routes) != len(entry.Routes) {
		t.Fatalf("routes = %d, want %d", len(back.Routes), len(entry.Routes))
	}
}

func TestWriteLock_RejectsInvalidDocumentEvenBypassingApprove(t *testing.T) {
	// A hand-built LockEntry (not produced via Approve, which always strips it) carrying a
	// route's manifest-only "reason" field - the lock schema's route $def has no such property.
	// WriteLock must catch this itself, not rely on every caller routing through Approve first.
	dir := t.TempDir()
	lock := &integrations.Lock{Version: 1, Integrations: map[string]*integrations.LockEntry{
		"bad": {
			ManifestSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
			ApprovedAt:     "2026-01-01T00:00:00Z",
			Capabilities:   []string{},
			Routes:         []integrations.Route{{Slot: "s", Method: "GET", Path: "/x", Reason: "not allowed in the lock"}},
		},
	}}
	if err := integrations.WriteLock(filepath.Join(dir, "veduta.lock.yaml"), lock); err == nil {
		t.Fatal("expected WriteLock to refuse a document with a manifest-only field")
	}
}

func TestWriteLock_NoTempFileLeftBehindOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.lock.yaml")
	lock := &integrations.Lock{Version: 1, Integrations: map[string]*integrations.LockEntry{}}
	if err := integrations.WriteLock(path, lock); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "veduta.lock.yaml" {
		t.Fatalf("dir contains %v, want exactly veduta.lock.yaml (no leftover temp file)", entries)
	}
}
