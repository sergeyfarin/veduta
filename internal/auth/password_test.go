// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/storage"
)

// The property that matters: whatever HashPassword emits, the login path accepts. Anything less
// than a real Service.Login here would let the two halves drift and only fail on the operator's
// server, at start-up, with the password already committed to a secret store.
func TestHashPasswordProducesAHashLoginAccepts(t *testing.T) {
	const password = "correct horse battery staple"
	encoded, err := HashPassword(password, DefaultHashCost)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	service, err := New(context.Background(), Config{
		Store: store, Username: "admin", PasswordHash: encoded, SessionTTL: "1h",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), "admin", password, "test", "127.0.0.1"); err != nil {
		t.Fatalf("login with a generated hash: %v", err)
	}
	if _, err := service.Login(context.Background(), "admin", password+"!", "test", "127.0.0.1"); err == nil {
		t.Fatal("login accepted the wrong password")
	}
}

func TestHashPasswordSaltsEveryHash(t *testing.T) {
	first, err := HashPassword("same password", DefaultHashCost)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("same password", DefaultHashCost)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two hashes of one password are identical: the salt is not random")
	}
}

func TestHashPasswordEncodesTheCostItWasGiven(t *testing.T) {
	encoded, err := HashPassword("pw", HashCost{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("cost not reflected in %q", encoded)
	}
	params, err := parseArgon2ID(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if params.memory != 8*1024 || params.iterations != 1 || params.parallelism != 1 {
		t.Fatalf("parsed cost %+v", params)
	}
}

// Generation refuses exactly what verification refuses, because both call checkCost. Each case
// here is a hash that would have been written into a config file and then rejected at start-up.
func TestHashPasswordRefusesWhatVerificationWouldRefuse(t *testing.T) {
	cases := map[string]HashCost{
		"memory below the floor":   {MemoryKiB: 8*1024 - 1, Iterations: 3, Parallelism: 4},
		"memory above the ceiling": {MemoryKiB: 256*1024 + 1, Iterations: 3, Parallelism: 4},
		"no iterations":            {MemoryKiB: 64 * 1024, Iterations: 0, Parallelism: 4},
		"too many iterations":      {MemoryKiB: 64 * 1024, Iterations: 11, Parallelism: 4},
		"no lanes":                 {MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 0},
		"too many lanes":           {MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 17},
	}
	for name, cost := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := HashPassword("pw", cost); err == nil {
				t.Fatal("accepted a cost the verifier rejects")
			}
		})
	}
}

func TestHashPasswordRefusesAnEmptyPassword(t *testing.T) {
	if _, err := HashPassword("", DefaultHashCost); err == nil {
		t.Fatal("hashed an empty password")
	}
}

func TestDefaultHashCostIsWithinTheSafeRange(t *testing.T) {
	if err := checkCost(DefaultHashCost.MemoryKiB, DefaultHashCost.Iterations, DefaultHashCost.Parallelism); err != nil {
		t.Fatalf("the default cost is one the verifier rejects: %v", err)
	}
}
