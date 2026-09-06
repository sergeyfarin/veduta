// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"net/http"
	"testing"
	"time"

	"veduta.dev/veduta/internal/integrations"
)

// TestComputeDiff_RealExamplesMatchTheirLockRecords is the load-bearing integration test: it
// proves the diff logic reproduces the real, checked-in plugins/*/manifest.yaml against the
// real, checked-in examples/veduta.lock.yaml, not just synthetic fixtures. glances and jellyfin
// are approved for exactly what their manifests request; immich is approved for a deliberate
// subset (docs/01-architecture.md: "approving a subset ... is the normal case").
func TestComputeDiff_RealExamplesMatchTheirLockRecords(t *testing.T) {
	lock, err := integrations.ReadLock("../../examples/veduta.lock.yaml")
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}

	for _, c := range []struct {
		id              string
		wantEmpty       bool
		wantAddedRoutes int
	}{
		{"glances", true, 0},
		{"jellyfin", true, 0},
		{"immich", false, 1},
	} {
		t.Run(c.id, func(t *testing.T) {
			m, err := integrations.LoadManifest("../../plugins/" + c.id)
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			entry := lock.Integrations[c.id]
			if entry == nil {
				t.Fatalf("no lock entry for %s", c.id)
			}
			if entry.ManifestSHA256 != m.Digest {
				t.Fatalf("fixture drift: lock records %s, manifest digests to %s - "+
					"regenerate examples/veduta.lock.yaml if plugins/%s/manifest.yaml changed",
					entry.ManifestSHA256, m.Digest, c.id)
			}
			diff, err := integrations.ComputeDiff(m, entry)
			if err != nil {
				t.Fatalf("ComputeDiff: %v", err)
			}
			if diff.Empty() != c.wantEmpty {
				t.Fatalf("diff = %+v, want Empty()=%v", diff, c.wantEmpty)
			}
			if len(diff.AddedRoutes) != c.wantAddedRoutes {
				t.Fatalf("addedRoutes = %+v, want %d entries", diff.AddedRoutes, c.wantAddedRoutes)
			}
		})
	}
}

func TestComputeDiff_ImmichFlagsTheUnapprovedServerStatisticsRoute(t *testing.T) {
	lock, err := integrations.ReadLock("../../examples/veduta.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := integrations.LoadManifest("../../plugins/immich")
	if err != nil {
		t.Fatal(err)
	}
	diff, err := integrations.ComputeDiff(m, lock.Integrations["immich"])
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.AddedRoutes) != 1 || diff.AddedRoutes[0].Path != "/api/server/statistics" {
		t.Fatalf("addedRoutes = %+v, want exactly /api/server/statistics", diff.AddedRoutes)
	}
	if len(diff.RemovedRoutes) != 0 || len(diff.AddedCapabilities) != 0 || len(diff.RaisedLimits) != 0 {
		t.Fatalf("expected only the added route to differ, got %+v", diff)
	}
}

func TestComputeDiff_UnapprovedIntegrationShowsEverythingAsAdded(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	diff, err := integrations.ComputeDiff(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.AddedRoutes) != len(m.Routes) {
		t.Fatalf("addedRoutes = %d, want %d (every manifest route)", len(diff.AddedRoutes), len(m.Routes))
	}
	if len(diff.AddedCapabilities) != len(m.Capabilities) {
		t.Fatalf("addedCapabilities = %d, want %d", len(diff.AddedCapabilities), len(m.Capabilities))
	}
	if diff.Empty() {
		t.Fatal("an unapproved integration must never show an empty diff")
	}
}

func TestComputeDiff_RaisedLimitDetected(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := integrations.Approve(m, m.Digest, integrations.Grants{Capabilities: m.Capabilities, Routes: m.Routes},
		"a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Mutation test: lower the recorded effectiveLimits.timeoutMs below what the manifest asks
	// for, and confirm the diff catches it - this is exactly the check that would have caught
	// examples/veduta.lock.yaml's own glances.timeoutMs authoring bug (3000 recorded, 4000
	// actually requested) before this milestone existed to compute it.
	entry.EffectiveLimits.TimeoutMs = 1000
	diff, err := integrations.ComputeDiff(m, entry)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ld := range diff.RaisedLimits {
		if ld.Field == "timeoutMs" {
			found = true
			if ld.Current != 1000 || ld.Requested != 4000 {
				t.Fatalf("timeoutMs diff = %+v, want current=1000 requested=4000", ld)
			}
		}
	}
	if !found {
		t.Fatalf("expected timeoutMs in raisedLimits, got %+v", diff.RaisedLimits)
	}
}

func TestComputeDiff_MalformedManifestRoutePropagatesError(t *testing.T) {
	m := &integrations.Manifest{Routes: []integrations.Route{{Slot: "s", Method: "GET", Path: "/a/../b"}}}
	if _, err := integrations.ComputeDiff(m, nil); err == nil {
		t.Fatal("expected the manifest route's canonicalisation error to propagate")
	}
}

func TestComputeDiff_MalformedLockRoutePropagatesError(t *testing.T) {
	m := &integrations.Manifest{}
	entry := &integrations.LockEntry{Routes: []integrations.Route{{Slot: "s", Method: "GET", Path: "/a/../b"}}}
	if _, err := integrations.ComputeDiff(m, entry); err == nil {
		t.Fatal("expected the lock route's canonicalisation error to propagate")
	}
}

func TestComputeDiff_BodyBearingRoutesFlagged(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/immich")
	if err != nil {
		t.Fatal(err)
	}
	diff, err := integrations.ComputeDiff(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range diff.BodyBearingRoutes {
		if r.Method == http.MethodPost && r.Path == "/api/search/metadata" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the POST /api/search/metadata route flagged as body-bearing, got %+v", diff.BodyBearingRoutes)
	}
	for _, r := range diff.BodyBearingRoutes {
		if r.Method == http.MethodGet {
			t.Fatalf("a GET route must never be flagged as body-bearing: %+v", r)
		}
	}
}
