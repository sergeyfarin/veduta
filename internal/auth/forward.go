// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
)

// ForwardConfig defines headers accepted only from explicitly trusted proxy networks.
type ForwardConfig struct {
	TrustedProxies       []string
	UserHeader           string
	GroupsHeader         string
	AdminGroups          []string
	PrivilegedOperations string
}

type forwardGeneration struct {
	trusted              []netip.Prefix
	userHeader           string
	groupsHeader         string
	adminGroups          map[string]struct{}
	privilegedOperations string
}

// Forward authenticates identities asserted by a trusted reverse proxy.
type Forward struct {
	mu         sync.RWMutex
	generation forwardGeneration
}

// NewForward validates and creates a trusted-header authenticator.
func NewForward(cfg ForwardConfig) (*Forward, error) {
	forward := &Forward{}
	if err := forward.Reconfigure(cfg); err != nil {
		return nil, err
	}
	return forward, nil
}

// Reconfigure atomically replaces the trusted networks and header policy.
func (f *Forward) Reconfigure(cfg ForwardConfig) error {
	if cfg.UserHeader == "" {
		cfg.UserHeader = "Remote-User"
	}
	if cfg.PrivilegedOperations == "" {
		cfg.PrivilegedOperations = "cli-only"
	}
	generation := forwardGeneration{userHeader: http.CanonicalHeaderKey(cfg.UserHeader), groupsHeader: http.CanonicalHeaderKey(cfg.GroupsHeader), adminGroups: map[string]struct{}{}, privilegedOperations: cfg.PrivilegedOperations}
	for _, raw := range cfg.TrustedProxies {
		prefix, err := parsePrefix(raw)
		if err != nil {
			return errors.New("auth: invalid trusted proxy " + raw)
		}
		generation.trusted = append(generation.trusted, prefix)
	}
	if len(generation.trusted) == 0 {
		return errors.New("auth: at least one trusted proxy is required")
	}
	for _, group := range cfg.AdminGroups {
		generation.adminGroups[group] = struct{}{}
	}
	if generation.privilegedOperations == "admin-group" && (generation.groupsHeader == "" || len(generation.adminGroups) == 0) {
		return errors.New("auth: admin-group requires a groups header and at least one admin group")
	}
	f.mu.Lock()
	f.generation = generation
	f.mu.Unlock()
	return nil
}

// Authenticate accepts forwarded headers only when the direct peer is trusted.
func (f *Forward) Authenticate(r *http.Request) (Identity, error) {
	f.mu.RLock()
	generation := f.generation
	f.mu.RUnlock()
	peer, err := peerAddress(r.RemoteAddr)
	if err != nil || !contains(generation.trusted, peer) {
		return Identity{}, ErrNoSession
	}
	username := strings.TrimSpace(r.Header.Get(generation.userHeader))
	if username == "" || len(username) > 256 {
		return Identity{}, ErrNoSession
	}
	groups := splitGroups(r.Header.Get(generation.groupsHeader))
	admin := false
	for _, group := range groups {
		if _, ok := generation.adminGroups[group]; ok {
			admin = true
			break
		}
	}
	sum := sha256.Sum256([]byte("forward\x00" + username))
	return Identity{Username: username, SessionID: hex.EncodeToString(sum[:]), Mode: "forward", Admin: admin, Groups: groups}, nil
}

// AllowsPrivileged reports whether identity may use REST privileged operations.
func (f *Forward) AllowsPrivileged(identity Identity) bool {
	f.mu.RLock()
	policy := f.generation.privilegedOperations
	f.mu.RUnlock()
	return policy == "admin-group" && identity.Admin
}

// CheckMutation rejects browser cross-site requests in header-auth mode.
func (f *Forward) CheckMutation(r *http.Request) error {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return ErrCSRF
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
		return ErrCSRF
	}
	return nil
}

func parsePrefix(raw string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked(), nil
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(address, address.BitLen()), nil
}

func peerAddress(remote string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(host)
}

func contains(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func splitGroups(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if group := strings.TrimSpace(part); group != "" {
			out = append(out, group)
		}
	}
	return out
}
