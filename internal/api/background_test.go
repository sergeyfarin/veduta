// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"bytes"
	"image"
	"image/png"
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

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	// Pixels go straight into Pix (RGBA order). The standard library's colour-model package would
	// be the obvious route, but its American spelling trips this repo's UK-locale misspell linter,
	// and the test only needs recognisable bytes.
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(i), 2, 3, 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// backgroundServer writes a config whose dashboard.background is `background`, relative to the
// config directory, and returns a server plus that directory.
func backgroundServer(t *testing.T, background string) (*api.Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	line := ""
	if background != "" {
		line = ", background: " + background
	}
	body := []byte(`version: 1
server: {listen: "127.0.0.1:8099"}
auth: {mode: none}
dashboard: {title: Home, appearance: veil` + line + `}
connections: {}
integrations: [{id: demo, source: builtin}]
sections:
  - title: Test
    cards:
      - {id: card-one, title: Card One, integration: demo, operation: show}
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
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store, ConfigDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func get(t *testing.T, s *api.Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestBackground_ServesAConfiguredImage covers the ordinary path, including that the relative
// path resolves against the CONFIG directory rather than the process working directory - the
// property that makes a background behave the same whichever directory veduta was started from.
func TestBackground_ServesAConfiguredImage(t *testing.T) {
	s, dir := backgroundServer(t, "wallpaper.png")
	want := pngBytes(t)
	if err := os.WriteFile(filepath.Join(dir, "wallpaper.png"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get(t, s, "/api/v1/background")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Error("body is not the configured file")
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("no ETag, so every viewer refetches the wallpaper on every load")
	}
}

// TestBackground_RefusesAFileThatIsNotAnImage is the reason this route sniffs content instead of
// trusting the path. The operator wrote the config, so this is not a privilege boundary - it is a
// blast-radius limit on a TYPO: without the check, a mistyped path turns a background into an
// arbitrary file read served to every viewer of the dashboard.
func TestBackground_RefusesAFileThatIsNotAnImage(t *testing.T) {
	s, dir := backgroundServer(t, "secrets.txt")
	secret := "AKIAIOSFODNN7EXAMPLE not-an-image"
	if err := os.WriteFile(filepath.Join(dir, "secrets.txt"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get(t, s, "/api/v1/background")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a non-image", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "AKIA") {
		t.Fatal("the file's contents were served")
	}
}

func TestBackground_MissingFileIs404(t *testing.T) {
	s, _ := backgroundServer(t, "absent.png")
	if rec := get(t, s, "/api/v1/background"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestBackground_UnconfiguredIs404(t *testing.T) {
	s, _ := backgroundServer(t, "")
	if rec := get(t, s, "/api/v1/background"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestBackground_DashboardAdvertisesItAsABooleanNotAPath: the browser is told whether a
// background exists, never where it lives on the host's filesystem.
func TestBackground_DashboardAdvertisesItAsABooleanNotAPath(t *testing.T) {
	s, dir := backgroundServer(t, "wallpaper.png")
	if err := os.WriteFile(filepath.Join(dir, "wallpaper.png"), pngBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	body := get(t, s, "/api/v1/dashboard").Body.String()
	if !strings.Contains(body, `"background":true`) {
		t.Errorf("dashboard payload does not advertise the background: %s", body)
	}
	if strings.Contains(body, "wallpaper.png") || strings.Contains(body, dir) {
		t.Errorf("dashboard payload leaks a filesystem path: %s", body)
	}
}

// TestBackground_RejectedUnlessAppearanceIsVeil: a background under the clean preset would do
// nothing, and configuration that silently does nothing is the defect dashboard.theme was.
func TestBackground_RejectedUnlessAppearanceIsVeil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	body := []byte(`version: 1
server: {listen: "127.0.0.1:8099"}
auth: {mode: none}
dashboard: {title: Home, appearance: clean, background: wallpaper.png}
connections: {}
integrations: []
sections: []
rules: []
notifications: {channels: {}}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, diags := config.Load(path); !diags.HasErrors() {
		t.Fatal("a background under appearance: clean must be a configuration error")
	}
}
