// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"veduta.dev/veduta/internal/api"
	"veduta.dev/veduta/internal/config"
)

// TestRefusesPublicBindWithoutAuth is the D46 gate: the dashboard holds service credentials, and
// its listener used to default to every interface with authentication scheduled several phases
// later. Binding publicly without auth is a refusal to start, not a warning.
func TestRefusesPublicBindWithoutAuth(t *testing.T) {
	public := []string{":8099", "0.0.0.0:8099", "192.168.1.10:8099", "[::]:8099"}
	for _, addr := range public {
		t.Run(addr, func(t *testing.T) {
			if _, err := api.New(api.Config{Listen: addr}); !errors.Is(err, api.ErrPublicWithoutAuth) {
				t.Fatalf("New(%q) error = %v, want ErrPublicWithoutAuth", addr, err)
			}
		})
	}

	loopback := []string{"127.0.0.1:8099", "localhost:8099", "[::1]:8099"}
	for _, addr := range loopback {
		t.Run(addr, func(t *testing.T) {
			if _, err := api.New(api.Config{Listen: addr}); err != nil {
				t.Fatalf("New(%q) = %v, want success", addr, err)
			}
		})
	}

	if _, err := api.New(api.Config{Listen: ":8099", AuthConfigured: true}); err != nil {
		t.Fatalf("public bind with auth configured should succeed, got %v", err)
	}
	if _, err := api.New(api.Config{Listen: ":8099", AllowPublicWithoutAuth: true}); err != nil {
		t.Fatalf("explicit override should succeed, got %v", err)
	}
}

func TestConfigRoutesExposeSnapshotAndStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	body := []byte(`version: 1
server: {listen: "127.0.0.1:8099"}
auth: {mode: none}
dashboard: {title: Home, theme: auto, layout: {columns: 4, gap: normal}, groupBy: section}
connections: {}
integrations: [{id: demo, source: builtin}]
sections:
  - title: Test
    cards:
      - {id: card-one, title: Card One, integration: demo, operation: show, span: {columns: 2, rows: 1}}
rules: []
notifications: {channels: {}}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	store, diags := config.Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, contains string
	}{
		{"/api/v1/config/status", `"generation":1`},
		{"/api/v1/dashboard", `"columns":2`},
		{"/api/v1/cards", `"state":"pending"`},
	} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("GET %s = %d %s, want 200 containing %s", tc.path, rec.Code, rec.Body.String(), tc.contains)
		}
	}
}

func TestHealthAndVersion(t *testing.T) {
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()

	for _, path := range []string{"/api/v1/health", "/api/v1/version"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("GET %s content type = %q", path, ct)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("GET %s: %v", path, err)
		}
	}

	// AGPL section 13: a running instance must offer its corresponding source.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	var info struct {
		SourceURL string `json:"sourceUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.SourceURL == "" {
		t.Error("version response must carry the source URL")
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	s, _ := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route = %d, want 404", rec.Code)
	}
}
