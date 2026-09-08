// SPDX-License-Identifier: AGPL-3.0-or-later

package assetcache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"veduta.dev/veduta/internal/storage"
)

func testCache(t *testing.T, budget int64) *Cache {
	t.Helper()
	s, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	c, err := New(s, budget)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func key(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestHitCoalescesAndCorruptionRefetches(t *testing.T) {
	c := testCache(t, 1024)
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	fetch := func(context.Context) (Result, error) {
		calls.Add(1)
		close(started)
		<-release
		return Result{Body: []byte("image"), ContentType: "image/png"}, nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.GetOrFetch(context.Background(), key("a"), "c", fetch); err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("fetches=%d", calls.Load())
	}
	if err := os.WriteFile(c.path(key("a")), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetOrFetch(context.Background(), key("a"), "c", func(context.Context) (Result, error) {
		calls.Add(1)
		return Result{Body: []byte("image"), ContentType: "image/png"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("corrupt entry was not fetched again; calls=%d", calls.Load())
	}
}

func TestLRUEvictionRespectsBudget(t *testing.T) {
	c := testCache(t, 8)
	for _, id := range []string{"a", "b", "c"} {
		_, err := c.GetOrFetch(context.Background(), key(id), "c", func(context.Context) (Result, error) {
			return Result{Body: []byte("12345"), ContentType: "image/png"}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := c.store.DB().QueryRow(`SELECT count(*) FROM asset_cache`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("entries=%d want 1", count)
	}
}
