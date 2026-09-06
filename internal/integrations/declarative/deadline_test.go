// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

const shortTimeoutManifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata:
  id: slow
  name: Slow
  version: 0.1.0
spec:
  runtime: declarative
  slots:
    - name: server
      kind: http
  capabilities: [http]
  limits: { timeoutMs: 100 }
  operations:
    - id: op
      routes:
        - { slot: server, method: GET, path: /slow }
      pipeline:
        - as: r
          request: { slot: server, method: GET, path: /slow }
      output:
        title: Slow
`

// TestInvoke_ManifestTimeoutIsActuallyEnforcedOnTheHTTPCall is the regression test for a real gap
// found in review: the deadline derived from the manifest's approved timeoutMs was computed but
// never attached to the context passed to the broker's HTTP call - a card approved for a 3 s
// timeout could still block for the connection's own, much longer HTTP client timeout. The
// upstream here sleeps for 400 ms; a manifest approved for timeoutMs: 100 must not wait for it.
func TestInvoke_ManifestTimeoutIsActuallyEnforcedOnTheHTTPCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(shortTimeoutManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifestload.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	reg, err := connections.New(map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/slow", Use: capabilities.UseData}
	grant := capabilities.NewGrant("slow", "0.1.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})

	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest, EffectiveLimits: integrations.EffectiveLimits{TimeoutMs: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "op", Params: []byte(`{}`), Grant: grant})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the 100ms manifest timeout to fail this invocation")
	}
	if elapsed > 350*time.Millisecond {
		t.Fatalf("Invoke took %s, want it bounded by the manifest's 100ms timeout, well before the upstream's 400ms sleep", elapsed)
	}
}

// TestInvoke_CallerDeadlineCanOnlyNarrowNeverExtendTheApprovedTimeout is the regression test for
// the deadline formula itself: a caller-supplied InvokeRequest.Deadline used to *replace* the
// manifest's own timeoutMs outright rather than only narrow it - the wrong direction for an
// "effective limit" (docs/01-architecture.md's effective(k) = min(...), never max). A deadline far
// in the future must not grant more time than the manifest was actually approved for.
func TestInvoke_CallerDeadlineCanOnlyNarrowNeverExtendTheApprovedTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(shortTimeoutManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifestload.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := connections.New(map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/slow", Use: capabilities.UseData}
	grant := capabilities.NewGrant("slow", "0.1.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})
	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest, EffectiveLimits: integrations.EffectiveLimits{TimeoutMs: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	// A caller-supplied deadline ten seconds out must not override the manifest's own 100ms.
	_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{
		Operation: "op", Params: []byte(`{}`), Grant: grant, Deadline: time.Now().Add(10 * time.Second),
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the manifest's 100ms timeout to still apply despite a far-future caller deadline")
	}
	if elapsed > 350*time.Millisecond {
		t.Fatalf("Invoke took %s, want it bounded by the manifest's 100ms timeout regardless of the caller's deadline", elapsed)
	}
}
