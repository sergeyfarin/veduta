// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
	wasmrt "veduta.dev/veduta/internal/integrations/wasm"
)

type jellyfinBroker struct {
	t       *testing.T
	dataDir string
	calls   int
}

func (b *jellyfinBroker) HTTP(_ context.Context, _ capabilities.Grant, request capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	b.t.Helper()
	b.calls++
	if request.Header["Accept"] != `application/json; profile="CamelCase"` {
		b.t.Errorf("Accept = %q", request.Header["Accept"])
	}
	fixture := ""
	switch request.Path {
	case "/Users/Me":
		fixture = "user.json"
	case "/Items/Latest":
		fixture = "latest.json"
		wantQuery(b.t, request.Query, "userId", "user-1", "limit", "5", "fields", "ProductionYear,ImageTags", "imageTypeLimit", "1", "enableImageTypes", "Primary")
	case "/Items":
		wantQuery(b.t, request.Query, "userId", "user-1", "recursive", "true", "limit", "1")
		switch request.Query["includeItemTypes"] {
		case "Movie":
			fixture = "movies.json"
		case "Series":
			fixture = "shows.json"
		default:
			b.t.Fatalf("unexpected item type %q", request.Query["includeItemTypes"])
		}
	case "/Sessions":
		fixture = "sessions.json"
	default:
		b.t.Fatalf("unexpected Jellyfin request %s", request.Path)
	}
	body, err := os.ReadFile(filepath.Join(b.dataDir, fixture))
	return capabilities.HTTPResponse{StatusCode: 200, Body: body}, err
}

func wantQuery(t *testing.T, got map[string]string, pairs ...string) {
	t.Helper()
	for i := 0; i < len(pairs); i += 2 {
		if got[pairs[i]] != pairs[i+1] {
			t.Errorf("query[%s] = %q, want %q", pairs[i], got[pairs[i]], pairs[i+1])
		}
	}
}

func (*jellyfinBroker) CacheGet(context.Context, capabilities.Grant, string) ([]byte, bool, error) {
	return nil, false, errors.New("unexpected cache call")
}
func (*jellyfinBroker) CachePut(context.Context, capabilities.Grant, string, []byte, time.Duration) error {
	return errors.New("unexpected cache call")
}
func (b *jellyfinBroker) AssetRef(_ context.Context, _ capabilities.Grant, _ string, path string, _ url.Values, transform capabilities.Transform) (string, error) {
	b.t.Helper()
	if transform.Width != 320 || transform.Format != "" {
		b.t.Errorf("transform = %+v", transform)
	}
	id := strings.Split(path, "/")[2]
	refs := map[string]string{
		"a1": "v1.eyJjIjoiamVsbHlmaW4ifQ.9f2c1a7b",
		"a2": "v1.YTI.c2lnMg",
		"a3": "v1.YTM.c2lnMw",
		"a4": "v1.YTQ.c2lnNA",
		"a5": "v1.YTU.c2lnNQ",
	}
	ref, ok := refs[id]
	if !ok || path != "/Items/"+id+"/Images/Primary" {
		return "", errors.New("unexpected asset path " + path)
	}
	return ref, nil
}
func (*jellyfinBroker) Log(capabilities.Grant, string, string, map[string]any) error {
	return errors.New("unexpected log call")
}
func (*jellyfinBroker) Emit(context.Context, capabilities.Grant, capabilities.Event) error {
	return errors.New("unexpected event call")
}

func TestJellyfinPluginMatchesGoldenDocument(t *testing.T) {
	pluginDir := filepath.Join("..", "..", "..", "plugins", "jellyfin")
	manifest, err := manifestload.Load(filepath.Join(pluginDir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	limits := integrations.EffectiveLimits{MemoryMB: 64, TimeoutMs: 3000, OutputKB: 64, HTTPRequests: 5, ResponseMB: 4, CacheEntries: 64, InputMB: 4, JSONDepth: 32, JSONNodes: 200000, ExprNodes: 512, Iterations: 20000, RequestBodyKB: 64, HostCalls: 10, CacheBytesKB: 256}
	routes := manifestRoutes(manifest)
	lock := &integrations.LockEntry{ManifestSHA256: manifest.Digest, ModuleSHA256: manifest.ModuleSHA256, Version: manifest.Version, Runtime: "wasm", Capabilities: []string{"http", "assets"}, Routes: lockRoutes(routes), EffectiveLimits: limits}
	broker := &jellyfinBroker{t: t, dataDir: filepath.Join(pluginDir, "testdata")}
	runtime, err := wasmrt.NewWithBroker(context.Background(), filepath.Join(t.TempDir(), "cache"), broker)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close(context.Background()) }()
	instance, err := runtime.Load(context.Background(), integrations.Installed{Manifest: manifest, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	grant := capabilities.NewGrant("jellyfin", "1.0.0", "jellyfin-recent", map[string]string{"server": "jellyfin"}, capabilities.NewCapSet("http", "assets"), routes, routes, nil, capabilities.Limits{HTTPRequests: 5, ResponseMB: 4, HostCalls: 10}, capabilities.ExecutionIdentity{})
	response, err := instance.Invoke(context.Background(), integrations.InvokeRequest{Operation: "recently-added", Params: json.RawMessage(`{"limit":5}`), Grant: grant})
	if err != nil {
		t.Fatal(err)
	}
	if broker.calls != 5 {
		t.Fatalf("HTTP calls = %d, want 5", broker.calls)
	}
	got, err := json.Marshal(response.Document)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "widgets", "jellyfin-recent.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if json.Unmarshal(got, &gotValue) != nil || json.Unmarshal(want, &wantValue) != nil {
		t.Fatal("golden or plugin output is not JSON")
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("Jellyfin document does not match golden\ngot: %s\nwant: %s", got, want)
	}
}

func manifestRoutes(manifest *manifestload.Manifest) []capabilities.Route {
	var routes []capabilities.Route
	for _, operation := range manifest.Operations {
		for _, route := range operation.Routes {
			use := capabilities.UseData
			if route.Use != "" {
				use = capabilities.UseKind(route.Use)
			}
			routes = append(routes, capabilities.Route{Slot: route.Slot, Method: route.Method, Path: route.Path, Use: use, QueryKeys: route.QueryKeys, ContentType: route.ContentType, MaxBodyKB: route.MaxBodyKB})
		}
	}
	return routes
}

func lockRoutes(routes []capabilities.Route) []integrations.Route {
	out := make([]integrations.Route, 0, len(routes))
	for _, route := range routes {
		out = append(out, integrations.Route{Slot: route.Slot, Method: route.Method, Path: route.Path, Use: string(route.Use), QueryKeys: route.QueryKeys, ContentType: route.ContentType, MaxBodyKB: route.MaxBodyKB})
	}
	return out
}
