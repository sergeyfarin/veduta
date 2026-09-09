// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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

func TestIntegrationApprovalIsCLIOnlyWithExplicitAuthNone(t *testing.T) {
	_, dir := glancesServer(t)
	// Rebuild with the production auth policy so the legacy helper remains useful to the
	// approval transaction tests while this test covers the security boundary itself.
	store, diags := config.Open(filepath.Join(dir, "veduta.yaml"), nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	secured, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store, AuthMode: config.AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, secured, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
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

func doRaw(t *testing.T, s *api.Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestIntegrationApprove_ClientCannotSupplyApprovedBy is the regression test for a real gap
// found in review: approveRequest used to carry an approvedBy field taken directly from the
// request body and written straight into the audit trail with no authentication behind it at
// all. approveRequest no longer has such a field, and DisallowUnknownFields means a client that
// still sends one gets a clear 400, not a silently-ignored value; the entry actually written is
// always attributed to the fixed, honest sentinel until a real session system (H1) exists.
func TestIntegrationApprove_ClientCannotSupplyApprovedBy(t *testing.T) {
	s, dir := glancesServer(t)
	preview := doJSON(t, s, http.MethodGet, "/api/v1/integrations/glances/approval", nil)
	var p struct {
		ManifestSHA256 string `json:"manifestSha256"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{
		"expectedManifestSha256": p.ManifestSHA256,
		"grants":                 map[string]any{},
		"approvedBy":             "attacker-supplied-identity",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for an unknown approvedBy field", rec.Code, rec.Body.String())
	}

	// A normal request (no approvedBy at all) must still succeed and be attributed honestly.
	rec = doJSON(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{
		"expectedManifestSha256": p.ManifestSHA256,
		"grants":                 map[string]any{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	lock, err := integrations.ReadLock(filepath.Join(dir, integrations.LockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Integrations["glances"].ApprovedBy != "rest-api (unauthenticated)" {
		t.Fatalf("approvedBy = %q, want the fixed unauthenticated sentinel", lock.Integrations["glances"].ApprovedBy)
	}
}

// TestIntegrationApprove_RejectsTrailingJSON and the oversized-body test below are the
// regression tests for a real gap found in review: the approve endpoint decoded only the first
// JSON value with no check for trailing content, and had no request body size ceiling at all -
// still self-documented as an unfinished A3 requirement ("max header/body bytes").
func TestIntegrationApprove_RejectsTrailingJSON(t *testing.T) {
	s, _ := glancesServer(t)
	rec := doRaw(t, s, http.MethodPost, "/api/v1/integrations/glances/approve",
		[]byte(`{"expectedManifestSha256":"x","grants":{}}{"extra":"document"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for trailing JSON content", rec.Code, rec.Body.String())
	}
}

func TestIntegrationApprove_OversizedBodyRejected(t *testing.T) {
	s, _ := glancesServer(t)
	huge := make([]byte, 2<<20) // 2 MiB, over the 1 MiB server-wide body cap
	for i := range huge {
		huge[i] = 'x'
	}
	body := []byte(`{"expectedManifestSha256":"` + string(huge) + `","grants":{}}`)
	rec := doRaw(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for a body over the server-wide size cap", rec.Code, rec.Body.String())
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

// TestIntegrationApprove_ConcurrentApprovalsOfDifferentIntegrationsDoNotLoseAnUpdate is the
// regression test for a real race found in review: two concurrent POST .../approve calls for
// *different* integrations could each read the same lock, add their own entry to their own
// in-memory copy, and the second WriteLock would silently discard the first's addition - approveMu
// now serialises the whole read-modify-write. Both integrations here are declared and unapproved;
// after firing both approvals concurrently, the written lock must contain both.
func TestIntegrationApprove_ConcurrentApprovalsOfDifferentIntegrationsDoNotLoseAnUpdate(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"glances", "immich"} {
		src, err := filepath.Abs(filepath.Join("..", "..", "plugins", name))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "plugins", name)
		if mkdirErr := os.MkdirAll(dst, 0o755); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		manifest, err := os.ReadFile(filepath.Join(src, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if writeErr := os.WriteFile(filepath.Join(dst, "manifest.yaml"), manifest, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	configPath := filepath.Join(dir, "veduta.yaml")
	body := "version: 1\nauth: {mode: none}\nintegrations:\n" +
		"  - id: glances\n    source: path:./plugins/glances\n" +
		"  - id: immich\n    source: path:./plugins/immich\n"
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

	digestOf := func(id string) string {
		preview := doJSON(t, s, http.MethodGet, "/api/v1/integrations/"+id+"/approval", nil)
		var p struct {
			ManifestSHA256 string `json:"manifestSha256"`
		}
		if unmarshalErr := json.Unmarshal(preview.Body.Bytes(), &p); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		return p.ManifestSHA256
	}
	glancesDigest, immichDigest := digestOf("glances"), digestOf("immich")

	var wg sync.WaitGroup
	results := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		rec := doJSON(t, s, http.MethodPost, "/api/v1/integrations/glances/approve", map[string]any{
			"expectedManifestSha256": glancesDigest, "grants": map[string]any{},
		})
		results[0] = rec.Code
	}()
	go func() {
		defer wg.Done()
		rec := doJSON(t, s, http.MethodPost, "/api/v1/integrations/immich/approve", map[string]any{
			"expectedManifestSha256": immichDigest, "grants": map[string]any{},
		})
		results[1] = rec.Code
	}()
	wg.Wait()

	if results[0] != http.StatusOK || results[1] != http.StatusOK {
		t.Fatalf("approve status codes = %v, want both 200", results)
	}
	lock, err := integrations.ReadLock(filepath.Join(dir, integrations.LockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Integrations["glances"] == nil || lock.Integrations["immich"] == nil {
		t.Fatalf("expected both integrations in the written lock, got %+v", lock.Integrations)
	}
}
