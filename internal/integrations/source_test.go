// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"path/filepath"
	"testing"

	"veduta.dev/veduta/internal/integrations"
)

func TestResolveSource_Builtin(t *testing.T) {
	src, err := integrations.ResolveSource("/etc/veduta", "builtin")
	if err != nil {
		t.Fatal(err)
	}
	if !src.Builtin || src.Dir != "" {
		t.Fatalf("got %+v, want a bare builtin source", src)
	}
}

func TestResolveSource_PathIsRelativeToConfigDir(t *testing.T) {
	src, err := integrations.ResolveSource("/etc/veduta", "path:./plugins/immich")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/etc/veduta", "plugins", "immich")
	if src.Builtin || src.Dir != want {
		t.Fatalf("got %+v, want Dir=%s", src, want)
	}
}

func TestResolveSource_UnknownSchemeRejected(t *testing.T) {
	if _, err := integrations.ResolveSource("/etc/veduta", "docker-image:immich/server"); err == nil {
		t.Fatal("expected an error for an unrecognised source scheme")
	}
}

func TestResolveSource_RealExampleConfig(t *testing.T) {
	// examples/veduta.yaml's own declared sources ("path:./plugins/immich") are relative to
	// wherever veduta.yaml itself is deployed - the repository root, where plugins/ actually
	// lives, is what that resolves against in a real checkout.
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		source  string
		builtin bool
	}{
		{"path:./plugins/immich", false},
		{"path:./plugins/jellyfin", false},
		{"path:./plugins/glances", false},
		{"builtin", true},
	} {
		src, err := integrations.ResolveSource(dir, c.source)
		if err != nil {
			t.Fatalf("%s: %v", c.source, err)
		}
		if src.Builtin != c.builtin {
			t.Fatalf("%s: builtin = %v, want %v", c.source, src.Builtin, c.builtin)
		}
		if !c.builtin {
			if _, err := integrations.LoadManifest(src.Dir); err != nil {
				t.Fatalf("%s: LoadManifest(%s): %v", c.source, src.Dir, err)
			}
		}
	}
}
