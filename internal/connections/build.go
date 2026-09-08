// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/secrets"
)

// New builds a Registry from configuration and already-resolved secrets (internal/secrets -
// milestone C2's ResolveAll, run once at config load time, never per request). It fails loudly
// rather than building a connection with a silently-empty credential: every ${secret:NAME} a
// connection references must already be present in resolved, or New refuses to start - the same
// discipline docs/01-architecture.md section 2 applies to config loading itself.
func New(connections map[string]config.Connection, resolved map[string]secrets.Value, logger *slog.Logger) (Registry, error) {
	if logger == nil {
		logger = slog.Default()
	}
	r := &registry{
		connections: make(map[string]*Connection, len(connections)),
		clients:     make(map[string]*client, len(connections)),
	}
	for id, cfg := range connections {
		conn, err := buildConnection(id, cfg, resolved)
		if err != nil {
			return nil, err
		}
		r.connections[id] = conn
		if conn.Kind == KindHTTP {
			c, err := newHTTPClient(id, conn.HTTP, logger)
			if err != nil {
				return nil, err
			}
			r.clients[id] = c
		}
	}
	return r, nil
}

func buildConnection(id string, cfg config.Connection, resolved map[string]secrets.Value) (*Connection, error) {
	switch cfg.Kind {
	case "http":
		http, err := buildHTTPConfig(id, cfg.HTTP, resolved)
		if err != nil {
			return nil, err
		}
		return &Connection{ID: id, Kind: KindHTTP, HTTP: http}, nil
	case "docker":
		docker, err := buildDockerConfig(id, cfg.Docker)
		if err != nil {
			return nil, err
		}
		return &Connection{ID: id, Kind: KindDocker, Docker: docker}, nil
	default:
		return nil, fmt.Errorf("connection %q: unknown kind %q", id, cfg.Kind)
	}
}

func buildHTTPConfig(id string, cfg *config.HTTPConnection, resolved map[string]secrets.Value) (*HTTPConfig, error) {
	auth, err := buildAuth(id, cfg.Auth, resolved)
	if err != nil {
		return nil, err
	}
	timeout, err := parseDurationDefault(cfg.Timeout, defaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("connection %q: timeout: %w", id, err)
	}
	return &HTTPConfig{
		BaseURL: cfg.BaseURL,
		Auth:    auth,
		Headers: cfg.Headers,
		TLS: TLSConfig{
			InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
			CAFile:             cfg.TLS.CAFile,
			ServerName:         cfg.TLS.ServerName,
		},
		Timeout:          timeout,
		MaxResponseBytes: int64(cfg.MaxResponseBytes),
		MaxRedirects:     cfg.MaxRedirects,
		AllowedPaths:     cfg.AllowedPaths,
		RateLimit:        RateLimit{RPS: cfg.RateLimit.RPS, Burst: cfg.RateLimit.Burst},
		Concurrency:      cfg.Concurrency,
	}, nil
}

func buildAuth(id string, cfg config.ConnectionAuth, resolved map[string]secrets.Value) (Auth, error) {
	value, err := resolveRef(id, "auth.value", cfg.Value, resolved)
	if err != nil {
		return Auth{}, err
	}
	pass, err := resolveRef(id, "auth.password", cfg.Password, resolved)
	if err != nil {
		return Auth{}, err
	}
	authType := cfg.Type
	if authType == "" {
		authType = "none"
	}
	return Auth{
		Type:  AuthType(authType),
		Name:  cfg.Name,
		Value: value,
		User:  cfg.Username,
		Pass:  pass,
	}, nil
}

func buildDockerConfig(id string, cfg *config.DockerConnection) (*DockerConfig, error) {
	timeout, err := parseDurationDefault(cfg.Timeout, defaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("connection %q: timeout: %w", id, err)
	}
	return &DockerConfig{Endpoint: cfg.Endpoint, AllowActions: cfg.AllowActions, Timeout: timeout}, nil
}

// resolveRef turns a config.SecretRef into a secrets.Value: a literal is wrapped as-is (still
// opaque past this point - Auth.Value/Pass are secrets.Value even for a "secret" that was never
// actually secret), and a ${secret:NAME} reference is looked up in resolved, which must already
// hold it - config.Load + secrets.ResolveAll already produced a diagnostic naming the exact
// config location for anything that failed to resolve, so reaching here with it still missing
// would mean a caller skipped that step, not a normal runtime condition.
func resolveRef(id, field string, ref config.SecretRef, resolved map[string]secrets.Value) (secrets.Value, error) {
	if !ref.IsSecret() {
		return secrets.New(ref.Literal), nil
	}
	if ref.Template != "" {
		composed := ref.Template
		for _, name := range ref.Names {
			v, ok := resolved[name]
			if !ok {
				return secrets.Value{}, fmt.Errorf("connection %q: %s references secret %q, which was not resolved", id, field, name)
			}
			composed = strings.Replace(composed, "${secret:"+name+"}", v.Reveal(), 1)
		}
		return secrets.New(composed), nil
	}
	v, ok := resolved[ref.Name]
	if !ok {
		return secrets.Value{}, fmt.Errorf("connection %q: %s references secret %q, which was not resolved", id, field, ref.Name)
	}
	return v, nil
}

func parseDurationDefault(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}
