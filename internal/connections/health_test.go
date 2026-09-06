// SPDX-License-Identifier: AGPL-3.0-or-later

package connections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/secrets"
)

func registryFor(t *testing.T, baseURL string) connections.Registry {
	t.Helper()
	reg, err := connections.New(map[string]config.Connection{
		"svc": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: baseURL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestHealth_ReachableOn2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	h := registryFor(t, srv.URL).Health(context.Background(), "svc")
	if !h.Reachable || h.Stage != "" || h.StatusCode != http.StatusOK {
		t.Fatalf("health = %+v, want reachable 200", h)
	}
}

func TestHealth_AuthStageOn401And403(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		h := registryFor(t, srv.URL).Health(context.Background(), "svc")
		srv.Close()
		if h.Reachable || h.Stage != connections.StageAuth || h.StatusCode != code {
			t.Fatalf("code %d: health = %+v, want stage auth", code, h)
		}
	}
}

func TestHealth_HTTPStatusStageOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	h := registryFor(t, srv.URL).Health(context.Background(), "svc")
	if h.Reachable || h.Stage != connections.StageStatus || h.StatusCode != http.StatusInternalServerError {
		t.Fatalf("health = %+v, want stage http-status", h)
	}
}

// TestHealth_TCPStageOnConnectionRefused points at a port nothing is listening on - a closed
// httptest.Server leaves its address free but unreachable, the ordinary shape of "service is
// down" rather than "hostname doesn't exist" (DNS) or "wrong certificate" (TLS).
func TestHealth_TCPStageOnConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()
	h := registryFor(t, addr).Health(context.Background(), "svc")
	if h.Reachable || h.Stage != connections.StageTCP {
		t.Fatalf("health = %+v, want stage tcp", h)
	}
}

// TestHealth_DNSStageOnUnresolvableHost uses a stub connections.New(...) baseUrl with a hostname
// guaranteed never to resolve (the .invalid TLD is reserved for exactly this by RFC 2606), rather
// than depending on any real network's actual failure behaviour.
func TestHealth_DNSStageOnUnresolvableHost(t *testing.T) {
	h := registryFor(t, "http://this-host-does-not-exist.invalid").Health(context.Background(), "svc")
	if h.Reachable || h.Stage != connections.StageDNS {
		t.Fatalf("health = %+v, want stage dns", h)
	}
}

// TestHealth_TLSStageOnUntrustedCertificate uses httptest.NewTLSServer's self-signed
// certificate, which nothing in the registry's default TLS config is told to trust (no
// insecureSkipVerify, no caFile) - a real, common misconfiguration (self-signed or internal-CA
// upstream, admin forgot tls.caFile), not a synthetic error.
func TestHealth_TLSStageOnUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	h := registryFor(t, srv.URL).Health(context.Background(), "svc")
	if h.Reachable || h.Stage != connections.StageTLS {
		t.Fatalf("health = %+v, want stage tls", h)
	}
}

// TestHealth_NetworkFailureNeverLeaksAQueryAuthCredential: a query-type auth connection injects
// its resolved secret directly into the request URL (injectAuth); *url.Error formats as
// `Op "URL": Err`, so a network-level failure would otherwise put the raw credential straight
// into Health.Error. This is the exact scenario D5's own AC guards against ("the response
// contains no credentials, asserted by scanning the JSON for every configured secret value"),
// proven here at the source rather than only downstream in the API response test.
func TestHealth_NetworkFailureNeverLeaksAQueryAuthCredential(t *testing.T) {
	const secretValue = "super-secret-api-key-value"
	reg, err := connections.New(map[string]config.Connection{
		"svc": {Kind: "http", HTTP: &config.HTTPConnection{
			BaseURL: "http://192.0.2.1:81", // TEST-NET-1: reserved, guaranteed not to answer
			Auth:    config.ConnectionAuth{Type: "query", Name: "api_key", Value: config.SecretRef{Name: "KEY"}},
		}},
	}, map[string]secrets.Value{"KEY": secrets.New(secretValue)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	h := reg.Health(ctx, "svc")
	if strings.Contains(h.Error, secretValue) {
		t.Fatalf("Health.Error leaked the credential verbatim: %q", h.Error)
	}
}

func TestHealth_UnknownConnection(t *testing.T) {
	reg := registryFor(t, "http://example.invalid")
	h := reg.Health(context.Background(), "nope")
	if h.Reachable || h.Error == "" {
		t.Fatalf("health = %+v, want an error for an unknown connection", h)
	}
}

func TestHealth_DockerConnectionIsNotHTTP(t *testing.T) {
	reg, err := connections.New(map[string]config.Connection{
		"docker1": {Kind: "docker", Docker: &config.DockerConnection{Endpoint: "tcp://127.0.0.1:1"}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := reg.Health(context.Background(), "docker1")
	if h.Reachable || h.Error == "" {
		t.Fatalf("health = %+v, want an error for a non-http connection", h)
	}
}

// TestHealth_ContextDeadlineDuringConnectIsStagedTCP: a request that times out establishing the
// connection (a non-routable address that never answers, per RFC 5737's TEST-NET-1) is staged as
// TCP - the layer a connect timeout actually belongs to, not left unstaged.
func TestHealth_ContextDeadlineDuringConnectIsStagedTCP(t *testing.T) {
	reg := registryFor(t, "http://192.0.2.1:81") // TEST-NET-1: reserved, guaranteed not to answer
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	h := reg.Health(ctx, "svc")
	if h.Reachable || h.Stage != connections.StageTCP {
		t.Fatalf("health = %+v, want stage tcp", h)
	}
}
