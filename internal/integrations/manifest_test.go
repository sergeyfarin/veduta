// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/integrations"
)

func TestLoadManifest_Glances(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.ID != "glances" {
		t.Fatalf("id = %q, want glances", m.ID)
	}
	if m.Runtime != "declarative" {
		t.Fatalf("runtime = %q, want declarative", m.Runtime)
	}
	if len(m.Capabilities) != 1 || m.Capabilities[0] != "http" {
		t.Fatalf("capabilities = %v, want [http]", m.Capabilities)
	}
	if len(m.Routes) != 6 {
		t.Fatalf("routes = %d, want 6: %+v", len(m.Routes), m.Routes)
	}
	if m.Digest == "" {
		t.Fatal("digest must not be empty")
	}
}

// TestLoadManifest_RejectsOversizedManifestWithoutReadingItWhole is the regression test for a
// gap found in review: LoadManifest used to read the whole file with os.ReadFile before checking
// its length, so a manifest far larger than the 256 KiB cap would still be fully allocated before
// being rejected. This does not assert on memory directly (Go has no cheap way to do that from a
// black-box test), but it does prove the cap is actually enforced end to end - the same coverage
// manifestload's own TestLoad_RejectsOversizedManifest has for its loader.
func TestLoadManifest_RejectsOversizedManifestWithoutReadingItWhole(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("x", 300<<10) // 300 KiB, past the 256 KiB cap
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := integrations.LoadManifest(dir); err == nil {
		t.Fatal("expected LoadManifest to reject an oversized manifest")
	}
}

func TestLoadManifest_ImmichAggregatesRoutesAcrossOperations(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/immich")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	// Three operations share the thumbnail route: 3 + 1 + 2 - 1 = 5 unique routes.
	if len(m.Routes) != 5 {
		t.Fatalf("routes = %d, want 5 (aggregated across all operations): %+v", len(m.Routes), m.Routes)
	}
}

func TestLoadManifest_JellyfinWasmCarriesModuleSHA256(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/jellyfin")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Runtime != "wasm" {
		t.Fatalf("runtime = %q, want wasm", m.Runtime)
	}
	if m.ModuleSHA256 == "" {
		t.Fatal("wasm manifest must carry its declared module sha256")
	}
}

func TestLoadManifest_MissingDirectory(t *testing.T) {
	_, err := integrations.LoadManifest("../../plugins/does-not-exist")
	var notFound *integrations.ErrManifestNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("got %v (%T), want *ErrManifestNotFound", err, err)
	}
	if notFound.Error() == "" {
		t.Fatal("Error() must describe which directory was missing a manifest")
	}
}

func TestLoadManifest_DigestStableAcrossReload(t *testing.T) {
	a, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	b, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("digest is not stable: %s != %s", a.Digest, b.Digest)
	}
}
