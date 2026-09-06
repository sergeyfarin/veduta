// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"veduta.dev/veduta/internal/api"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/integrations"
)

// glancesServer builds a real *api.Server backed by a temp config that declares the real
// plugins/glances integration (copied in, since `path:` sources resolve relative to the config
// file's own directory), plus a running config.Store so GET /api/v1/integrations and friends
// have something real to read.
func glancesServer(t *testing.T) (*api.Server, string) {
	t.Helper()
	dir := t.TempDir()

	src, err := filepath.Abs("../../plugins/glances")
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "plugins", "glances")
	if err = os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(src, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dst, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "veduta.yaml")
	body := "version: 1\nauth: {mode: none}\nintegrations:\n  - id: glances\n    source: path:./plugins/glances\n"
	if err = os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	store, diags := config.Open(configPath, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func doJSON(t *testing.T, s *api.Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestIntegrationsList_ShowsUnapproved(t *testing.T) {
	s, _ := glancesServer(t)
	rec := doJSON(t, s, http.MethodGet, "/api/v1/integrations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["id"] != "glances" || got[0]["status"] != string(integrations.StatusUnapproved) {
		t.Fatalf("got %+v", got)
	}
}

func TestIntegrationApproval_ReturnsDiffForUnapproved(t *testing.T) {
	s, _ := glancesServer(t)
	rec := doJSON(t, s, http.MethodGet, "/api/v1/integrations/glances/approval", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		ManifestSHA256 string            `json:"manifestSha256"`
		CurrentLock    any               `json:"currentLock"`
		Diff           integrations.Diff `json:"diff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ManifestSHA256 == "" {
		t.Fatal("expected a non-empty manifestSha256")
	}
	if preview.CurrentLock != nil {
		t.Fatalf("currentLock = %v, want nil for an unapproved integration", preview.CurrentLock)
	}
	if len(preview.Diff.AddedRoutes) != 4 {
		t.Fatalf("addedRoutes = %+v, want 4 (glances' full route set)", preview.Diff.AddedRoutes)
	}
}

func TestIntegrationApproval_UnknownIDIs404(t *testing.T) {
	s, _ := glancesServer(t)
	rec := doJSON(t, s, http.MethodGet, "/api/v1/integrations/nope/approval", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationApprove_HappyPathWritesLock(t *testing.T) {
	s, dir := glancesServer(t)

	preview := doJSON(t, s, http.MethodGet, "/api/v1/integrations/glances/approval", nil)
	var p struct {
		ManifestSHA256 string `json:"manifestSha256"`
		Diff           struct {
			AddedRoutes       []integrations.Route `json:"addedRoutes"`
			AddedCapabilities []string             `json:"addedCapabilities"`
		} `json:"diff"`
		ManifestLimits integrations.Limits `json:"manifestLimits"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{
		"expectedManifestSha256": p.ManifestSHA256,
		"grants": map[string]any{
			"capabilities": p.Diff.AddedCapabilities,
			"routes":       p.Diff.AddedRoutes,
			"limits":       p.ManifestLimits,
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	lock, err := integrations.ReadLock(filepath.Join(dir, integrations.LockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Integrations["glances"] == nil {
		t.Fatal("approval did not write a lock entry")
	}

	// A second GET of the approval preview should now show an empty diff.
	after := doJSON(t, s, http.MethodGet, "/api/v1/integrations/glances/approval", nil)
	var ap struct {
		Diff integrations.Diff `json:"diff"`
	}
	if err := json.Unmarshal(after.Body.Bytes(), &ap); err != nil {
		t.Fatal(err)
	}
	if !ap.Diff.Empty() {
		t.Fatalf("diff after approval = %+v, want empty", ap.Diff)
	}
}

func TestIntegrationApprove_StaleDigestReturns409(t *testing.T) {
	s, _ := glancesServer(t)
	rec := doJSON(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{
		"expectedManifestSha256": "0000000000000000000000000000000000000000000000000000000000000000",
		"grants":                 map[string]any{},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestIntegrationApprove_GrantExceedingRequestReturns400(t *testing.T) {
	s, _ := glancesServer(t)
	preview := doJSON(t, s, http.MethodGet, "/api/v1/integrations/glances/approval", nil)
	var p struct {
		ManifestSHA256 string `json:"manifestSha256"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{
		"expectedManifestSha256": p.ManifestSHA256,
		"grants": map[string]any{
			"capabilities": []string{"assets"}, // glances never requests assets
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestIntegrationApprove_BuiltinReturns409(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "veduta.yaml")
	body := "version: 1\nauth: {mode: none}\nintegrations:\n  - id: docker\n    source: builtin\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store, diags := config.Open(configPath, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store})
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, s, http.MethodGet, "/api/v1/integrations/docker/approval", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rec.Code, rec.Body.String())
	}
}
