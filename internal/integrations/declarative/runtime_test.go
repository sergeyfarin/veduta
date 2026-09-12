// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

type fixtureBroker struct{}

func (fixtureBroker) HTTP(_ context.Context, _ capabilities.Grant, r capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	body := map[string]string{
		"/api/4/cpu":                       `{"total":25}`,
		"/api/4/mem":                       `{"percent":40}`,
		"/api/4/fs":                        `[{"mnt_point":"/","percent":50}]`,
		"/api/4/uptime":                    `"1:27:01"`,
		"/api/4/network":                   `[{"interface_name":"eth0","bytes_all_rate_per_sec":2048}]`,
		"/api/4/sensors":                   `[{"label":"CPU","type":"temperature_core","value":52}]`,
		"/api/collections/systems/records": `{"items":[{"name":"Build host","status":"up","updated":"2026-09-09T00:00:00Z","info":{"cpu":12.5,"mp":40,"dp":55,"u":3600,"bb":4096,"dt":52}}]}`,
		"/api/assets/statistics":           `{"images":1,"videos":2,"total":3}`,
		"/api/search/metadata":             `{"assets":{"items":[{"id":"x","originalFileName":"x.jpg","fileCreatedAt":"2026-01-01T00:00:00Z"}]}}`,
	}[r.Path]
	return capabilities.HTTPResponse{StatusCode: 200, Body: []byte(body)}, nil
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

// An asset node mints the ref and nothing else, so an image can carry alt text. It could not while
// the node evaluated to a whole {"ref": …} object: any sibling key stopped it being an asset node,
// which left every declarative integration unable to describe its own images. See
// docs/03-backlog-resolved.md.
func TestAssetNodeIsAValueSoImagesCanCarryAltText(t *testing.T) {
	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "immich", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "recent-assets", Params: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(resp.Document)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Blocks []struct {
			Type  string `json:"type"`
			Items []struct {
				Image struct {
					Ref string `json:"ref"`
					Alt string `json:"alt"`
				} `json:"image"`
			} `json:"items"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Blocks[0].Type != "image-grid" || len(doc.Blocks[0].Items) == 0 {
		t.Fatalf("expected a populated image-grid, got %s", body)
	}
	image := doc.Blocks[0].Items[0].Image
	if image.Ref != "v1.a.b" {
		t.Errorf("image.ref = %q, want the broker's minted ref", image.Ref)
	}
	if image.Alt != "x.jpg" {
		t.Errorf("image.alt = %q, want the photo's original filename", image.Alt)
	}
}

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
	if resp.Document.Title != "Host" || len(resp.Document.Blocks) != 4 {
		t.Fatalf("unexpected document: %#v", resp.Document)
	}
}

func TestBeszelGoldenPipeline(t *testing.T) {
	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "beszel", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := got.Invoke(context.Background(), integrations.InvokeRequest{Operation: "overview", Params: []byte(`{"systemId":"node-1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Document.Title != "Build host" || resp.Document.Status == nil || resp.Document.Status.Text != "up" || len(resp.Document.Blocks) != 2 {
		t.Fatalf("unexpected document: %#v", resp.Document)
	}
}

func TestLoadCompilesAllShippedDeclarativeManifests(t *testing.T) {
	for _, name := range []string{"beszel", "glances", "immich"} {
		m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", name, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// statusBroker answers every request with one chosen status and a JSON body, so the test can tell
// "the body was ignored because of the status" apart from "the body could not be decoded".
type statusBroker struct {
	fixtureBroker
	status int
}

func (b statusBroker) HTTP(ctx context.Context, g capabilities.Grant, r capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	resp, err := b.fixtureBroker.HTTP(ctx, g, r)
	resp.StatusCode = b.status
	return resp, err
}

// An upstream error is an error, not data. The body here is perfectly good JSON, so before this was
// enforced a 500 produced a cheerful, entirely fictional card.
func TestPipelineRejectsNonSuccessStatus(t *testing.T) {
	for _, status := range []int{301, 401, 403, 404, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "glances", "manifest.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			inst, err := New(statusBroker{status: status}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "overview", Params: []byte(`{}`)})
			if err == nil {
				t.Fatalf("HTTP %d produced a document instead of an error", status)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(status)) {
				t.Errorf("error should name the status, got %v", err)
			}
		})
	}
}

func TestPipelineAcceptsEverySuccessStatus(t *testing.T) {
	for _, status := range []int{200, 201, 204, 299} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "glances", "manifest.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			inst, err := New(statusBroker{status: status}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "overview", Params: []byte(`{}`)}); err != nil {
				t.Fatalf("HTTP %d should be accepted: %v", status, err)
			}
		})
	}
}
