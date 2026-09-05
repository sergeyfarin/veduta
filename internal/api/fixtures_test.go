// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"veduta.dev/veduta/internal/api"
	"veduta.dev/veduta/internal/fixtures"
)

func mustLoadFixtures(t *testing.T) *fixtures.Bundle {
	t.Helper()
	b, err := fixtures.Load()
	if err != nil {
		t.Fatalf("fixtures.Load: %v", err)
	}
	return &b
}

// TestFixtureRoutesAbsentByDefault proves the --fixtures surface does not exist at all unless
// Config.Fixtures is set - a config-shaped mistake here would mean the dev-only showcase leaks
// into a binary nobody asked to run in that mode.
func TestFixtureRoutesAbsentByDefault(t *testing.T) {
	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/dashboard", "/api/v1/cards", "/api/v1/cards/frigate", "/api/v1/assets/v1.poster.dune"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s without --fixtures = %d, want 404", path, rec.Code)
		}
	}
}

func TestFixtureRoutes(t *testing.T) {
	s, err := api.New(api.Config{
		Listen:   "127.0.0.1:0",
		Assets:   fstest.MapFS{},
		Fixtures: mustLoadFixtures(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()

	t.Run("dashboard", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d", rec.Code)
		}
		var body struct {
			Sections []struct {
				Cards []struct{ ID string } `json:"cards"`
			} `json:"sections"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Sections) == 0 || len(body.Sections[0].Cards) == 0 {
			t.Fatal("dashboard has no cards")
		}
	})

	t.Run("cards", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cards", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d", rec.Code)
		}
		var cards []struct {
			CardID    string `json:"cardId"`
			Execution struct {
				State string `json:"state"`
			} `json:"execution"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
			t.Fatal(err)
		}
		if len(cards) != 13 {
			t.Fatalf("got %d cards, want 13", len(cards))
		}
	})

	t.Run("one card", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cards/frigate", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d", rec.Code)
		}
		var cs struct {
			CardID string `json:"cardId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &cs); err != nil {
			t.Fatal(err)
		}
		if cs.CardID != "frigate" {
			t.Fatalf("cardId = %q", cs.CardID)
		}
	})

	t.Run("unknown card is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cards/does-not-exist", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
	})

	t.Run("known asset", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/v1.poster.dune", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("content type = %q", ct)
		}
		if rec.Body.Len() == 0 {
			t.Error("empty image body")
		}
	})

	t.Run("unknown asset is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/v1.no.such", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
	})
}
