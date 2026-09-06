// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

const bigTextManifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata:
  id: big
  name: Big
  version: 0.1.0
spec:
  runtime: declarative
  slots:
    - name: server
      kind: http
  capabilities: [http]
  operations:
    - id: op
      routes:
        - { slot: server, method: GET, path: /x }
      pipeline:
        - as: r
          request: { slot: server, method: GET, path: /x }
      output:
        title: Big
        blocks:
          - type: text
            content: { expr: r.text }
`

// TestInvoke_ApprovedOutputKBIsActuallyEnforced is the regression test for a real gap found in
// review: the manifest's approved outputKB (EffectiveLimits.OutputKB) was reconciled but never
// threaded into widgets.Validate, which only ever checked its own hardcoded 64 KiB default - a
// narrower approval had no effect. A 2 KiB document must be rejected under a 1 KiB approval, even
// though it would pass the 64 KiB default undisturbed.
func TestInvoke_ApprovedOutputKBIsActuallyEnforced(t *testing.T) {
	bigText := strings.Repeat("x", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"` + bigText + `"}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(bigTextManifest), 0o600); err != nil {
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
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/x", Use: capabilities.UseData}
	grant := capabilities.NewGrant("big", "0.1.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})

	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest, EffectiveLimits: integrations.EffectiveLimits{OutputKB: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "op", Params: []byte(`{}`), Grant: grant})
	if err == nil {
		t.Fatal("expected the 1 KiB approved outputKB to reject a ~2 KiB document")
	}
}
