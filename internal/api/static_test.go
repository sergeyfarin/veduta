// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html><html><body>veduta</body></html>")},
		"assets/index-abc123.js":  {Data: []byte("export const x = 1;")},
		"assets/index-abc123.css": {Data: []byte("body{margin:0}")},
		"favicon.svg":             {Data: []byte("<svg/>")},
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{Listen: "127.0.0.1:0", Assets: testAssets(), AssetsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesIndexAndAssets(t *testing.T) {
	h := testServer(t).staticHandler(testAssets(), true)

	t.Run("index", func(t *testing.T) {
		rec := get(t, h, "/")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("content type = %q, want text/html", ct)
		}
		if !strings.Contains(rec.Body.String(), "veduta") {
			t.Error("body is not the SPA document")
		}
		// index.html names the hashed bundles, so caching it is how a browser ends up asking
		// for a bundle that no longer exists.
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("index Cache-Control = %q, want no-cache", cc)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Errorf("missing or weak CSP: %q", csp)
		}
	})

	t.Run("hashed asset is immutable", func(t *testing.T) {
		rec := get(t, h, "/assets/index-abc123.js")
		if rec.Code != http.StatusOK {
			t.Fatalf("= %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Errorf("content type = %q, want a javascript type", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("Cache-Control = %q, want immutable", cc)
		}
	})

	t.Run("unhashed file is not cached forever", func(t *testing.T) {
		rec := get(t, h, "/favicon.svg")
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", cc)
		}
	})
}

// TestSPAFallback: a deep link must survive a cold load, which means unknown paths render the
// application rather than 404.
func TestSPAFallback(t *testing.T) {
	h := testServer(t).staticHandler(testAssets(), true)
	for _, path := range []string{"/dashboard", "/settings/connections", "/deep/link/nested"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (SPA fallback)", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "veduta") {
			t.Errorf("GET %s did not fall back to index.html", path)
		}
	}
}

// TestAPINamespaceIsNeverShadowed: a typo in an endpoint must 404, not return the SPA with a 200
// and not a 503 either - a client bug otherwise looks like a routing bug for an hour. Checked
// both with a frontend embedded and without, because the first version of this guard lived
// inside the has-a-build branch and an API-only binary answered 503 for every unknown endpoint.
func TestAPINamespaceIsNeverShadowed(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"with a frontend", Config{Listen: "127.0.0.1:0", Assets: testAssets(), AssetsPresent: true}},
		{"api only", Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, AssetsPresent: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			h := s.routes()
			for _, p := range []string{"/api/v1/nope", "/api/nope", "/api"} {
				rec := get(t, h, p)
				if rec.Code != http.StatusNotFound {
					t.Errorf("GET %s = %d, want 404", p, rec.Code)
				}
				if strings.Contains(rec.Body.String(), "<html") {
					t.Errorf("GET %s returned the SPA document instead of a 404", p)
				}
			}
		})
	}
}

// TestNoBuildEmbedded: a binary built without running the frontend build says so, rather than
// serving a blank page that looks like a broken dashboard.
func TestNoBuildEmbedded(t *testing.T) {
	h := testServer(t).staticHandler(fstest.MapFS{}, false)
	rec := get(t, h, "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("= %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pnpm build") {
		t.Errorf("the error should say how to fix it, got %q", rec.Body.String())
	}
}

// TestPathTraversal: the embedded FS is read-only and rooted, but the guard is worth pinning.
func TestPathTraversal(t *testing.T) {
	h := testServer(t).staticHandler(testAssets(), true)
	for _, path := range []string{"/../go.mod", "/assets/../../go.mod", "/%2e%2e/go.mod"} {
		rec := get(t, h, path)
		if strings.Contains(rec.Body.String(), "module veduta.dev") {
			t.Errorf("GET %s escaped the embedded filesystem", path)
		}
	}
}
