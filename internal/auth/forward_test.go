// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardTrustsOnlyDirectAllowedPeer(t *testing.T) {
	forward, err := NewForward(ForwardConfig{TrustedProxies: []string{"192.0.2.0/24"}, UserHeader: "X-User"})
	if err != nil {
		t.Fatal(err)
	}
	trusted := httptest.NewRequest(http.MethodGet, "http://veduta.test/api/v1/auth/me", nil)
	trusted.RemoteAddr = "192.0.2.10:443"
	trusted.Header.Set("X-User", "alice")
	identity, err := forward.Authenticate(trusted)
	if err != nil || identity.Username != "alice" || identity.Mode != "forward" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}

	spoofed := trusted.Clone(trusted.Context())
	spoofed.RemoteAddr = "198.51.100.10:443"
	spoofed.Header.Set("X-Forwarded-For", "192.0.2.10")
	if _, err := forward.Authenticate(spoofed); !errors.Is(err, ErrNoSession) {
		t.Fatalf("spoofed header from untrusted peer: %v", err)
	}
}

func TestForwardAdminGroupAndCrossSiteMutation(t *testing.T) {
	forward, err := NewForward(ForwardConfig{TrustedProxies: []string{"127.0.0.1"}, UserHeader: "X-User", GroupsHeader: "X-Groups", AdminGroups: []string{"veduta-admins"}, PrivilegedOperations: "admin-group"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://veduta.test/api/v1/cards/x/refresh", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-User", "alice")
	r.Header.Set("X-Groups", "users, veduta-admins")
	identity, err := forward.Authenticate(r)
	if err != nil || !forward.AllowsPrivileged(identity) {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if !errors.Is(forward.CheckMutation(r), ErrCSRF) {
		t.Fatal("cross-site mutation was accepted")
	}
}
