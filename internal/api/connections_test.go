// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"veduta.dev/veduta/internal/api"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/secrets"
)

// connectionsServer builds a real *api.Server with two connections: one healthy (a real
// httptest.Server) and one whose credential must never appear in any response - the exact
// scenario D5's own AC guards against ("the response contains no credentials, asserted by
// scanning the JSON for every configured secret value").
func connectionsServer(t *testing.T, upstream *httptest.Server) (*api.Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	body := `version: 1
auth: {mode: none}
connections:
  healthy:
    kind: http
    baseUrl: ` + upstream.URL + `
    auth: {type: none}
  broken:
    kind: http
    baseUrl: http://192.0.2.1:81
    auth: {type: query, name: api_key, value: "${secret:BROKEN_KEY}"}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BROKEN_KEY", "leaked-if-this-appears-anywhere-in-a-response")

	store, diags := config.Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	resolved, diags := secrets.ResolveAll(store.Snapshot().SecretRefs, secrets.DefaultResolver())
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	reg, err := connections.New(store.Snapshot().Config.Connections, resolved, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	return s, "leaked-if-this-appears-anywhere-in-a-response"
}

func TestConnectionsList_NeverLeaksAConfiguredSecret(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()

	s, secretValue := connectionsServer(t, upstream)
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil).WithContext(ctx)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretValue) {
		t.Fatalf("response leaked the configured secret verbatim: %s", rec.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d connections, want 2", len(got))
	}
	byID := map[string]map[string]any{}
	for _, c := range got {
		byID[c["id"].(string)] = c
	}
	if byID["healthy"] == nil || byID["broken"] == nil {
		t.Fatalf("expected both connections listed, got %+v", byID)
	}
	healthyHealth := byID["healthy"]["health"].(map[string]any)
	if healthyHealth["reachable"] != true {
		t.Errorf("healthy connection reported unreachable: %+v", healthyHealth)
	}
	brokenHealth := byID["broken"]["health"].(map[string]any)
	if brokenHealth["reachable"] == true {
		t.Errorf("broken connection reported reachable: %+v", brokenHealth)
	}
	// No field anywhere in the response is allowed to be named after a credential concept either
	// - "never credentials" means the shape itself, not merely that today's values look opaque.
	for _, forbidden := range []string{"baseUrl", "auth", "value", "password"} {
		if _, ok := byID["broken"][forbidden]; ok {
			t.Errorf("response includes a %q field, which should not exist on a connection summary", forbidden)
		}
	}
}

// hangingServer accepts TCP connections but never responds - a deterministic stand-in for
// "unreachable" that actually consumes wall-clock time until the caller's context gives up,
// unlike a dial to a reserved/unrouted address (which CI's network stack may fail immediately,
// unlike a developer machine's). Used to prove one connection's health check cannot starve
// another's, regardless of how the environment happens to fail a dead address.
func hangingServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				<-done
				_ = c.Close()
			}(conn)
		}
	}()
	return "http://" + ln.Addr().String()
}

// TestConnectionsList_OneSlowConnectionDoesNotStarveAnothersHealthCheck is the regression test
// for a real bug found in review: GET /api/v1/connections checked every connection sequentially,
// all sharing r.Context() - a connection that hangs until the context's own deadline expires (as
// a real network black hole would, not every environment fails a dead address quickly) consumed
// the whole remaining time budget, so any connection checked after it inherited an
// already-expired context and was wrongly reported unreachable for a reason having nothing to do
// with its own health. Health checks now run concurrently (connections.go's routeConnections),
// so the fast connection here must come back healthy regardless of where it sorts relative to
// the hanging one.
func TestConnectionsList_OneSlowConnectionDoesNotStarveAnothersHealthCheck(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer fast.Close()
	slowURL := hangingServer(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	body := `version: 1
auth: {mode: none}
connections:
  aaa-slow:
    kind: http
    baseUrl: ` + slowURL + `
    auth: {type: none}
  zzz-fast:
    kind: http
    baseUrl: ` + fast.URL + `
    auth: {type: none}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store, diags := config.Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	reg, err := connections.New(store.Snapshot().Config.Connections, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil).WithContext(ctx)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, c := range got {
		byID[c["id"].(string)] = c
	}
	fastHealth := byID["zzz-fast"]["health"].(map[string]any)
	if fastHealth["reachable"] != true {
		t.Fatalf("fast connection (sorted after the hanging one) reported unreachable: %+v", fastHealth)
	}
}

func TestConnectionsTest_UnknownIDIs404(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	s, _ := connectionsServer(t, upstream)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/connections/nope/test", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestConnectionsTest_HealthyConnection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	s, _ := connectionsServer(t, upstream)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/connections/healthy/test", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	health := got["health"].(map[string]any)
	if health["reachable"] != true {
		t.Fatalf("health = %+v, want reachable", health)
	}
}
