// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"veduta.dev/veduta/internal/storage"
)

func testHash(password string) string {
	salt := []byte("0123456789abcdef")
	hash := argon2.IDKey([]byte(password), salt, 1, 8*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}

func testService(t *testing.T, now *time.Time) (*Service, *storage.Store) {
	t.Helper()
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := New(context.Background(), Config{Store: store, Username: "admin", PasswordHash: testHash("correct horse"), SessionTTL: "1h", Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func TestLoginAlwaysVerifiesAndLocksRepeatedFailures(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	for i := 0; i < 5; i++ {
		if _, err := service.Login(context.Background(), "unknown", "wrong", "test", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := service.Login(context.Background(), "unknown", "wrong", "test", "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("locked attempt: %v", err)
	}
	if _, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.2"); err != nil {
		t.Fatalf("different source should remain usable: %v", err)
	}
}

func TestSessionRotationExpiryAndServerSideLogout(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, store := testService(t, &now)
	first, err := service.Login(context.Background(), "admin", "correct horse", "one", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(context.Background(), "admin", "correct horse", "two", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.CSRFToken == second.CSRFToken {
		t.Fatal("login reused session material")
	}
	if _, _, err := service.Authenticate(context.Background(), first.Token); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background(), first.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Authenticate(context.Background(), first.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("revoked session authenticated: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, _, err := service.Authenticate(context.Background(), second.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("expired session authenticated: %v", err)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired sessions=%d err=%v", count, err)
	}
}

func TestCSRFMustMatchStoredCookieAndHeader(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckCSRF(context.Background(), session.Token, session.CSRFToken, "attacker"); !errors.Is(err, ErrCSRF) {
		t.Fatalf("mismatched header: %v", err)
	}
	if identity, err := service.CheckCSRF(context.Background(), session.Token, session.CSRFToken, session.CSRFToken); err != nil || identity.Username != "admin" {
		t.Fatalf("valid CSRF: identity=%+v err=%v", identity, err)
	}
}

func TestReconfigureReplacesPasswordGeneration(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	if err := service.Reconfigure(context.Background(), Config{Username: "operator", PasswordHash: testHash("new password"), SessionTTL: "2h"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old credentials accepted: %v", err)
	}
	session, err := service.Login(context.Background(), "operator", "new password", "test", "192.0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if got := session.ExpiresAt.Sub(now); got != 2*time.Hour {
		t.Fatalf("session TTL=%s", got)
	}
}

func TestSudoWindowExpiresAndLogoutRevokesIt(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := service.Authenticate(context.Background(), session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if service.AllowsPrivileged(identity) {
		t.Fatal("new session unexpectedly has a sudo window")
	}
	if err := service.OpenSudo(context.Background(), session.Token, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if !service.AllowsPrivileged(identity) {
		t.Fatal("verified password did not open sudo window")
	}
	now = now.Add(6 * time.Minute)
	if service.AllowsPrivileged(identity) {
		t.Fatal("expired sudo window remained open")
	}
	if err := service.OpenSudo(context.Background(), session.Token, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background(), session.Token); err != nil {
		t.Fatal(err)
	}
	if service.AllowsPrivileged(identity) {
		t.Fatal("logout did not revoke sudo window")
	}
}
