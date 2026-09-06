// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// dynamicOutputManifest's output is a single {expr} node evaluating to the whole document,
// including the entire "signals" object, rather than an object-shaped output with per-signal
// {expr} children. manifestload's own compileTemplate only sees an object's keys as static (and
// thus checkable at load time) when the manifest source itself writes an object literal - here
// the manifest never does, so the compiler has no static "signals" key to check against
// op.Signals at all. This is the concrete, previously-disputed case: a first review pass on this
// runtime concluded dynamic signal keys were not reachable in the grammar and that the runtime's
// own post-evaluation declared-signal check (runtime.go, right after widgets.ValidateWithLimit)
// was defensive-only; a second pass correctly challenged that as wrong, and this test is the
// proof - the pipeline's own upstream response controls the actual signal names that reach
// widgets.Document, and only the runtime check (not manifestload's static one) can catch an
// undeclared one.
const dynamicOutputManifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata:
  id: dynout
  name: DynOut
  version: 0.1.0
spec:
  runtime: declarative
  slots:
    - name: server
      kind: http
  capabilities: [http]
  operations:
    - id: op
      signals:
        - { name: known, type: number }
      routes:
        - { slot: server, method: GET, path: /data }
      pipeline:
        - as: r
          request: { slot: server, method: GET, path: /data }
      output:
        expr: '{"title": "dyn", "blocks": [], "signals": r}'
`

func loadDynamicOutputInstance(t *testing.T, upstreamBody string) integrations.Instance {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamBody))
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(dynamicOutputManifest), 0o600); err != nil {
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
	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// TestInvoke_DynamicOutputEmittingAnUndeclaredSignalIsRejected is the regression test for the
// dynamic-signal-key case: the whole "signals" object comes from an upstream-controlled value
// via a single {expr} node, not a manifest-literal object manifestload could check the keys of
// at load time. An upstream response naming a signal the manifest never declared must still be
// rejected by the runtime's own post-evaluation check.
func TestInvoke_DynamicOutputEmittingAnUndeclaredSignalIsRejected(t *testing.T) {
	inst := loadDynamicOutputInstance(t, `{"totally_undeclared": {"value": 42}}`)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/data", Use: capabilities.UseData}
	grant := capabilities.NewGrant("dynout", "0.1.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})
	_, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "op", Params: []byte(`{}`), Grant: grant})
	if err == nil {
		t.Fatal("expected Invoke to reject an upstream-controlled signal name the manifest never declared")
	}
}

// TestInvoke_DynamicOutputWithOnlyDeclaredSignalsSucceeds proves the check above is a real
// name check, not a blanket rejection of dynamic output: the same manifest, with an upstream
// response naming only the declared "known" signal, must succeed.
func TestInvoke_DynamicOutputWithOnlyDeclaredSignalsSucceeds(t *testing.T) {
	inst := loadDynamicOutputInstance(t, `{"known": {"value": 1}}`)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/data", Use: capabilities.UseData}
	grant := capabilities.NewGrant("dynout", "0.1.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{HTTPRequests: 10, ResponseMB: 4, HostCalls: 100}, capabilities.ExecutionIdentity{})
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "op", Params: []byte(`{}`), Grant: grant})
	if err != nil {
		t.Fatalf("unexpected error for an upstream response naming only the declared signal: %v", err)
	}
	if _, ok := resp.Document.Signals["known"]; !ok {
		t.Fatalf("expected the declared signal %q in the resulting document, got %+v", "known", resp.Document.Signals)
	}
}
