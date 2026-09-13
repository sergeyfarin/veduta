// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth implements the single-admin password session boundary.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/storage"
)

const (
	// SessionCookie is the HttpOnly browser session cookie.
	SessionCookie = "veduta_session"
	// CSRFCookie is the readable half of the double-submit CSRF token.
	CSRFCookie = "veduta_csrf"
	// CSRFHeader carries the client-provided half of the double-submit token.
	CSRFHeader = "X-CSRF-Token"
)

var (
	// ErrInvalidCredentials deliberately covers both an unknown user and a bad password.
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrRateLimited indicates a temporarily locked login source.
	ErrRateLimited = errors.New("too many login attempts; try again later")
	// ErrNoSession indicates an absent, expired, or revoked session.
	ErrNoSession = errors.New("authentication required")
	// ErrCSRF indicates a missing or mismatched double-submit token.
	ErrCSRF = errors.New("invalid CSRF token")
	// ErrBusy indicates password verification was refused without running, because too many
	// verifications were already in flight or queued - see verifier.
	ErrBusy = errors.New("too many authentication requests in flight; try again shortly")
)

// Config contains the persistent and configured inputs for password authentication.
type Config struct {
	Store           *storage.Store
	Username        string
	PasswordHash    string
	ResolvedSecrets map[string]secrets.Value
	SessionTTL      string
	Now             func() time.Time
}

// Session is the browser credential and its associated CSRF token.
type Session struct {
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

// Identity is the authenticated request principal.
type Identity struct {
	Username  string   `json:"username"`
	SessionID string   `json:"-"`
	Mode      string   `json:"mode"`
	Admin     bool     `json:"admin"`
	Groups    []string `json:"groups,omitempty"`
}

type attempts struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

// Service verifies the configured administrator and owns server-side sessions.
type Service struct {
	store    *storage.Store
	configMu sync.RWMutex
	username string
	params   argonParams
	ttl      time.Duration
	now      func() time.Time

	verifier *verifier

	mu        sync.Mutex
	attempts  map[string]attempts
	sudoMu    sync.Mutex
	sudoUntil map[string]time.Time
}

// Reconfigure atomically replaces the administrator hash and session lifetime after validation.
func (s *Service) Reconfigure(ctx context.Context, cfg Config) error {
	passwordHash, err := resolvePasswordHash(cfg.PasswordHash, cfg.ResolvedSecrets)
	if err != nil {
		return err
	}
	params, err := parseArgon2ID(passwordHash.Reveal())
	if err != nil {
		return err
	}
	ttl, err := parseTTL(cfg.SessionTTL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return errors.New("auth: username is required")
	}
	_, err = s.store.DB().ExecContext(ctx, `UPDATE users SET username=?,password_hash=? WHERE id='admin'`, cfg.Username, passwordHash.Reveal())
	if err != nil {
		return err
	}
	s.configMu.Lock()
	s.username = cfg.Username
	s.params = params
	s.ttl = ttl
	s.configMu.Unlock()
	return nil
}

// New validates the Argon2id hash and synchronises the configured single administrator.
func New(ctx context.Context, cfg Config) (*Service, error) {
	passwordHash, err := resolvePasswordHash(cfg.PasswordHash, cfg.ResolvedSecrets)
	if err != nil {
		return nil, err
	}
	params, err := parseArgon2ID(passwordHash.Reveal())
	if err != nil {
		return nil, err
	}
	if cfg.Store == nil || strings.TrimSpace(cfg.Username) == "" {
		return nil, errors.New("auth: store and username are required")
	}
	ttl, err := parseTTL(cfg.SessionTTL)
	if err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	now := cfg.Now().UTC().Format(time.RFC3339Nano)
	_, err = cfg.Store.DB().ExecContext(ctx, `INSERT INTO users(id,username,password_hash,created_at) VALUES('admin',?,?,?) ON CONFLICT(id) DO UPDATE SET username=excluded.username,password_hash=excluded.password_hash`, cfg.Username, passwordHash.Reveal(), now)
	if err != nil {
		return nil, err
	}
	return &Service{store: cfg.Store, username: cfg.Username, params: params, ttl: ttl, now: cfg.Now,
		verifier: newVerifier(maxConcurrentVerifications, maxQueuedVerifications),
		attempts: map[string]attempts{}, sudoUntil: map[string]time.Time{}}, nil
}

// Login verifies credentials, applies source lockout, and creates a fresh server-side session.
func (s *Service) Login(ctx context.Context, username, password, userAgent, ip string) (Session, error) {
	s.configMu.RLock()
	configuredUsername := s.username
	params := s.params
	ttl := s.ttl
	s.configMu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(username)) + "\x00" + ip
	now := s.now()
	s.mu.Lock()
	a := s.attempts[key]
	locked := now.Before(a.lockedUntil)
	s.mu.Unlock()

	// Admission comes before the hash, not after it. The previous order ran a full Argon2
	// verification and then returned ErrRateLimited, so a locked-out source still cost the host
	// 64 MiB and the lockout bounded nothing an attacker cared about. Returning here leaks
	// nothing the caller does not already learn from the 429 the handler sends.
	if locked {
		return Session{}, ErrRateLimited
	}
	valid, err := s.verifier.verify(func() bool { return verifyPassword(params, password) })
	if err != nil {
		return Session{}, err
	}
	// Compared after the hash, never instead of it, so an unknown username costs the same as a
	// known one with a wrong password.
	valid = valid && subtle.ConstantTimeCompare([]byte(username), []byte(configuredUsername)) == 1
	if !valid {
		s.recordFailure(key, now)
		return Session{}, ErrInvalidCredentials
	}
	s.mu.Lock()
	delete(s.attempts, key)
	s.mu.Unlock()

	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	expires := now.Add(ttl)
	_, err = s.store.DB().ExecContext(ctx, `INSERT INTO sessions(id,user_id,csrf_token,created_at,expires_at,last_seen_at,user_agent,ip) VALUES(?,?,?,?,?,?,?,?)`, tokenHash(token), "admin", csrf, now.UTC().Format(time.RFC3339Nano), expires.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), userAgent, ip)
	if err != nil {
		return Session{}, err
	}
	_, _ = s.store.DB().ExecContext(ctx, `UPDATE users SET last_login_at=? WHERE id='admin'`, now.UTC().Format(time.RFC3339Nano))
	_ = s.PruneExpired(ctx)
	return Session{Token: token, CSRFToken: csrf, ExpiresAt: expires}, nil
}

// maxTrackedSources bounds the attempts map. Its keys contain a caller-supplied username, so a
// burst of distinct usernames from one source would otherwise grow it without limit - pruning
// entries that have merely expired does not bound a burst that is still arriving. Past the
// ceiling the least recently active entry is evicted rather than tracking being refused, so an
// attacker cannot keep their own counter out of the map by flooding it with others.
const maxTrackedSources = 4096

func (s *Service) recordFailure(key string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, tracked := s.attempts[key]; !tracked {
		s.evictLocked(now)
	}
	a := s.attempts[key]
	if a.windowStart.IsZero() || now.Sub(a.windowStart) >= 10*time.Minute {
		a = attempts{windowStart: now}
	}
	a.count++
	if a.count >= 5 {
		a.lockedUntil = now.Add(15 * time.Minute)
	}
	s.attempts[key] = a
}

// evictLocked makes room for one new entry: expired windows that are no longer locked out go
// first, and if that is not enough, the entry with the oldest window. Caller holds s.mu.
func (s *Service) evictLocked(now time.Time) {
	if len(s.attempts) < maxTrackedSources {
		return
	}
	for key, a := range s.attempts {
		if now.Sub(a.windowStart) >= 10*time.Minute && !now.Before(a.lockedUntil) {
			delete(s.attempts, key)
		}
	}
	if len(s.attempts) < maxTrackedSources {
		return
	}
	oldestKey, oldest := "", time.Time{}
	for key, a := range s.attempts {
		if oldest.IsZero() || a.windowStart.Before(oldest) {
			oldestKey, oldest = key, a.windowStart
		}
	}
	delete(s.attempts, oldestKey)
}

// Authenticate returns the identity for an unexpired session token.
func (s *Service) Authenticate(ctx context.Context, token string) (Identity, string, error) {
	if token == "" {
		return Identity{}, "", ErrNoSession
	}
	var username, csrf, expires string
	err := s.store.DB().QueryRowContext(ctx, `SELECT users.username,sessions.csrf_token,sessions.expires_at FROM sessions JOIN users ON users.id=sessions.user_id WHERE sessions.id=?`, tokenHash(token)).Scan(&username, &csrf, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, "", ErrNoSession
	}
	if err != nil {
		return Identity{}, "", err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !s.now().Before(expiresAt) {
		_, _ = s.store.DB().ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, tokenHash(token))
		return Identity{}, "", ErrNoSession
	}
	_, _ = s.store.DB().ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, s.now().UTC().Format(time.RFC3339Nano), tokenHash(token))
	return Identity{Username: username, SessionID: tokenHash(token), Mode: "password", Admin: true}, csrf, nil
}

// Logout revokes token in the server-side store.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	sessionID := tokenHash(token)
	_, err := s.store.DB().ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, sessionID)
	s.sudoMu.Lock()
	delete(s.sudoUntil, sessionID)
	s.sudoMu.Unlock()
	return err
}

// OpenSudo re-verifies the password and opens a five-minute privileged-operation window.
//
// It is throttled and pooled exactly as Login is. Requiring a valid session first makes this
// narrower than the login surface, not exempt from it: a single stolen or shared session could
// otherwise drive unbounded Argon2 verifications, and until this it had no lockout, no attempt
// record and no admission control of any kind. Failures are counted against the session rather
// than the username, which is the thing being guessed against here.
func (s *Service) OpenSudo(ctx context.Context, sessionToken, password, ip string) error {
	identity, _, err := s.Authenticate(ctx, sessionToken)
	if err != nil {
		return err
	}
	s.configMu.RLock()
	params := s.params
	s.configMu.RUnlock()

	key := "sudo\x00" + identity.SessionID + "\x00" + ip
	now := s.now()
	s.mu.Lock()
	locked := now.Before(s.attempts[key].lockedUntil)
	s.mu.Unlock()
	if locked {
		return ErrRateLimited
	}

	valid, err := s.verifier.verify(func() bool { return verifyPassword(params, password) })
	if err != nil {
		return err
	}
	if !valid {
		s.recordFailure(key, now)
		return ErrInvalidCredentials
	}
	s.mu.Lock()
	delete(s.attempts, key)
	s.mu.Unlock()

	s.sudoMu.Lock()
	s.sudoUntil[identity.SessionID] = s.now().Add(5 * time.Minute)
	s.sudoMu.Unlock()
	return nil
}

// AllowsPrivileged reports whether identity has a current sudo window.
func (s *Service) AllowsPrivileged(identity Identity) bool {
	s.sudoMu.Lock()
	defer s.sudoMu.Unlock()
	until := s.sudoUntil[identity.SessionID]
	if !s.now().Before(until) {
		delete(s.sudoUntil, identity.SessionID)
		return false
	}
	return true
}

// CheckCSRF authenticates the session and compares its stored, cookie, and header tokens.
func (s *Service) CheckCSRF(ctx context.Context, sessionToken, cookieToken, headerToken string) (Identity, error) {
	identity, stored, err := s.Authenticate(ctx, sessionToken)
	if err != nil {
		return Identity{}, err
	}
	if cookieToken == "" || headerToken == "" || subtle.ConstantTimeCompare([]byte(stored), []byte(cookieToken)) != 1 || subtle.ConstantTimeCompare([]byte(stored), []byte(headerToken)) != 1 {
		return Identity{}, ErrCSRF
	}
	return identity, nil
}

// PruneExpired removes expired sessions immediately; the storage janitor also handles retention data.
func (s *Service) PruneExpired(ctx context.Context) error {
	_, err := s.store.DB().ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now().UTC().Format(time.RFC3339Nano))
	return err
}

// ClientIP returns the direct peer address used for rate-limit attribution.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

var secretPlaceholder = regexp.MustCompile(`^\$\{secret:([A-Za-z0-9_]+)\}$`)

func resolvePasswordHash(configured string, resolved map[string]secrets.Value) (secrets.Value, error) {
	match := secretPlaceholder.FindStringSubmatch(configured)
	if match == nil {
		return secrets.New(configured), nil
	}
	value, ok := resolved[match[1]]
	if !ok {
		return secrets.Value{}, errors.New("auth: configured password hash secret is unresolved")
	}
	return value, nil
}

func parseTTL(value string) (time.Duration, error) {
	if value == "" {
		return 30 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseUint(strings.TrimSuffix(value, "d"), 10, 32)
		if err != nil || days == 0 {
			return 0, errors.New("auth: invalid sessionTTL")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil || ttl <= 0 {
		return 0, errors.New("auth: invalid sessionTTL")
	}
	return ttl, nil
}
