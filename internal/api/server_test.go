package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"veduta.dev/veduta/internal/api"
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

func TestHealthAndVersion(t *testing.T) {
	s, err := api.New(api.Config{Listen: "127.0.0.1:0"})
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
	s, _ := api.New(api.Config{Listen: "127.0.0.1:0"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route = %d, want 404", rec.Code)
	}
}
