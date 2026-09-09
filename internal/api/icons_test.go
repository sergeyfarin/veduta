// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/icons"
)

func TestIconEndpointServesOfflinePackWithIsolationHeaders(t *testing.T) {
	server, err := New(Config{Listen: "127.0.0.1:0", IconProxy: icons.New(nil), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/icons/sh:immich", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Fatalf("Content-Security-Policy=%q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options=%q", got)
	}
}

func TestIconEndpointRejectsUnknownSyntax(t *testing.T) {
	server, err := New(Config{Listen: "127.0.0.1:0", IconProxy: icons.New(nil), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/icons/file:%2F%2Fetc%2Fpasswd", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
