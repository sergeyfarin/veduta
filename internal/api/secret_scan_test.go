// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestConfiguredSecretsAppearInNoHTTPResponse is the CI response scan promised by L3. It drives
// every response-producing route available on a server holding a real resolved connection secret
// and checks both headers and bodies, including success, upstream failure and 404 paths.
func TestConfiguredSecretsAppearInNoHTTPResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	server, secret := connectionsServer(t, upstream)

	requests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/v1/health"},
		{http.MethodGet, "/api/v1/version"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, "/api/v1/config/status"},
		{http.MethodGet, "/api/v1/dashboard"},
		{http.MethodGet, "/api/v1/cards"},
		{http.MethodGet, "/api/v1/connections"},
		{http.MethodPost, "/api/v1/connections/healthy/test"},
		{http.MethodPost, "/api/v1/connections/broken/test"},
		{http.MethodGet, "/api/v1/integrations"},
		{http.MethodGet, "/api/v1/not-found"},
	}
	for _, item := range requests {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, httptest.NewRequest(item.method, item.path, nil))
			wire := fmt.Sprint(response.Header()) + "\n" + response.Body.String()
			if strings.Contains(wire, secret) {
				t.Fatalf("configured secret appeared in HTTP response: %s", wire)
			}
		})
	}
}
