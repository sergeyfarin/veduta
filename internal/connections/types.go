// SPDX-License-Identifier: AGPL-3.0-or-later

// Package connections implements milestone D1: the credential boundary. Integrations never see a
// URL or a credential - only a slot name; this package holds the real connection, injects auth,
// and is the one place a request actually leaves the process. See docs/01-architecture.md
// section 3.
package connections

import (
	"time"

	"veduta.dev/veduta/internal/secrets"
)

// Kind selects which of the two connection shapes a Connection carries. 0.3 adds "ssh", "agent"
// (docs/01's own note); nothing here should assume these two are the only ones forever.
type Kind string

// Kind values.
const (
	KindHTTP   Kind = "http"
	KindDocker Kind = "docker"
)

// Connection is a fully resolved, ready-to-use connection: every secret it needs has already
// been resolved to a real value (internal/secrets), and it is built once at config load time,
// not per request. Exactly one of HTTP/Docker is non-nil, matching Kind - see New.
type Connection struct {
	ID     string
	Kind   Kind
	HTTP   *HTTPConfig
	Docker *DockerConfig
}

// AuthType is how a request authenticates against an HTTPConfig's upstream.
type AuthType string

// AuthType values.
const (
	AuthNone   AuthType = "none"
	AuthHeader AuthType = "header"
	AuthQuery  AuthType = "query"
	AuthBearer AuthType = "bearer"
	AuthBasic  AuthType = "basic"
)

// Auth carries a connection's credential, already resolved. Value/Pass are secrets.Value even
// when the underlying config held a literal (never a secret) - see internal/config.SecretRef and
// resolveSecretRef in build.go - so this type never has to distinguish "a real secret" from
// "a literal that happens to look like a credential" at the point it gets injected into a
// request; both are handled identically and neither is ever logged.
type Auth struct {
	Type  AuthType
	Name  string // header or query parameter name (header/query only)
	Value secrets.Value
	User  string
	Pass  secrets.Value
}

// TLSConfig is an HTTPConfig's transport security. InsecureSkipVerify requires the operator to
// have set it explicitly in configuration - New logs a warning for every connection with it set,
// so turning it on is never silent.
type TLSConfig struct {
	InsecureSkipVerify bool
	CAFile             string
	ServerName         string
}

// RateLimit is requests-per-second plus burst, enforced per connection (rate.go).
type RateLimit struct {
	RPS   float64
	Burst int
}

// HTTPConfig is connections.*.kind == http, resolved and ready. See docs/01-architecture.md
// section 3 for the exact field set and defaults this mirrors.
type HTTPConfig struct {
	BaseURL          string
	Auth             Auth
	Headers          map[string]string
	TLS              TLSConfig
	Timeout          time.Duration
	MaxResponseBytes int64
	MaxRedirects     int
	AllowedPaths     []string
	RateLimit        RateLimit
	Concurrency      int
}

// DockerConfig is connections.*.kind == docker, resolved and ready.
type DockerConfig struct {
	Endpoint     string
	AllowActions bool
	Timeout      time.Duration
}
