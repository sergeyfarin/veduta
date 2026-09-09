// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/state"
)

type immichRegistry struct{}

func (immichRegistry) Get(id string) (*connections.Connection, bool) {
	return &connections.Connection{ID: id, Kind: connections.KindHTTP, HTTP: &connections.HTTPConfig{}}, true
}

type dockerRegistry struct{}

func (dockerRegistry) Get(id string) (*connections.Connection, bool) {
	return &connections.Connection{ID: id, Kind: connections.KindDocker, Docker: &connections.DockerConfig{}}, true
}

func (dockerRegistry) Do(context.Context, string, connections.Request) (*connections.Response, error) {
	return nil, connections.ErrNotHTTP
}

func (dockerRegistry) Health(context.Context, string) connections.Health { return connections.Health{} }

func (dockerRegistry) DockerGET(context.Context, string, string) (*connections.Response, error) {
	return &connections.Response{StatusCode: http.StatusOK, Body: []byte(`[{"Id":"abc","Names":["/web"],"Image":"nginx:latest","State":"running","Status":"Up 1 hour","Created":1788890400}]`)}, nil
}

func TestBuildGenerationRunsBuiltinDockerIntegration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "veduta.yaml")
	source := `version: 1
auth: { mode: none }
connections:
  local: { kind: docker, endpoint: "unix:///var/run/docker.sock" }
integrations:
  - { id: docker, source: builtin }
sections:
  - cards:
      - id: containers
        integration: docker
        operation: containers
        slots: { server: local }
`
	if err := os.WriteFile(configPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, diags := config.LoadPath(configPath)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	generation, err := BuildGeneration(context.Background(), snapshot, 1, configPath, dockerRegistry{}, map[string]string{}, nil, filepath.Join(dir, "wasm-cache"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = generation.Close(context.Background()) }()
	if len(generation.Definitions) != 1 || generation.Definitions[0].Source.Runtime != state.RuntimeBuiltin || generation.Definitions[0].Run == nil {
		t.Fatalf("definition=%+v", generation.Definitions)
	}
	document, err := generation.Definitions[0].Run(context.Background())
	if err != nil || document.Title != "Containers" {
		t.Fatalf("document=%+v err=%v", document, err)
	}
}

func TestBuildGenerationLoadsApprovedWASMRuntime(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "jellyfin")
	if err := os.MkdirAll(filepath.Dir(pluginDir), 0o700); err != nil {
		t.Fatal(err)
	}
	repoPlugin, err := filepath.Abs(filepath.Join("..", "..", "plugins", "jellyfin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(repoPlugin, pluginDir); err != nil {
		t.Fatal(err)
	}
	manifest, err := integrations.LoadManifest(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := integrations.Approve(manifest, manifest.Digest, integrations.Grants{Capabilities: manifest.Capabilities, Routes: manifest.Routes, Limits: manifest.Limits}, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = integrations.WriteLock(filepath.Join(dir, integrations.LockFileName), &integrations.Lock{Version: 1, Integrations: map[string]*integrations.LockEntry{"jellyfin": entry}}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "veduta.yaml")
	source := `version: 1
auth: { mode: none }
connections:
  jellyfin: { kind: http, baseUrl: "https://jellyfin.test", auth: { type: none } }
integrations:
  - { id: jellyfin, source: "path:./plugins/jellyfin" }
sections:
  - cards:
      - id: recent
        integration: jellyfin
        operation: recently-added
        slots: { server: jellyfin }
        params: { limit: 5 }
        refresh: 5m
`
	if err = os.WriteFile(configPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, diags := config.LoadPath(configPath)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	tokens, _ := assettokens.New(make([]byte, 32))
	generation, err := BuildGeneration(context.Background(), snap, 1, configPath, immichRegistry{}, map[string]string{"jellyfin": "revision"}, tokens, filepath.Join(dir, "wasm-cache"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = generation.Close(context.Background()) }()
	if len(generation.Definitions) != 1 || generation.Definitions[0].Run == nil || generation.Definitions[0].Source.Runtime != state.RuntimeWASM {
		t.Fatalf("WASM definition not loaded: %+v", generation.Definitions)
	}
}
func (immichRegistry) Health(context.Context, string) connections.Health { return connections.Health{} }
func (immichRegistry) Do(_ context.Context, _ string, req connections.Request) (*connections.Response, error) {
	body := `{}`
	switch req.Path {
	case "/api/assets/statistics":
		body = `{"images":1,"videos":2,"total":3}`
	case "/api/search/metadata":
		body = `{"assets":{"items":[{"id":"photo-1","originalFileName":"one.jpg","fileCreatedAt":"2026-01-01T00:00:00Z"}]}}`
	}
	return &connections.Response{StatusCode: http.StatusOK, Body: []byte(body)}, nil
}

func TestDefinitionKeyIncludesEverySlotConnectionAndRevision(t *testing.T) {
	base := definitionKey("digest", "op", map[string]string{"server": "a"}, map[string]string{"server": "r1"}, map[string]any{"limit": 6})
	cases := map[string]string{
		"connection":  definitionKey("digest", "op", map[string]string{"server": "b"}, map[string]string{"server": "r1"}, map[string]any{"limit": 6}),
		"revision":    definitionKey("digest", "op", map[string]string{"server": "a"}, map[string]string{"server": "r2"}, map[string]any{"limit": 6}),
		"second slot": definitionKey("digest", "op", map[string]string{"server": "a", "metadata": "a"}, map[string]string{"server": "r1", "metadata": "r1"}, map[string]any{"limit": 6}),
	}
	for name, key := range cases {
		if key == base {
			t.Errorf("%s did not change single-flight key", name)
		}
	}
	reordered := definitionKey("digest", "op", map[string]string{"metadata": "a", "server": "a"}, map[string]string{"metadata": "r1", "server": "r1"}, map[string]any{"limit": 6})
	want := definitionKey("digest", "op", map[string]string{"server": "a", "metadata": "a"}, map[string]string{"server": "r1", "metadata": "r1"}, map[string]any{"limit": 6})
	if reordered != want {
		t.Fatal("map iteration order changed definition key")
	}
}

func TestImmichDefinitionProducesSignedCredentialFreeDocument(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "immich")
	if err := os.MkdirAll(filepath.Dir(pluginDir), 0o700); err != nil {
		t.Fatal(err)
	}
	repoPlugin, err := filepath.Abs(filepath.Join("..", "..", "plugins", "immich"))
	if err != nil {
		t.Fatal(err)
	}
	if symlinkErr := os.Symlink(repoPlugin, pluginDir); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}
	m, err := integrations.LoadManifest(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := integrations.Approve(m, m.Digest, integrations.Grants{Capabilities: m.Capabilities, Routes: m.Routes, Limits: m.Limits}, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = integrations.WriteLock(filepath.Join(dir, integrations.LockFileName), &integrations.Lock{Version: 1, Integrations: map[string]*integrations.LockEntry{"immich": entry}}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "veduta.yaml")
	source := `version: 1
auth: { mode: none }
connections:
  immich: { kind: http, baseUrl: "https://immich.test", auth: { type: none } }
integrations:
  - { id: immich, source: "path:./plugins/immich" }
sections:
  - cards:
      - id: recent
        integration: immich
        operation: recent-assets
        slots: { server: immich }
        params: { limit: 6 }
        refresh: 10m
`
	if err = os.WriteFile(configPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, diags := config.LoadPath(configPath)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	tokens, _ := assettokens.New(make([]byte, 32))
	generation, err := BuildGeneration(context.Background(), snap, 1, configPath, immichRegistry{}, map[string]string{"immich": "opaque-revision"}, tokens, filepath.Join(dir, "wasm-cache"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = generation.Close(context.Background()) }()
	if len(generation.Definitions) != 1 || generation.Definitions[0].Run == nil {
		t.Fatalf("definitions=%+v", generation.Definitions)
	}
	doc, err := generation.Definitions[0].Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "api-key") || strings.Contains(string(body), "credential") {
		t.Fatalf("credential material in document: %s", body)
	}
	if !strings.Contains(string(body), `"ref":"`) {
		t.Fatalf("signed asset reference missing: %s", body)
	}
}
