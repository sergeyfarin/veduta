// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config implements milestone C1: the one config tree assembled from veduta.yaml plus an
// optional conf.d/*.yaml, per docs/01-architecture.md section 2. Loading pipeline: yaml.v3 into
// *yaml.Node (positions and comments preserved) -> merge conf.d (later files override, arrays
// replace) -> JSON Schema validation (schemas/config.v1.schema.json) -> decode into these typed
// structs -> semantic validation (duplicate ids, dangling references) -> an immutable *Snapshot.
// Every error carries file:line:col, recovered from the Node tree, not just a JSON Schema path.
//
// Every field below carries an explicit `yaml:"..."` tag matching the schema's exact key name -
// confirmed necessary, not stylistic: yaml.v3's untagged implicit key is strings.ToLower(field),
// so BaseURL would otherwise look for "baseurl" and silently leave "baseUrl" unset, with no
// decode error at all. Verified directly against the library before writing this file.
package config

// Config is the whole document, decoded and schema-valid, before semantic validation.
type Config struct {
	Version       int                   `yaml:"version"`
	Server        Server                `yaml:"server"`
	Auth          Auth                  `yaml:"auth"`
	Dashboard     Dashboard             `yaml:"dashboard"`
	Connections   map[string]Connection `yaml:"connections"`
	Integrations  []Integration         `yaml:"integrations"`
	Sections      []Section             `yaml:"sections"`
	Rules         []Rule                `yaml:"rules"`
	Notifications Notifications         `yaml:"notifications"`
}

// Server is the process's own listen/storage configuration.
type Server struct {
	Listen         string   `yaml:"listen"`
	DataDir        string   `yaml:"dataDir"`
	BaseURL        string   `yaml:"baseURL"`
	TrustedProxies []string `yaml:"trustedProxies"`
}

// AuthMode is the schema's auth.mode enum.
type AuthMode string

// AuthMode values.
const (
	AuthPassword AuthMode = "password"
	AuthForward  AuthMode = "forward"
	AuthNone     AuthMode = "none"
)

// Auth is required: docs/01-architecture.md D46 - a dashboard holding service credentials must
// state, in the file, who may reach it. Exactly one of Admin/Forward is set, matching Mode - the
// schema's own if/then already enforces that; Validate re-checks it at the Go level (D-phase
// callers must not have to trust the schema alone).
type Auth struct {
	Mode       AuthMode     `yaml:"mode"`
	Admin      *AdminAuth   `yaml:"admin"`
	Forward    *ForwardAuth `yaml:"forward"`
	SessionTTL string       `yaml:"sessionTTL"`
}

// AdminAuth backs auth.mode: password.
type AdminAuth struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"passwordHash"`
}

// PrivilegedOps is auth.forward.privilegedOperations: who may approve integrations and perform
// other privileged operations under forward auth. A group header is authorisation, not proof of
// current human presence - see the schema's own description.
type PrivilegedOps string

// PrivilegedOps values.
const (
	PrivilegedCLIOnly    PrivilegedOps = "cli-only"
	PrivilegedAdminGroup PrivilegedOps = "admin-group"
)

// ForwardAuth backs auth.mode: forward.
type ForwardAuth struct {
	TrustedProxies       []string      `yaml:"trustedProxies"`
	UserHeader           string        `yaml:"userHeader"`
	GroupsHeader         string        `yaml:"groupsHeader"`
	AdminGroups          []string      `yaml:"adminGroups"`
	PrivilegedOperations PrivilegedOps `yaml:"privilegedOperations"`
}

// Dashboard is presentation-level configuration: title, theme, layout defaults.
type Dashboard struct {
	Title   string `yaml:"title"`
	Theme   string `yaml:"theme"`
	Layout  Layout `yaml:"layout"`
	GroupBy string `yaml:"groupBy"` // section | tag
}

// Layout is dashboard.layout.
type Layout struct {
	Columns int    `yaml:"columns"`
	Gap     string `yaml:"gap"`
}

// Connection is a discriminated union over Kind: exactly one of HTTP/Docker is non-nil, matching
// Kind, the same way internal/state.Source and internal/widgets.BlockMedia carry a discriminator
// rather than being modelled as an interface - direct field access, no type assertion at call
// sites. See UnmarshalYAML in yamltypes.go.
type Connection struct {
	Kind   string // http | docker
	HTTP   *HTTPConnection
	Docker *DockerConnection
}

// HTTPConnection is connections.*.kind == http. Connections own every URL, credential, TLS
// setting, timeout and rate limit - integrations never see anything here (docs/01 section 7).
type HTTPConnection struct {
	BaseURL          string            `yaml:"baseUrl"`
	Auth             ConnectionAuth    `yaml:"auth"`
	Headers          map[string]string `yaml:"headers"`
	TLS              TLSConfig         `yaml:"tls"`
	Timeout          string            `yaml:"timeout"`
	MaxResponseBytes int               `yaml:"maxResponseBytes"`
	MaxRedirects     int               `yaml:"maxRedirects"`
	AllowedPaths     []string          `yaml:"allowedPaths"`
	RateLimit        RateLimit         `yaml:"rateLimit"`
	Concurrency      int               `yaml:"concurrency"`
}

// ConnectionAuth is httpConnection.auth: exactly one shape per Type, matching the schema's
// if/then. none carries nothing; header/query carry Name+Value; bearer carries Value only;
// basic carries Username+Password only.
type ConnectionAuth struct {
	Type     string    `yaml:"type"` // none | header | query | bearer | basic
	Name     string    `yaml:"name"`
	Value    SecretRef `yaml:"value"`
	Username string    `yaml:"username"`
	Password SecretRef `yaml:"password"`
}

// TLSConfig is httpConnection.tls.
type TLSConfig struct {
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify"`
	CAFile             string `yaml:"caFile"`
	ServerName         string `yaml:"serverName"`
}

// RateLimit is httpConnection.rateLimit.
type RateLimit struct {
	RPS   float64 `yaml:"rps"`
	Burst int     `yaml:"burst"`
}

// DockerConnection is connections.*.kind == docker.
type DockerConnection struct {
	Endpoint     string `yaml:"endpoint"`
	AllowActions bool   `yaml:"allowActions"`
	Timeout      string `yaml:"timeout"`
}

// Integration declares that an integration exists. It grants nothing - capabilities and routes
// come from veduta.lock.yaml, approved separately (Phase D).
type Integration struct {
	ID          string `yaml:"id"`
	Source      string `yaml:"source"`
	Enabled     bool   `yaml:"enabled"`
	Description string `yaml:"description"`
}

// Section groups cards under an optional heading.
type Section struct {
	Title     string `yaml:"title"`
	Collapsed bool   `yaml:"collapsed"`
	Cards     []Card `yaml:"cards"`
}

// Span is a card's footprint in grid units. Mirrors internal/fixtures.Span; that one exists for
// the --fixtures showcase and predates this one, but the schema is the single source of truth
// for both, so field-for-field they must never diverge.
type Span struct {
	Columns int `yaml:"columns" json:"columns,omitempty"`
	Rows    int `yaml:"rows" json:"rows,omitempty"`
}

// Card is one dashboard tile: which integration/operation runs, which connections its slots bind
// to, and its presentation. Content and health come from its CardState at runtime, never from
// here.
type Card struct {
	ID          string            `yaml:"id"`
	Title       string            `yaml:"title"`
	Href        string            `yaml:"href"`
	Icon        string            `yaml:"icon"`
	Integration string            `yaml:"integration"`
	Operation   string            `yaml:"operation"`
	Slots       map[string]string `yaml:"slots"`
	Params      map[string]any    `yaml:"params"`
	View        map[string]any    `yaml:"view"`
	Refresh     string            `yaml:"refresh"`
	Span        Span              `yaml:"span"`
	Tags        []string          `yaml:"tags"`
}

// Rule is one alerting rule: an expr predicate over declared signals and card execution state.
// When is not evaluated here - only load-time-checkable references (card ids, notify channels)
// are; the expression language itself is milestone D7/S3's decision, and signal-name-against-
// manifest checking needs a manifest, which does not exist until Phase D.
type Rule struct {
	ID       string   `yaml:"id"`
	When     string   `yaml:"when"`
	For      string   `yaml:"for"`
	Severity string   `yaml:"severity"`
	Notify   []string `yaml:"notify"`
	Resolve  bool     `yaml:"resolve"`
}

// Notifications is notifications.channels.
type Notifications struct {
	Channels map[string]Channel `yaml:"channels"`
}

// Channel is a discriminated union over Type, same pattern as Connection.
type Channel struct {
	Type    string // ntfy | webhook
	Ntfy    *NtfyChannel
	Webhook *WebhookChannel
}

// NtfyChannel is notifications.channels.*.type == ntfy.
type NtfyChannel struct {
	URL       string          `yaml:"url"`
	Topic     string          `yaml:"topic"`
	Token     SecretRef       `yaml:"token"`
	Priority  int             `yaml:"priority"`
	RateLimit NotifyRateLimit `yaml:"rateLimit"`
}

// WebhookChannel is notifications.channels.*.type == webhook. URL is secret-capable per the
// schema's own description: webhook URLs commonly embed a token in the path or query.
type WebhookChannel struct {
	URL       SecretRef            `yaml:"url"`
	Method    string               `yaml:"method"`
	Headers   map[string]SecretRef `yaml:"headers"`
	RateLimit NotifyRateLimit      `yaml:"rateLimit"`
}

// NotifyRateLimit is the rateLimit shape shared by both channel kinds.
type NotifyRateLimit struct {
	PerHour  int    `yaml:"perHour"`
	Cooldown string `yaml:"cooldown"`
}
