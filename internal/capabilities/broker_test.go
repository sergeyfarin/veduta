// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// registryAgainst builds a real connections.Registry with one http connection ("server")
// pointed at srv, with the given auth so header/query-stripping tests can prove the broker
// actually strips connection-owned values before the real request is built.
func registryAgainst(t *testing.T, srv *httptest.Server, auth config.ConnectionAuth) connections.Registry {
	t.Helper()
	cfg := map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: auth}},
	}
	reg, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func dataRoute() capabilities.Route {
	return capabilities.Route{Slot: "server", Method: "GET", Path: "/api/stats", Use: capabilities.UseData}
}

func fullGrant(routes []capabilities.Route, caps ...string) capabilities.Grant {
	if caps == nil {
		caps = []string{"http", "cache", "assets", "log", "events"}
	}
	return capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet(caps...), routes, routes, nil, capabilities.Limits{}, capabilities.ExecutionIdentity{})
}

func TestBroker_HTTP_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant([]capabilities.Route{dataRoute()})
	resp, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "ok" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestBroker_HTTP_CapDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant([]capabilities.Route{dataRoute()}, "cache") // no "http" cap
	_, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"})
	if !errors.Is(err, capabilities.ErrCapDenied) {
		t.Fatalf("err = %v, want ErrCapDenied", err)
	}
}

func TestBroker_HTTP_SlotDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant([]capabilities.Route{dataRoute()})
	_, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "not-granted", Method: "GET", Path: "/api/stats"})
	if !errors.Is(err, capabilities.ErrSlotDenied) {
		t.Fatalf("err = %v, want ErrSlotDenied", err)
	}
}

func TestBroker_HTTP_RouteDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant([]capabilities.Route{dataRoute()})
	_, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "DELETE", Path: "/api/stats"})
	if !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("err = %v, want ErrRouteDenied", err)
	}
}

func TestBroker_HTTP_BudgetExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{dataRoute()}, []capabilities.Route{dataRoute()},
		nil, capabilities.Limits{HTTPRequests: 1}, capabilities.ExecutionIdentity{})

	if _, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}); err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}
	if _, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}); !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("second call err = %v, want ErrBudgetExceeded (httpRequests limit of 1)", err)
	}
}

// TestBroker_HTTP_StripsConnectionOwnedAuthAndPluginHeaders proves the stripping actually
// happens on the real request path, not just in isolated header-filter unit tests: a plugin
// tries to override the connection's own auth header and inject a non-allowlisted header; the
// upstream server sees neither.
func TestBroker_HTTP_StripsConnectionOwnedAuthAndPluginHeaders(t *testing.T) {
	var gotAPIKey, gotCustom, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotCustom = r.Header.Get("X-Custom")
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	auth := config.ConnectionAuth{Type: "header", Name: "X-Api-Key", Value: config.SecretRef{Literal: "real-secret"}}
	b := capabilities.NewBroker(registryAgainst(t, srv, auth), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant([]capabilities.Route{dataRoute()})
	_, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{
		Slot: "server", Method: "GET", Path: "/api/stats",
		Header: map[string]string{"X-Api-Key": "plugin-supplied", "X-Custom": "smuggled", "Accept": "application/json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "real-secret" {
		t.Errorf("X-Api-Key = %q, want the connection's own auth value, not the plugin's", gotAPIKey)
	}
	if gotCustom != "" {
		t.Errorf("X-Custom = %q, want empty - not on the plugin allowlist", gotCustom)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want the plugin's allowlisted value to survive", gotAccept)
	}
}

func TestBroker_CacheRoundTripAndNamespacing(t *testing.T) {
	cache := capabilities.NewMemCache()
	b := capabilities.NewBroker(nil, cache, capabilities.NewMemAudit(), nil)

	g1 := capabilities.NewGrant("plug-a", "1.0.0", "inst1", nil, capabilities.NewCapSet("cache"), nil, nil, nil, capabilities.Limits{}, capabilities.ExecutionIdentity{})
	g2 := capabilities.NewGrant("plug-b", "1.0.0", "inst1", nil, capabilities.NewCapSet("cache"), nil, nil, nil, capabilities.Limits{}, capabilities.ExecutionIdentity{})

	if err := b.CachePut(context.Background(), g1, "k", []byte("plug-a's secret"), time.Minute); err != nil {
		t.Fatal(err)
	}
	val, ok, err := b.CacheGet(context.Background(), g1, "k")
	if err != nil || !ok || string(val) != "plug-a's secret" {
		t.Fatalf("own read: val=%q ok=%v err=%v", val, ok, err)
	}

	// TestBroker AC: "cache namespacing prevents cross-plugin reads."
	_, ok, err = b.CacheGet(context.Background(), g2, "k")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a different plugin must not read another plugin's cache entry under the same key")
	}
}

func TestBroker_Cache_CapDenied(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("http"), nil, nil, nil, capabilities.Limits{}, capabilities.ExecutionIdentity{})
	if _, _, err := b.CacheGet(context.Background(), g, "k"); !errors.Is(err, capabilities.ErrCapDenied) {
		t.Fatalf("CacheGet err = %v, want ErrCapDenied", err)
	}
	if err := b.CachePut(context.Background(), g, "k", []byte("v"), 0); !errors.Is(err, capabilities.ErrCapDenied) {
		t.Fatalf("CachePut err = %v, want ErrCapDenied", err)
	}
}

func TestBroker_CachePut_BudgetExceeded(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("cache"), nil, nil, nil,
		capabilities.Limits{CacheEntries: 1}, capabilities.ExecutionIdentity{})
	if err := b.CachePut(context.Background(), g, "k1", []byte("v"), 0); err != nil {
		t.Fatalf("first write should succeed: %v", err)
	}
	if err := b.CachePut(context.Background(), g, "k2", []byte("v"), 0); !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("second write err = %v, want ErrBudgetExceeded (cacheEntries limit of 1)", err)
	}
}

func TestBroker_Log_CapDenied(t *testing.T) {
	audit := capabilities.NewMemAudit()
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), audit, discardLogger())
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("http"), nil, nil, nil, capabilities.Limits{}, capabilities.ExecutionIdentity{})
	if err := b.Log(g, "info", "hello", nil); !errors.Is(err, capabilities.ErrCapDenied) {
		t.Fatalf("Log err = %v, want ErrCapDenied", err)
	}
	if audit.Total("plug") != 1 {
		t.Fatalf("Log without the log capability should still be recorded as a denial, got total=%d", audit.Total("plug"))
	}
}

func TestBroker_Emit_CapDenied(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("http"), nil, nil, nil, capabilities.Limits{}, capabilities.ExecutionIdentity{})
	if err := b.Emit(context.Background(), g, capabilities.Event{Type: "x"}); !errors.Is(err, capabilities.ErrCapDenied) {
		t.Fatalf("err = %v, want ErrCapDenied", err)
	}
}

// TestBroker_HostCallsSharedAcrossMethods is the D2 AC: "A single hostCalls budget across every
// broker method." Exhausting it via Log must then block Emit too.
func TestBroker_HostCallsSharedAcrossMethods(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), discardLogger())
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("log", "events"), nil, nil, nil,
		capabilities.Limits{HostCalls: 1}, capabilities.ExecutionIdentity{})
	if err := b.Log(g, "info", "first", nil); err != nil { // consumes the one hostCall
		t.Fatal(err)
	}
	if err := b.Emit(context.Background(), g, capabilities.Event{Type: "x"}); !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded - hostCalls is shared across Log and Emit", err)
	}
}

// TestBroker_AssetRef_DeniesDataRoute and its converse are the D2 AC: "AssetRef against a
// use: data route denied and HTTP against a use: asset route denied."
func TestBroker_AssetRef_DeniesDataRoute(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/thumb", Use: capabilities.UseData}
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("assets"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{}, capabilities.ExecutionIdentity{})
	_, err := b.AssetRef(context.Background(), g, "server", "/api/thumb", url.Values{}, capabilities.Transform{})
	if !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("err = %v, want ErrRouteDenied - the only matching route is use:data, not use:asset", err)
	}
}

func TestBroker_HTTP_DeniesAssetRoute(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/thumb", Use: capabilities.UseAsset}
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{}, capabilities.ExecutionIdentity{})
	_, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/thumb"})
	if !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("err = %v, want ErrRouteDenied - the only matching route is use:asset, not use:data", err)
	}
}

func TestBroker_AssetRef_HappyPath(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/thumb/*", Use: capabilities.UseAsset}
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("assets"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{}, capabilities.ExecutionIdentity{})
	ref, err := b.AssetRef(context.Background(), g, "server", "/api/thumb/42", url.Values{"size": []string{"preview"}}, capabilities.Transform{Width: 320, Format: "webp"})
	if err != nil {
		t.Fatal(err)
	}
	if ref == "" {
		t.Fatal("want a non-empty token")
	}
}

func TestBroker_AssetRef_InvalidTransformRejected(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	route := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/thumb/*", Use: capabilities.UseAsset}
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("assets"), []capabilities.Route{route}, []capabilities.Route{route}, nil,
		capabilities.Limits{}, capabilities.ExecutionIdentity{})
	_, err := b.AssetRef(context.Background(), g, "server", "/api/thumb/42", nil, capabilities.Transform{Width: 999})
	if err == nil {
		t.Fatal("want an error for a width outside the allowlist (160/320/640/1280)")
	}
}

// TestBroker_HTTP_HostCallsBudgetSpecifically covers the hostCalls check in HTTP's own preamble
// distinctly from httpRequests - the two are different budgets (docs/01-architecture.md: hostCalls
// is shared across every method, httpRequests is HTTP-specific), and the D2 AC calls for 100%
// statement coverage on every broker method's own preamble.
func TestBroker_HTTP_HostCallsBudgetSpecifically(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{dataRoute()}, []capabilities.Route{dataRoute()},
		nil, capabilities.Limits{HostCalls: 1}, capabilities.ExecutionIdentity{})
	if _, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}); err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}
	if _, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"}); !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("second call err = %v, want ErrBudgetExceeded (hostCalls limit of 1)", err)
	}
}

func TestBroker_CachePut_HostCallsBudgetSpecifically(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("cache"), nil, nil, nil,
		capabilities.Limits{HostCalls: 1}, capabilities.ExecutionIdentity{})
	if err := b.CachePut(context.Background(), g, "k1", []byte("v"), 0); err != nil {
		t.Fatalf("first write should succeed: %v", err)
	}
	if err := b.CachePut(context.Background(), g, "k2", []byte("v"), 0); !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("second write err = %v, want ErrBudgetExceeded (hostCalls limit of 1)", err)
	}
}

// TestBroker_HTTP_SlotPointsAtNonHTTPConnection covers the "connection is not an http
// connection" branch: a slot bound to a docker connection cannot be used for HTTP.
func TestBroker_HTTP_SlotPointsAtNonHTTPConnection(t *testing.T) {
	cfg := map[string]config.Connection{
		"conn1": {Kind: "docker", Docker: &config.DockerConnection{Endpoint: "unix:///var/run/docker.sock"}},
	}
	reg, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant([]capabilities.Route{dataRoute()})
	_, err = b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"})
	if err == nil {
		t.Fatal("want an error: the granted slot points at a docker connection, not http")
	}
}

// TestBroker_Log_LevelsAndFields covers Log's level switch and its fields-to-args loop, none of
// which is part of the four-check preamble but all of which this milestone's own package should
// exercise rather than leave silently untested.
func TestBroker_Log_LevelsAndFields(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), discardLogger())
	g := fullGrant(nil)
	for _, level := range []string{"info", "warn", "error", "debug", "unknown-defaults-to-info"} {
		if err := b.Log(g, level, "message", map[string]any{"key": "value"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBroker_Emit_HappyPath(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), discardLogger())
	g := fullGrant(nil)
	if err := b.Emit(context.Background(), g, capabilities.Event{Type: "card-error", Data: map[string]any{"card": "x"}}); err != nil {
		t.Fatal(err)
	}
}

// TestBroker_CachePut_BytesBudgetSpecifically covers cacheBytesKB distinctly from cacheEntries -
// few large writes exhaust bytes before they exhaust the entry count.
func TestBroker_CachePut_BytesBudgetSpecifically(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", nil, capabilities.NewCapSet("cache"), nil, nil, nil,
		capabilities.Limits{CacheEntries: 10, CacheBytesKB: 1}, capabilities.ExecutionIdentity{})
	if err := b.CachePut(context.Background(), g, "k1", make([]byte, 512), 0); err != nil {
		t.Fatalf("first write (0.5KB, within the 1KB budget) should succeed: %v", err)
	}
	if err := b.CachePut(context.Background(), g, "k2", make([]byte, 700), 0); !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("second write (would push total past 1KB) err = %v, want ErrBudgetExceeded", err)
	}
}

// TestBroker_HTTP_ResponseMBEnforced is the regression test for a real gap found in review:
// Limits.ResponseMB was documented as a per-response ceiling the broker enforces, narrower than
// the connection's own MaxResponseBytes, but nothing ever actually read the field - an approval
// with a small ResponseMB got no narrower ceiling than whatever the connection itself allowed.
func TestBroker_HTTP_ResponseMBEnforced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 2<<20)) // 2 MiB
	}))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{dataRoute()}, []capabilities.Route{dataRoute()}, nil,
		capabilities.Limits{ResponseMB: 1}, capabilities.ExecutionIdentity{})
	_, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"})
	if !errors.Is(err, capabilities.ErrBudgetExceeded) {
		t.Fatalf("got %v, want ErrBudgetExceeded for a 2 MiB response against a 1 MB limit", err)
	}
}

func TestBroker_HTTP_ResponseMBAllowsUnderTheLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 10))
	}))
	defer srv.Close()
	b := capabilities.NewBroker(registryAgainst(t, srv, config.ConnectionAuth{Type: "none"}), capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := capabilities.NewGrant("plug", "1.0.0", "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), []capabilities.Route{dataRoute()}, []capabilities.Route{dataRoute()}, nil,
		capabilities.Limits{ResponseMB: 1}, capabilities.ExecutionIdentity{})
	resp, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/stats"})
	if err != nil {
		t.Fatalf("unexpected error for a 10-byte response under a 1 MB limit: %v", err)
	}
	if len(resp.Body) != 10 {
		t.Fatalf("body = %d bytes, want 10", len(resp.Body))
	}
}

// TestBroker_HTTP_RedirectToAnUnapprovedRouteIsDenied is the regression test for a real gap
// found in review: connections.redirectPolicy could check host, scheme and a connection's own
// allowedPaths on a redirect hop, but had no way to re-run the manifest/lock route grant itself,
// since only the broker holds the Grant. An approved GET /api/public that redirects to
// GET /api/admin - a route this Grant never approved - must not be silently followed just
// because allowedPaths is unset (nil, meaning "any path on this connection", the deliberate
// default for a connection with no allowedPaths configured at all).
func TestBroker_HTTP_RedirectToAnUnapprovedRouteIsDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/public" {
			http.Redirect(w, r, "/api/admin", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	cfg := map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}, MaxRedirects: 3}},
	}
	reg, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	publicRoute := capabilities.Route{Slot: "server", Method: "GET", Path: "/api/public", Use: capabilities.UseData}
	g := fullGrant([]capabilities.Route{publicRoute})

	_, err = b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/public"})
	if !errors.Is(err, capabilities.ErrRouteDenied) {
		t.Fatalf("err = %v, want ErrRouteDenied - the redirect to /api/admin was never approved", err)
	}
}

// TestBroker_HTTP_RedirectToAnApprovedRouteSucceeds proves the fix above is a real recheck, not
// a blanket "redirects are now denied": a redirect landing on a route the same Grant also
// approves must still succeed.
func TestBroker_HTTP_RedirectToAnApprovedRouteSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/public" {
			http.Redirect(w, r, "/api/public-v2", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}, MaxRedirects: 3}},
	}
	reg, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	routes := []capabilities.Route{
		{Slot: "server", Method: "GET", Path: "/api/public", Use: capabilities.UseData},
		{Slot: "server", Method: "GET", Path: "/api/public-v2", Use: capabilities.UseData},
	}
	g := fullGrant(routes)

	resp, err := b.HTTP(context.Background(), g, capabilities.HTTPRequest{Slot: "server", Method: "GET", Path: "/api/public"})
	if err != nil {
		t.Fatalf("unexpected error following a redirect to an equally-approved route: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "ok" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestBroker_Cache_ExpiredEntryIsGone(t *testing.T) {
	b := capabilities.NewBroker(nil, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	g := fullGrant(nil)
	if err := b.CachePut(context.Background(), g, "k", []byte("v"), 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	_, ok, err := b.CacheGet(context.Background(), g, "k")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("an expired entry should no longer be readable")
	}
}
