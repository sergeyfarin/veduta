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
	// scenarioDir, when set, is consulted for each fixture before dataDir, so a scenario file
	// need only contain the responses whose shape it is actually varying.
	scenarioDir string
	// failPath and failStatus make one endpoint answer non-2xx, to pin down whether a single
	// failed sub-request degrades the document or fails the whole invocation.
	failPath   string
	failStatus int
	// wantLimit is the limit the list request is expected to carry; "" means the default 5.
	wantLimit  string
	calls      int
	assetPaths []string
}

func (b *jellyfinBroker) HTTP(_ context.Context, _ capabilities.Grant, request capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	b.t.Helper()
	b.calls++
	if request.Header["Accept"] != `application/json; profile="CamelCase"` {
		b.t.Errorf("Accept = %q", request.Header["Accept"])
	}
	if b.wantLimit == "" {
		b.wantLimit = "5"
	}
	if b.failPath != "" && request.Path == b.failPath {
		return capabilities.HTTPResponse{StatusCode: b.failStatus, Body: []byte(`{"error":"upstream said no"}`)}, nil
	}
	fixture := ""
	switch request.Path {
	case "/Items":
		// A Jellyfin API key has no associated user, so there is no /Users/Me
		// step: recently-added is one sorted /Items query, counts are the same
		// endpoint with a type filter. The list request is the one that sorts.
		switch request.Query["includeItemTypes"] {
		case "Movie,Series":
			fixture = "recent.json"
			// Asserted as the whole map, not a subset: an extra query key is a different
			// upstream request and a different route grant, so a declarative rewrite has to
			// reproduce the set exactly, not merely the keys someone remembered to check.
			wantQuery(b.t, "list", request.Query, map[string]string{
				"recursive": "true", "includeItemTypes": "Movie,Series", "sortBy": "DateCreated",
				"sortOrder": "Descending", "limit": b.wantLimit, "fields": "ProductionYear",
				"imageTypeLimit": "1", "enableImageTypes": "Primary", "enableImages": "true",
			})
		case "Movie":
			fixture = "movies.json"
			wantQuery(b.t, "movie count", request.Query, map[string]string{"recursive": "true", "includeItemTypes": "Movie", "limit": "1"})
		case "Series":
			fixture = "shows.json"
			wantQuery(b.t, "series count", request.Query, map[string]string{"recursive": "true", "includeItemTypes": "Series", "limit": "1"})
		default:
			b.t.Fatalf("unexpected item type %q", request.Query["includeItemTypes"])
		}
	case "/Sessions":
		fixture = "sessions.json"
		wantQuery(b.t, "sessions", request.Query, map[string]string{})
	default:
		b.t.Fatalf("unexpected Jellyfin request %s", request.Path)
	}
	body, err := os.ReadFile(b.fixture(fixture))
	return capabilities.HTTPResponse{StatusCode: 200, Body: body}, err
}

func (b *jellyfinBroker) fixture(name string) string {
	if b.scenarioDir != "" {
		scoped := filepath.Join(b.scenarioDir, name)
		if _, err := os.Stat(scoped); err == nil {
			return scoped
		}
	}
	return filepath.Join(b.dataDir, name)
}

func wantQuery(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	if got == nil {
		got = map[string]string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s query = %v, want %v", label, got, want)
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
	b.assetPaths = append(b.assetPaths, path)
	id := strings.Split(path, "/")[2]
	if path != "/Items/"+id+"/Images/Primary" {
		return "", errors.New("unexpected asset path " + path)
	}
	// The golden document pins these five refs; scenario fixtures use other item ids and only
	// need a ref that is stable and traceable back to the item that asked for it.
	refs := map[string]string{
		"a1": "v1.eyJjIjoiamVsbHlmaW4ifQ.9f2c1a7b",
		"a2": "v1.YTI.c2lnMg",
		"a3": "v1.YTM.c2lnMw",
		"a4": "v1.YTQ.c2lnNA",
		"a5": "v1.YTU.c2lnNQ",
	}
	if ref, ok := refs[id]; ok {
		return ref, nil
	}
	return "v1.test." + id, nil
}
func (*jellyfinBroker) Log(capabilities.Grant, string, string, map[string]any) error {
	return errors.New("unexpected log call")
}
func (*jellyfinBroker) Emit(context.Context, capabilities.Grant, capabilities.Event) error {
	return errors.New("unexpected event call")
}

const jellyfinPluginDir = "../../../plugins/jellyfin"

// invokeJellyfin runs the real plugin against broker and returns whatever the invocation produced,
// error included: the scenario tests exist precisely to pin down which upstream shapes fail.
func invokeJellyfin(t *testing.T, broker *jellyfinBroker, params string) (integrations.InvokeResponse, error) {
	t.Helper()
	manifest, err := manifestload.Load(filepath.Join(jellyfinPluginDir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	limits := integrations.EffectiveLimits{MemoryMB: 64, TimeoutMs: 3000, OutputKB: 64, HTTPRequests: 4, ResponseMB: 4, CacheEntries: 64, InputMB: 4, JSONDepth: 32, JSONNodes: 200000, ExprNodes: 512, Iterations: 20000, RequestBodyKB: 64, HostCalls: 10, CacheBytesKB: 256}
	routes := manifestRoutes(manifest)
	lock := &integrations.LockEntry{ManifestSHA256: manifest.Digest, ModuleSHA256: manifest.ModuleSHA256, Version: manifest.Version, Runtime: "wasm", Capabilities: []string{"http", "assets"}, Routes: lockRoutes(routes), EffectiveLimits: limits}
	runtime, err := wasmrt.NewWithBroker(context.Background(), filepath.Join(t.TempDir(), "cache"), broker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	instance, err := runtime.Load(context.Background(), integrations.Installed{Manifest: manifest, Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	grant := capabilities.NewGrant("jellyfin", "1.0.0", "jellyfin-recent", map[string]string{"server": "jellyfin"}, capabilities.NewCapSet("http", "assets"), routes, routes, nil, capabilities.Limits{HTTPRequests: 4, ResponseMB: 4, HostCalls: 10}, capabilities.ExecutionIdentity{})
	return instance.Invoke(context.Background(), integrations.InvokeRequest{Operation: "recently-added", Params: json.RawMessage(params), Grant: grant})
}

func TestJellyfinPluginMatchesGoldenDocument(t *testing.T) {
	broker := &jellyfinBroker{t: t, dataDir: filepath.Join(jellyfinPluginDir, "testdata")}
	response, err := invokeJellyfin(t, broker, `{"limit":5}`)
	if err != nil {
		t.Fatal(err)
	}
	if broker.calls != 4 {
		t.Fatalf("HTTP calls = %d, want 4", broker.calls)
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
