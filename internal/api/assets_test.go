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
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/assets/"+token, nil))
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
			mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/assets/"+token, nil))
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
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/assets/"+mintedAsset(t, tokens, db, nil), nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", rec.Code)
	}
}
