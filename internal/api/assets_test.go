// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/storage/assetcache"
)

type assetRegistry struct {
	body   []byte
	header http.Header
	calls  int
}

func (r *assetRegistry) Get(id string) (*connections.Connection, bool) {
	return &connections.Connection{ID: id, Kind: connections.KindHTTP, HTTP: &connections.HTTPConfig{}}, true
}
func (r *assetRegistry) Do(_ context.Context, _ string, _ connections.Request) (*connections.Response, error) {
	r.calls++
	return &connections.Response{StatusCode: 200, Header: r.header, Body: r.body}, nil
}
func (r *assetRegistry) Health(context.Context, string) connections.Health {
	return connections.Health{}
}

func assetHandler(t *testing.T, authorize bool) (*http.ServeMux, *assettokens.Service, *storage.Store, *assetRegistry) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	revs, err := db.SyncConnectionRevisions(ctx, map[string][]byte{"photos": []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
	_ = revs
	cache, err := assetcache.New(db, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := assettokens.New(make([]byte, 32))
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	reg := &assetRegistry{body: png, header: http.Header{"Authorization": []string{"secret"}, "Set-Cookie": []string{"secret-cookie"}}}
	s := &Server{cfg: Config{AssetProxy: &AssetProxy{Tokens: tokens, Store: db, Cache: cache, Registry: reg, Authorize: func(context.Context, assettokens.Payload) (bool, error) { return authorize, nil }}}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	s.routeAssets(mux)
	return mux, tokens, db, reg
}

func mintedAsset(t *testing.T, tokens *assettokens.Service, db *storage.Store, mutate func(*assettokens.Payload)) string {
	t.Helper()
	revision, ok, err := db.ConnectionRevision(context.Background(), "photos")
	if err != nil || !ok {
		t.Fatal(err)
	}
	p := assettokens.Payload{V: 1, Connection: "photos", ConnectionRevision: revision, Path: "/api/assets/id/thumbnail", Query: "size=preview", Plugin: "immich@0.2.0", Expires: time.Now().Add(time.Hour).Unix()}
	if mutate != nil {
		mutate(&p)
	}
	token, err := tokens.Mint(p)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAssetProxyServesSniffedImageWithoutUpstreamHeadersAndCaches(t *testing.T) {
	mux, tokens, db, reg := assetHandler(t, true)
	token := mintedAsset(t, tokens, db, nil)
	for range 2 {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+token, nil))
		if rec.Code != 200 {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("content type=%q", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Authorization") != "" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("upstream credential-bearing headers leaked")
		}
	}
	if reg.calls != 1 {
		t.Fatalf("upstream calls=%d want 1", reg.calls)
	}
}

func TestAssetProxyRejectsRevokedChangedUnsafeAndForgedTokens(t *testing.T) {
	tests := []struct {
		name      string
		authorize bool
		mutate    func(*assettokens.Payload)
		after     func(*storage.Store)
		forge     bool
	}{
		{"approval revoked", false, nil, nil, false},
		{"connection changed", true, nil, func(db *storage.Store) {
			_, _ = db.SyncConnectionRevisions(context.Background(), map[string][]byte{"photos": []byte("changed")})
		}, false},
		{"traversal", true, func(p *assettokens.Payload) { p.Path = "/api/../secret" }, nil, false},
		{"transform", true, func(p *assettokens.Payload) { p.Transform = "w=999" }, nil, false},
		{"forged", true, nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, tokens, db, _ := assetHandler(t, tt.authorize)
			token := mintedAsset(t, tokens, db, tt.mutate)
			if tt.after != nil {
				tt.after(db)
			}
			if tt.forge {
				token = "A" + token[1:]
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+token, nil))
			if rec.Code != 404 {
				t.Fatalf("status=%d want 404", rec.Code)
			}
		})
	}
}

func TestAssetProxyRejectsHeaderLyingNonImage(t *testing.T) {
	mux, tokens, db, reg := assetHandler(t, true)
	reg.body = []byte("<html>credential prompt</html>")
	reg.header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+mintedAsset(t, tokens, db, nil), nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", rec.Code)
	}
}

// TestAssetProxyDeniesARedirectToAnUnapprovedPath is the regression test for a real gap found in
// review: the proxy verified the signed token's own path and then fetched it with the
// connection's credentials while attaching no redirect check at all. The broker had carried one
// for a plugin's own HTTP calls since the redirect finding was first closed; the asset path,
// which reaches the same connections.Registry, had been left out. An approved thumbnail that
// redirected to an unapproved path was fetched and served as an image.
//
// It uses a real registry against a real redirecting server, because the wiring being tested is
// precisely that the authorizer reaches connections.redirectPolicy through the context.
func TestAssetProxyDeniesARedirectToAnUnapprovedPath(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/assets/id/thumbnail" {
			http.Redirect(w, r, "/private/unapproved", http.StatusFound)
			return
		}
		png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
		_, _ = w.Write(png)
	}))
	defer upstream.Close()

	db, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.SyncConnectionRevisions(ctx, map[string][]byte{"photos": []byte("a")}); err != nil {
		t.Fatal(err)
	}
	cache, err := assetcache.New(db, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := connections.New(map[string]config.Connection{
		"photos": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: upstream.URL, Auth: config.ConnectionAuth{Type: "none"}, MaxRedirects: 3}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := assettokens.New(make([]byte, 32))

	// Authorises exactly the minted path, which is what an asset grant for this thumbnail means,
	// and nothing else - so the redirect destination is unapproved.
	authorize := func(_ context.Context, p assettokens.Payload) (bool, error) {
		return p.Path == "/api/assets/id/thumbnail", nil
	}
	s := &Server{cfg: Config{AssetProxy: &AssetProxy{Tokens: tokens, Store: db, Cache: cache, Registry: reg, Authorize: authorize}},
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	s.routeAssets(mux)

	token := mintedAsset(t, tokens, db, func(p *assettokens.Payload) { p.Query = "" })
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+token, nil))

	if rec.Code == http.StatusOK {
		t.Fatalf("the redirect to an unapproved path was followed and served (%d bytes)", rec.Body.Len())
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d - the fetch must fail, not serve", rec.Code, http.StatusBadGateway)
	}
}
