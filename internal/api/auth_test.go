// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/argon2"

	"veduta.dev/veduta/internal/auth"
	"veduta.dev/veduta/internal/storage"
)

func passwordAuthServer(t *testing.T) *Server {
	t.Helper()
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	salt := []byte("0123456789abcdef")
	digest := argon2.IDKey([]byte("correct horse"), salt, 1, 8*1024, 1, 32)
	hash := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest))
	service, err := auth.New(context.Background(), auth.Config{Store: store, Username: "admin", PasswordHash: hash, SessionTTL: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{Listen: "127.0.0.1:0", AuthConfigured: true, Auth: service, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestPasswordSessionCookiesProtectAPIAndLogoutRevokes(t *testing.T) {
	server := passwordAuthServer(t)
	handler := server.Handler()

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", bytes.NewBufferString(`{"username":"admin","password":"correct horse"}`))
	login.Header.Set("Content-Type", "application/json")
	login.TLS = &tls.ConnectionState{}
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusCreated {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range loginRec.Result().Cookies() {
		switch cookie.Name {
		case auth.SessionCookie:
			sessionCookie = cookie
		case auth.CSRFCookie:
			csrfCookie = cookie
		}
		if !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("unsafe cookie flags: %+v", cookie)
		}
	}
	if sessionCookie == nil || csrfCookie == nil || !sessionCookie.HttpOnly || csrfCookie.HttpOnly {
		t.Fatalf("session=%+v csrf=%+v", sessionCookie, csrfCookie)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	me.AddCookie(sessionCookie)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, me)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", meRec.Code, meRec.Body.String())
	}

	crossOrigin := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil)
	crossOrigin.AddCookie(sessionCookie)
	crossOrigin.AddCookie(csrfCookie)
	crossOrigin.Header.Set("Origin", "https://attacker.invalid")
	crossOriginRec := httptest.NewRecorder()
	handler.ServeHTTP(crossOriginRec, crossOrigin)
	if crossOriginRec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation status=%d", crossOriginRec.Code)
	}

	logout := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logout.Header.Set(auth.CSRFHeader, csrfCookie.Value)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logout)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}

	again := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	again.AddCookie(sessionCookie)
	againRec := httptest.NewRecorder()
	handler.ServeHTTP(againRec, again)
	if againRec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d", againRec.Code)
	}
}

func TestForwardAuthRejectsSpoofedUntrustedPeer(t *testing.T) {
	forward, err := auth.NewForward(auth.ForwardConfig{TrustedProxies: []string{"192.0.2.0/24"}, UserHeader: "X-User"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{Listen: "127.0.0.1:0", ForwardAuth: forward, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	spoofed := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	spoofed.RemoteAddr = "198.51.100.10:1234"
	spoofed.Header.Set("X-User", "attacker")
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, spoofed)
	if denied.Code != http.StatusUnauthorized || denied.Header().Get("X-Veduta-Auth-Mode") != "forward" {
		t.Fatalf("status=%d mode=%q", denied.Code, denied.Header().Get("X-Veduta-Auth-Mode"))
	}

	trusted := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	trusted.RemoteAddr = "192.0.2.10:1234"
	trusted.Header.Set("X-User", "alice")
	ok := httptest.NewRecorder()
	handler.ServeHTTP(ok, trusted)
	if ok.Code != http.StatusOK || !bytes.Contains(ok.Body.Bytes(), []byte(`"username":"alice"`)) {
		t.Fatalf("status=%d body=%s", ok.Code, ok.Body.String())
	}
}
