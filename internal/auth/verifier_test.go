// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestVerifierRefusesBeyondItsQueueWithoutRunning is the core admission property: past the
// bound, verify returns ErrBusy having never called fn - so the memory the hash would have
// allocated is never allocated.
func TestVerifierRefusesBeyondItsQueueWithoutRunning(t *testing.T) {
	v := newVerifier(1, 0)
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = v.verify(func() bool { <-release; return true })
	}()

	// Wait for the one slot to be occupied before crowding it.
	deadline := time.Now().Add(2 * time.Second)
	for v.admitted.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	ran := false
	_, err := v.verify(func() bool { ran = true; return true })
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy once the pool and queue are full", err)
	}
	if ran {
		t.Fatal("the refused verification must not have run")
	}
	close(release)
	wg.Wait()
}

// TestLockedOutLoginCostsNoHash is the finding itself: the previous order ran a full Argon2
// verification and only then returned ErrRateLimited, so lockout bounded nothing that mattered.
func TestLockedOutLoginCostsNoHash(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	for i := 0; i < 5; i++ {
		if _, err := service.Login(context.Background(), "admin", "wrong", "test", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	before := service.verifier.admitted.Load()
	if _, err := service.Login(context.Background(), "admin", "wrong", "test", "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if after := service.verifier.admitted.Load(); after != before {
		t.Fatalf("a locked-out login ran %d verification(s); it must run none", after-before)
	}
}

// TestSudoIsThrottled covers the surface that had no lockout, no attempt record and no admission
// control at all before this change.
func TestSudoIsThrottled(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	session, err := service.Login(context.Background(), "admin", "correct horse", "test", "192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := service.OpenSudo(context.Background(), session.Token, "wrong", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	before := service.verifier.admitted.Load()
	if err := service.OpenSudo(context.Background(), session.Token, "wrong", "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited after repeated sudo failures, got %v", err)
	}
	if after := service.verifier.admitted.Load(); after != before {
		t.Fatalf("a throttled sudo ran %d verification(s); it must run none", after-before)
	}
}

// TestAttemptsMapIsBounded: the keys carry a caller-supplied username, so a burst of distinct
// ones must not grow the map without limit.
func TestAttemptsMapIsBounded(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, _ := testService(t, &now)
	for i := 0; i < maxTrackedSources+500; i++ {
		service.recordFailure(fmt.Sprintf("user%d\x00192.0.2.1", i), now)
	}
	service.mu.Lock()
	size := len(service.attempts)
	service.mu.Unlock()
	if size > maxTrackedSources {
		t.Fatalf("attempts map holds %d entries, want at most %d", size, maxTrackedSources)
	}
}
