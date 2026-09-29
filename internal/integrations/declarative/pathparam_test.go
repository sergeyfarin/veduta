// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"encoding/json"
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

// The params schema below deliberately has no pattern: the values refused here are refused by the
// path template itself, which is the guarantee the loader's coverage proof rests on. A manifest's
// own pattern is a courtesy to the operator, not the boundary.
const pathParamManifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata: { id: pathparam, name: Path param, version: 0.1.0 }
spec:
  runtime: declarative
  slots: [{ name: server, kind: http }]
  capabilities: [http]
  operations:
    - id: item
      routes:
        - { slot: server, method: GET, path: /items/*/detail, queryKeys: [] }
      params:
        type: object
        required: [id]
        properties: { id: { type: [string, integer] } }
      pipeline:
        - as: item
          request: { slot: server, method: GET, path: "/items/{id}/detail" }
      output:
        title: { expr: item.name }
        blocks: []
`

func TestPathParameterSelectsOneSegmentAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(strings.Replace(pathParamManifest, "type: [string, integer]", "type: string", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifestload.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"name":"found"}`))
	}))
	defer srv.Close()
	reg, err := connections.New(map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	op := m.Operations[0]
	routes := []capabilities.Route{{Slot: "server", Method: "GET", Path: op.Routes[0].Path, QueryKeys: []string{}, Use: capabilities.UseData}}
	grant := capabilities.NewGrant(m.ID, m.Version, "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), routes, routes, nil,
		capabilities.Limits{HTTPRequests: 8, ResponseMB: 1, HostCalls: 100}, capabilities.ExecutionIdentity{})
	inst, err := declarative.New(capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)).
		Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(id any) error {
		params, _ := json.Marshal(map[string]any{"id": id})
		_, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "item", Params: params, Grant: grant})
		return err
	}

	if err := invoke("sensor.outside"); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0] != "/items/sensor.outside/detail" {
		t.Fatalf("upstream saw %v, want exactly /items/sensor.outside/detail", hits)
	}

	// Each would otherwise walk off the approved route, change the segment count, or smuggle a
	// query or fragment. None may produce a request at all. Most are also refused further down,
	// by the broker's canonicalisation; the last four are not - they are ordinary path characters
	// the upstream's router may read as structure (matrix parameters, globs) - so they prove the
	// template's own value rule is in force, not only the layer beneath it.
	hits = nil
	for _, bad := range []string{"a/b", "..", ".", "", "x?admin=1", "x#", "%2e%2e", "a%2fb", "a b", "a;b", "a*b", "a=b", "a,b"} {
		if err := invoke(bad); err == nil {
			t.Errorf("id %q was accepted", bad)
		}
	}
	if len(hits) != 0 {
		t.Fatalf("refused values still reached the upstream as %v", hits)
	}
}
