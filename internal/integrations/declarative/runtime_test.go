// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

type fixtureBroker struct{}

func (fixtureBroker) HTTP(_ context.Context, _ capabilities.Grant, r capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	body := map[string]string{"/api/4/cpu": `{"total":25}`, "/api/4/mem": `{"percent":40}`, "/api/4/fs": `[{"mnt_point":"/","percent":50}]`, "/api/assets/statistics": `{"images":1,"videos":2,"total":3}`, "/api/search/metadata": `{"assets":{"items":[{"id":"x","originalFileName":"x.jpg","fileCreatedAt":"2026-01-01T00:00:00Z"}]}}`}[r.Path]
	return capabilities.HTTPResponse{Body: []byte(body)}, nil
}
func (fixtureBroker) CacheGet(context.Context, capabilities.Grant, string) ([]byte, bool, error) {
	return nil, false, nil
}
func (fixtureBroker) CachePut(context.Context, capabilities.Grant, string, []byte, time.Duration) error {
	return nil
}
func (fixtureBroker) AssetRef(context.Context, capabilities.Grant, string, string, url.Values, capabilities.Transform) (string, error) {
	return "v1.a.b", nil
}

func TestImmichPipelineAppliesParamDefault(t *testing.T) {
	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "immich", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := got.Invoke(context.Background(), integrations.InvokeRequest{Operation: "recent-assets", Params: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Document.Title != "Immich" || len(resp.Document.Blocks) != 2 {
		t.Fatalf("unexpected document: %#v", resp.Document)
	}
}
func (fixtureBroker) Log(capabilities.Grant, string, string, map[string]any) error       { return nil }
func (fixtureBroker) Emit(context.Context, capabilities.Grant, capabilities.Event) error { return nil }

func TestGlancesGoldenPipeline(t *testing.T) {
	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "glances", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := New(fixtureBroker{})
	got, err := rt.Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := got.Invoke(context.Background(), integrations.InvokeRequest{Operation: "overview", Params: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Document.Title != "Host" || len(resp.Document.Blocks) != 1 {
		t.Fatalf("unexpected document: %#v", resp.Document)
	}
}

func TestLoadCompilesAllShippedDeclarativeManifests(t *testing.T) {
	for _, name := range []string{"glances", "immich"} {
		m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", name, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
