// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"errors"
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
	if len(m.Routes) != 4 {
		t.Fatalf("routes = %d, want 4: %+v", len(m.Routes), m.Routes)
	}
	if m.Digest == "" {
		t.Fatal("digest must not be empty")
	}
}

func TestLoadManifest_ImmichAggregatesRoutesAcrossOperations(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/immich")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	// Two operations: recent-assets (3 routes) and server-statistics (1 route).
	if len(m.Routes) != 4 {
		t.Fatalf("routes = %d, want 4 (aggregated across both operations): %+v", len(m.Routes), m.Routes)
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
