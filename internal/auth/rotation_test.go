// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func reconfigureTo(t *testing.T, service *Service, username, password string) {
	t.Helper()
	if err := service.Reconfigure(context.Background(), Config{Username: username, PasswordHash: testHash(password), SessionTTL: "1h"}); err != nil {
		t.Fatal(err)
	}
}

// TestChangingCredentialsRevokesExistingSessionsAndSudo is the finding: rotating the
// administrator's username and password left every existing session authenticated, still holding
// its sudo window. Rotation that cannot evict someone holding a stolen session is not rotation.
func TestChangingCredentialsRevokesExistingSessionsAndSudo(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.OpenSudo(context.Background(), session.Token, "correct horse", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	identity, _, err := service.Authenticate(context.Background(), session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !service.AllowsPrivileged(identity) {
		t.Fatal("expected a sudo window before rotation")
	}

	reconfigureTo(t, service, "newadmin", "new passphrase")

	if _, _, err = service.Authenticate(context.Background(), session.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("the old session still authenticates after rotation: %v", err)
	}
	if service.AllowsPrivileged(identity) {
		t.Fatal("the old session still holds its sudo window after rotation")
	}
}

// TestUnchangedCredentialsKeepSessions is the other half, and the reason revocation is
// conditional: New runs on every startup, so revoking unconditionally would log the operator out
// every time the process restarted.
func TestUnchangedCredentialsKeepSessions(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, store := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}

	// A restart: a brand new Service over the same store, same configured credentials.
	restarted, err := New(context.Background(), Config{Store: store, Username: "admin", PasswordHash: testHash("correct horse"), SessionTTL: "1h", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = restarted.Authenticate(context.Background(), session.Token); err != nil {
		t.Fatalf("an unchanged credential must not end existing sessions across a restart: %v", err)
	}
}

// TestRestartWithChangedCredentialsRevokes covers the startup path specifically: an operator who
// edits the configured password while the process is down expects the same eviction as one who
// changes it live. The session survived the restart in the database, so something has to notice.
func TestRestartWithChangedCredentialsRevokes(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, store := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(context.Background(), Config{Store: store, Username: "admin", PasswordHash: testHash("changed while down"), SessionTTL: "1h", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = restarted.Authenticate(context.Background(), session.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a credential changed while the process was down must end existing sessions: %v", err)
	}
}

// TestRolledBackActivationDoesNotRestoreRevokedSessions pins the behaviour when runtime
// activation reconfigures authentication and then a LATER step fails: cmd/veduta's rollback
// reconfigures authentication back to the previous snapshot, which is itself a credential change.
// The decision recorded here is that this is fail-closed - sessions revoked by the forward change
// stay revoked, rather than a rollback resurrecting credentials' worth of access that was briefly
// live under a different password. Deleted rows cannot come back, and nothing here tries.
func TestRolledBackActivationDoesNotRestoreRevokedSessions(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}

	reconfigureTo(t, service, "newadmin", "new passphrase") // activation's auth step
	reconfigureTo(t, service, "admin", "correct horse")     // a later step failed; rollback

	if _, _, err = service.Authenticate(context.Background(), session.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a rollback must not restore a session the forward change revoked: %v", err)
	}
	// The rolled-back credentials must still work for a NEW login, or the rollback achieved
	// nothing.
	if _, err = service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1"); err != nil {
		t.Fatalf("the rolled-back credentials must still authenticate: %v", err)
	}
}

// TestLoginInFlightDuringRotationCannotOutliveIt is the race the review found. A login reads the
// configured hash, spends the whole Argon2 cost verifying against it, and only then inserts a
// session; a rotation completing inside that window would delete the sessions that existed and
// then watch a new one appear, authorised by a password that no longer exists. Run under -race,
// this also covers the locking itself.
func TestLoginInFlightDuringRotationCannotOutliveIt(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)

	var mu sync.Mutex
	var tokens []string
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
				if err != nil {
					continue // rotated out from under us, or refused by admission: both fine
				}
				mu.Lock()
				tokens = append(tokens, session.Token)
				mu.Unlock()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		reconfigureTo(t, service, "newadmin", "new passphrase")
	}()
	wg.Wait()

	// Every token above was issued to the OLD password. None may survive the rotation.
	mu.Lock()
	defer mu.Unlock()
	for _, token := range tokens {
		if _, _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrNoSession) {
			t.Fatalf("a session issued with the pre-rotation password is still valid: %v", err)
		}
	}
}
