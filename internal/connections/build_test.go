// SPDX-License-Identifier: AGPL-3.0-or-later

package connections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/secrets"
)

func TestNew_BuildsHTTPConnectionFromConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "resolvedvalue123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := map[string]config.Connection{
		"svc": {
			Kind: "http",
			HTTP: &config.HTTPConnection{
				BaseURL: srv.URL,
				Auth: config.ConnectionAuth{
					Type:  "header",
					Name:  "X-Api-Key",
					Value: config.SecretRef{Name: "API_KEY"},
				},
			},
		},
	}
	resolved := map[string]secrets.Value{"API_KEY": secrets.New("resolvedvalue123")}

	reg, err := connections.New(cfg, resolved, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, ok := reg.Get("svc")
	if !ok || conn.Kind != connections.KindHTTP {
		t.Fatalf("Get(svc) = %+v, %v", conn, ok)
	}
	resp, err := reg.Do(context.Background(), "svc", connections.Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want the resolved secret to have been injected correctly", resp.StatusCode)
	}
}

// TestNew_MissingResolvedSecretFailsLoudly: a ${secret:NAME} reference this package cannot find
// in resolved must refuse to build, not silently inject an empty credential.
func TestNew_MissingResolvedSecretFailsLoudly(t *testing.T) {
	cfg := map[string]config.Connection{
		"svc": {
			Kind: "http",
			HTTP: &config.HTTPConnection{
				BaseURL: "http://example.com",
				Auth: config.ConnectionAuth{
					Type:  "bearer",
					Value: config.SecretRef{Name: "MISSING"},
				},
			},
		},
	}
	_, err := connections.New(cfg, map[string]secrets.Value{}, nil)
	if err == nil {
		t.Fatal("want an error when a referenced secret was never resolved")
	}
}

func TestNew_SkipsDisabledConnectionBeforeResolvingIt(t *testing.T) {
	disabled := false
	cfg := map[string]config.Connection{
		"imported": {
			Kind:    "http",
			Enabled: &disabled,
			HTTP: &config.HTTPConnection{BaseURL: "://invalid", Auth: config.ConnectionAuth{
				Type: "bearer", Value: config.SecretRef{Name: "NOT_CONFIGURED_YET"},
			}},
		},
	}
	registry, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("imported"); ok {
		t.Fatal("disabled connection was constructed")
	}
}

func TestNew_LiteralAuthValueWorksWithoutAnySecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer literal-token-value" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := map[string]config.Connection{
		"svc": {
			Kind: "http",
			HTTP: &config.HTTPConnection{
				BaseURL: srv.URL,
				Auth:    config.ConnectionAuth{Type: "bearer", Value: config.SecretRef{Literal: "literal-token-value"}},
			},
		},
	}
	reg, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := reg.Do(context.Background(), "svc", connections.Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestNew_DockerConnection(t *testing.T) {
	cfg := map[string]config.Connection{
		"docker-local": {
			Kind:   "docker",
			Docker: &config.DockerConnection{Endpoint: "unix:///var/run/docker.sock"},
		},
	}
	reg, err := connections.New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, ok := reg.Get("docker-local")
	if !ok || conn.Kind != connections.KindDocker || conn.Docker.Endpoint != "unix:///var/run/docker.sock" {
		t.Fatalf("Get(docker-local) = %+v, %v", conn, ok)
	}
}

func TestNew_UnknownKindFails(t *testing.T) {
	cfg := map[string]config.Connection{"x": {Kind: "ssh"}}
	if _, err := connections.New(cfg, nil, nil); err == nil {
		t.Fatal("want an error for an unknown connection kind")
	}
}

// TestNew_RealExampleConfig proves the whole chain composes against the actual shipped
// examples/veduta.yaml: config.LoadPath -> secrets.ResolveAll -> connections.New, the same
// sequence cmd/veduta's serve command runs (minus the HTTP server itself). Every one of the
// five real connections (three http, one Docker, one more http) must build without error.
func TestNew_RealExampleConfig(t *testing.T) {
	for _, name := range []string{"VEDUTA_ADMIN_HASH", "IMMICH_KEY", "JELLYFIN_KEY", "ADGUARD_PASSWORD", "NTFY_TOKEN"} {
		t.Setenv(name, "placeholder-value-for-test")
	}
	snap, diags := config.LoadPath("../../examples/veduta.yaml")
	if diags.HasErrors() {
		t.Fatalf("config.LoadPath: %s", diags)
	}
	resolved, secretDiags := secrets.ResolveAll(snap.SecretRefs, secrets.DefaultResolver())
	if secretDiags.HasErrors() {
		t.Fatalf("secrets.ResolveAll: %s", secretDiags)
	}

	reg, err := connections.New(snap.Config.Connections, resolved, nil)
	if err != nil {
		t.Fatalf("connections.New: %v", err)
	}
	for _, id := range []string{"immich", "jellyfin", "adguard", "coding-server", "docker-local"} {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("connection %q was not built", id)
		}
	}
}
