// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// TestGlances_RealBrokerEndToEnd runs the real, checked-in plugins/glances/manifest.yaml through
// the real capabilities.Broker (real connections.Registry, real Grant.Authorize) rather than the
// permissive fixtureBroker the other tests in this package use. That fake accepts any request
// regardless of what Grant or HTTPRequest is passed, so it cannot catch a mismatch between what
// D3 actually constructs (method/path/query/header shape) and what a real Grant's ManifestRoutes/
// ApprovedRoutes/ConnectionPolicy would authorise - this test closes that gap by exercising the
// same three-independent-checks path D2 already tests in isolation, with D3's real pipeline
// output as the input.
func TestGlances_RealBrokerEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{
			"/api/4/cpu":     `{"total":25}`,
			"/api/4/mem":     `{"percent":40}`,
			"/api/4/fs":      `[{"mnt_point":"/","percent":50}]`,
			"/api/4/uptime":  `"1:27:01"`,
			"/api/4/network": `[{"interface_name":"eth0","bytes_all_rate_per_sec":2048}]`,
			"/api/4/sensors": `[{"label":"CPU","type":"temperature_core","value":52}]`,
		}[r.URL.Path]
		if body == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "glances", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	routes := make([]capabilities.Route, len(m.Operations[0].Routes))
	for i, r := range m.Operations[0].Routes {
		routes[i] = capabilities.Route{Slot: r.Slot, Method: r.Method, Path: r.Path, Use: capabilities.UseData}
	}

	reg, err := connections.New(map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	grant := capabilities.NewGrant("glances", m.Version, "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), routes, routes, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})

	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "overview", Params: []byte(`{}`), Grant: grant})
	if err != nil {
		t.Fatalf("Invoke through the real broker: %v", err)
	}
	if resp.Document.Title != "Host" || len(resp.Document.Blocks) != 4 {
		t.Fatalf("unexpected document: %#v", resp.Document)
	}
}

// TestGlances_RealBrokerEndToEnd_DeniesUnapprovedRoute proves the real Grant is actually
// consulted, not merely plumbed through unused: dropping one of glances' six routes from
// ApprovedRoutes must make the pipeline step calling it fail with the broker's own route denial.
func TestGlances_RealBrokerEndToEnd_DeniesUnapprovedRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":25}`))
	}))
	defer srv.Close()

	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "glances", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifestRoutes := make([]capabilities.Route, len(m.Operations[0].Routes))
	for i, r := range m.Operations[0].Routes {
		manifestRoutes[i] = capabilities.Route{Slot: r.Slot, Method: r.Method, Path: r.Path, Use: capabilities.UseData}
	}
	// Approve only the first route (cpu) - not the rest the manifest's pipeline also calls.
	approved := manifestRoutes[:1]

	reg, err := connections.New(map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	grant := capabilities.NewGrant("glances", m.Version, "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), manifestRoutes, approved, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})

	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "overview", Params: []byte(`{}`), Grant: grant})
	if err == nil {
		t.Fatal("expected the second pipeline step's route denial to fail the invocation")
	}
}
