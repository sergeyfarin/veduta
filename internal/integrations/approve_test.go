// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"errors"
	"testing"
	"time"

	"veduta.dev/veduta/internal/integrations"
)

func TestApprove_HappyPath(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       m.Routes,
		Limits:       m.Limits,
	}, "sergey", time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if entry.ManifestSHA256 != m.Digest {
		t.Fatalf("manifestSha256 = %q, want %q", entry.ManifestSHA256, m.Digest)
	}
	if entry.ApprovedAt != "2026-03-01T12:00:00Z" {
		t.Fatalf("approvedAt = %q", entry.ApprovedAt)
	}
	if entry.EffectiveLimits.TimeoutMs != 4000 {
		t.Fatalf("effectiveLimits.timeoutMs = %d, want 4000 - the manifest's own request, granted "+
			"because Grants.Limits explicitly echoed it back", entry.EffectiveLimits.TimeoutMs)
	}
}

func TestApprove_WithoutAnExplicitLimitsOverrideStaysAtDefault(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances") // requests timeoutMs: 4000
	if err != nil {
		t.Fatal(err)
	}
	entry, err := integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       m.Routes,
		// Limits deliberately omitted - the manifest asks for timeoutMs above the documented
		// default (3000), but nothing here explicitly approves that: effective must stay at the
		// default, matching internal/contracts/semantic.go's own effectiveLimit formula, which
		// examples/veduta.lock.yaml is checked against.
	}, "sergey", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if entry.EffectiveLimits.TimeoutMs != 3000 {
		t.Fatalf("effectiveLimits.timeoutMs = %d, want 3000 (the default - no override was given)", entry.EffectiveLimits.TimeoutMs)
	}
	if entry.Limits != nil {
		t.Fatalf("Limits = %+v, want nil (Grants.Limits was the zero value)", entry.Limits)
	}
}

func TestApprove_RefusesLimitAboveWhatManifestRequests(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances") // requests timeoutMs: 4000
	if err != nil {
		t.Fatal(err)
	}
	over := 9000
	_, err = integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       m.Routes,
		Limits:       integrations.Limits{TimeoutMs: &over},
	}, "sergey", time.Now())
	if !errors.Is(err, integrations.ErrGrantExceedsRequest) {
		t.Fatalf("got %v, want ErrGrantExceedsRequest", err)
	}
}

func TestApprove_StaleDigestRefused(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	_, err = integrations.Approve(m, "0000000000000000000000000000000000000000000000000000000000000000",
		integrations.Grants{Capabilities: m.Capabilities, Routes: m.Routes}, "sergey", time.Now())
	if !errors.Is(err, integrations.ErrDigestChanged) {
		t.Fatalf("got %v, want ErrDigestChanged", err)
	}
}

func TestApprove_RefusesCapabilityNotRequested(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances") // requests only [http]
	if err != nil {
		t.Fatal(err)
	}
	_, err = integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: []string{"http", "assets"}, // assets was never requested
		Routes:       m.Routes,
	}, "sergey", time.Now())
	if !errors.Is(err, integrations.ErrGrantExceedsRequest) {
		t.Fatalf("got %v, want ErrGrantExceedsRequest", err)
	}
}

func TestApprove_RefusesRouteNotRequested(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	extra := append(append([]integrations.Route(nil), m.Routes...),
		integrations.Route{Slot: "server", Method: "DELETE", Path: "/api/4/all"})
	_, err = integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       extra,
	}, "sergey", time.Now())
	if !errors.Is(err, integrations.ErrGrantExceedsRequest) {
		t.Fatalf("got %v, want ErrGrantExceedsRequest", err)
	}
}

func TestApprove_MalformedManifestRoutePropagatesError(t *testing.T) {
	m := &integrations.Manifest{
		Digest: "d",
		Routes: []integrations.Route{{Slot: "s", Method: "GET", Path: "/a/../b"}},
	}
	_, err := integrations.Approve(m, "d", integrations.Grants{}, "a", time.Now())
	if err == nil {
		t.Fatal("expected the manifest route's canonicalisation error to propagate")
	}
}

func TestApprove_MalformedGrantRoutePropagatesAsExceedsRequest(t *testing.T) {
	m := &integrations.Manifest{Digest: "d"}
	_, err := integrations.Approve(m, "d", integrations.Grants{
		Routes: []integrations.Route{{Slot: "s", Method: "GET", Path: "/a/../b"}},
	}, "a", time.Now())
	if !errors.Is(err, integrations.ErrGrantExceedsRequest) {
		t.Fatalf("got %v, want ErrGrantExceedsRequest", err)
	}
}

func TestApprove_SubsetOfRequestedRoutesIsAllowed(t *testing.T) {
	m, err := integrations.LoadManifest("../../plugins/immich")
	if err != nil {
		t.Fatal(err)
	}
	var subset []integrations.Route
	for _, r := range m.Routes {
		if r.Path != "/api/server/statistics" {
			subset = append(subset, r)
		}
	}
	entry, err := integrations.Approve(m, m.Digest, integrations.Grants{
		Capabilities: m.Capabilities,
		Routes:       subset,
	}, "sergey", time.Now())
	if err != nil {
		t.Fatalf("approving a subset must be allowed: %v", err)
	}
	if len(entry.Routes) != len(subset) {
		t.Fatalf("entry has %d routes, want %d", len(entry.Routes), len(subset))
	}
}
