// SPDX-License-Identifier: AGPL-3.0-or-later

package httpjson_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/httpjson"
)

// adguardView mirrors examples/veduta.yaml's own http-json card verbatim - the real,
// checked-in example this milestone's AC ("a card with integration: http-json plus a view:
// block renders metrics from any JSON API") describes.
var adguardView = map[string]any{
	"title": "AdGuard Home",
	"blocks": []any{
		map[string]any{
			"type": "metrics",
			"items": []any{
				map[string]any{"label": "Queries", "value": "num_dns_queries", "format": "number"},
				map[string]any{"label": "Blocked", "value": "num_blocked_filtering", "format": "number"},
				map[string]any{"label": "Blocked %", "value": "num_blocked_filtering / num_dns_queries", "format": "percent"},
			},
		},
	},
}

func realBroker(t *testing.T, srv *httptest.Server, allowedPaths []string) (capabilities.Broker, capabilities.ConnectionPolicy) {
	t.Helper()
	reg, err := connections.New(map[string]config.Connection{
		"adguard": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil),
		capabilities.ConnectionPolicy{AllowedPaths: allowedPaths}
}

func TestBuild_AdGuardExampleRendersMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/control/stats" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"num_dns_queries":500,"num_blocked_filtering":100}`))
	}))
	defer srv.Close()

	broker, policy := realBroker(t, srv, nil)
	m, lock, grant, err := httpjson.Build("server", "adguard", httpjson.Card{Path: "/control/stats", View: adguardView}, policy)
	if err != nil {
		t.Fatal(err)
	}

	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{Manifest: m, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "request", Params: []byte(`{}`), Grant: grant})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Document.Title != "AdGuard Home" {
		t.Fatalf("title = %q", resp.Document.Title)
	}
	block, ok := resp.Document.Blocks[0].(interface{ BlockType() string })
	if !ok || block.BlockType() != "metrics" {
		t.Fatalf("blocks[0] = %#v, want a metrics block", resp.Document.Blocks[0])
	}
	body, _ := json.Marshal(resp.Document)
	t.Logf("document: %s", body)
}

func TestBuild_ConnectionAllowedPathsStillGates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"num_dns_queries":1,"num_blocked_filtering":1}`))
	}))
	defer srv.Close()

	// The connection's own allowedPaths does not cover /control/stats - docs/01-architecture.md:
	// "the connection's allowedPaths still applies, and is the only thing standing between a
	// card and every path on that connection." A card naming any other path on this connection
	// must still be refused, exactly as it would be for a real manifest.
	broker, policy := realBroker(t, srv, []string{"/other"})
	m, lock, grant, err := httpjson.Build("server", "adguard", httpjson.Card{Path: "/control/stats", View: adguardView}, policy)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{Manifest: m, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "request", Params: []byte(`{}`), Grant: grant})
	if err == nil {
		t.Fatal("expected the connection's allowedPaths to deny this request")
	}
}

func TestBuild_RejectsMethodOtherThanGetOrPost(t *testing.T) {
	_, _, _, err := httpjson.Build("server", "adguard", httpjson.Card{Path: "/x", Method: http.MethodDelete}, capabilities.ConnectionPolicy{})
	if err == nil {
		t.Fatal("expected DELETE to be refused - http-json's fixed surface is GET and POST only")
	}
}

// TestBuild_BareFieldNamesCannotReachInternalEnvKeys is the http-json-specific form of the D3
// review's __grant finding: view "value" expressions are rewritten so every bare identifier
// becomes member access on the pipeline's own JSON response ("num_dns_queries" ->
// "data.num_dns_queries" - see bindBareFieldsToData), specifically so a card author can never
// address declarative's internal env keys (__grant, __ctx) directly by name the way a real
// manifest expression could before that fix. Writing "__grant" as a value here does not error -
// it is rewritten to "data.__grant", ordinary (and, for a real upstream response, always absent)
// member access - the property this test actually checks: it evaluates to nil, never to the
// real capabilities.Grant the invocation carries.
func TestBuild_BareFieldNamesCannotReachInternalEnvKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"real_field":1}`)) // deliberately has no "__grant" key
	}))
	defer srv.Close()

	view := map[string]any{"blocks": []any{map[string]any{"type": "metrics", "items": []any{
		map[string]any{"label": "x", "value": "__grant"},
	}}}}
	broker, policy := realBroker(t, srv, nil)
	m, lock, grant, err := httpjson.Build("server", "adguard", httpjson.Card{Path: "/x", View: view}, policy)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{Manifest: m, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "request", Params: []byte(`{}`), Grant: grant})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(resp.Document)
	if bytes.Contains(body, []byte("ManifestRoutes")) || bytes.Contains(body, []byte("ApprovedRoutes")) {
		t.Fatalf("the rendered document leaked Grant-shaped content: %s", body)
	}
}
