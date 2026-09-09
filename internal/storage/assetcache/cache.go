// SPDX-License-Identifier: AGPL-3.0-or-later

// Package assetcache stores authenticated upstream images on disk with SQLite-backed LRU metadata.
package assetcache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"veduta.dev/veduta/internal/storage"
)

// Result is a cached upstream response safe for browser delivery.
type Result struct {
	Body        []byte
	ContentType string
}

// FetchFunc retrieves an asset after a cache miss.
type FetchFunc func(context.Context) (Result, error)
type flight struct {
	done   chan struct{}
	result Result
	err    error
}

// Cache stores authenticated assets on disk and coalesces concurrent misses.
type Cache struct {
	store   *storage.Store
	dir     string
	budget  int64
	mu      sync.Mutex
	flights map[string]*flight
}

// New creates an asset cache with the supplied byte budget.
func New(store *storage.Store, budget int64) (*Cache, error) {
	if budget <= 0 {
		budget = 512 << 20
	}
	dir := filepath.Join(store.DataDir(), "assets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Cache{store: store, dir: dir, budget: budget, flights: map[string]*flight{}}, nil
}

// GetOrFetch returns a cached asset or invokes fetch once for concurrent callers.
func (c *Cache) GetOrFetch(ctx context.Context, key, connectionID string, fetch FetchFunc) (Result, error) {
	if fetch == nil {
		return Result{}, errors.New("assetcache: nil fetch function")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil || len(decoded) != sha256.Size {
		return Result{}, fmt.Errorf("assetcache: invalid cache key")
	}
	c.mu.Lock()
	if existing := c.flights[key]; existing != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-existing.done:
			return clone(existing.result), existing.err
		}
	}
	f := &flight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	needsPut := false
	if cached, ok, cacheErr := c.get(ctx, key); cacheErr != nil {
		f.err = cacheErr
	} else if ok {
		f.result = cached
	} else {
		needsPut = true
		f.result, f.err = fetch(ctx)
	}
	if f.err == nil && needsPut {
		f.err = c.put(ctx, key, connectionID, f.result)
	}
	c.mu.Lock()
	delete(c.flights, key)
	close(f.done)
	c.mu.Unlock()
	return clone(f.result), f.err
}

func (c *Cache) get(ctx context.Context, key string) (Result, bool, error) {
	var contentType string
	var size int64
	err := c.store.DB().QueryRowContext(ctx, `SELECT content_type,bytes FROM asset_cache WHERE hash=?`, key).Scan(&contentType, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	data, err := os.ReadFile(c.path(key))
	if errors.Is(err, os.ErrNotExist) {
		_ = c.remove(ctx, key)
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if int64(len(data)) != size+sha256.Size {
		_ = c.remove(ctx, key)
		return Result{}, false, nil
	}
	want := data[:sha256.Size]
	got := sha256.Sum256(data[sha256.Size:])
	if !equal(want, got[:]) {
		_ = c.remove(ctx, key)
		return Result{}, false, nil
	}
	_, _ = c.store.DB().ExecContext(ctx, `UPDATE asset_cache SET last_access_at=? WHERE hash=?`, now(), key)
	return Result{Body: append([]byte(nil), data[sha256.Size:]...), ContentType: contentType}, true, nil
}

func (c *Cache) put(ctx context.Context, key, connectionID string, result Result) error {
	if int64(len(result.Body)) > c.budget {
		return nil
	}
	digest := sha256.Sum256(result.Body)
	data := append(digest[:], result.Body...)
	tmp, err := os.CreateTemp(c.dir, ".asset-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, c.path(key)); err != nil {
		return err
	}
	stamp := now()
	_, err = c.store.DB().ExecContext(ctx, `INSERT INTO asset_cache(hash,connection_id,content_type,bytes,created_at,last_access_at) VALUES(?,?,?,?,?,?) ON CONFLICT(hash) DO UPDATE SET connection_id=excluded.connection_id,content_type=excluded.content_type,bytes=excluded.bytes,last_access_at=excluded.last_access_at`, key, connectionID, result.ContentType, len(result.Body), stamp, stamp)
	if err != nil {
		return err
	}
	return c.evict(ctx)
}

func (c *Cache) evict(ctx context.Context) error {
	for {
		var total int64
		if err := c.store.DB().QueryRowContext(ctx, `SELECT coalesce(sum(bytes),0) FROM asset_cache`).Scan(&total); err != nil {
			return err
		}
		if total <= c.budget {
			return nil
		}
		var key string
		if err := c.store.DB().QueryRowContext(ctx, `SELECT hash FROM asset_cache ORDER BY last_access_at,hash LIMIT 1`).Scan(&key); err != nil {
			return err
		}
		if err := c.remove(ctx, key); err != nil {
			return err
		}
	}
}

func (c *Cache) remove(ctx context.Context, key string) error {
	_ = os.Remove(c.path(key))
	_, err := c.store.DB().ExecContext(ctx, `DELETE FROM asset_cache WHERE hash=?`, key)
	return err
}

func (c *Cache) path(key string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil || len(decoded) != sha256.Size {
		panic(fmt.Sprintf("assetcache: invalid internal key %q", key))
	}
	return filepath.Join(c.dir, key)
}
func clone(r Result) Result { r.Body = append([]byte(nil), r.Body...); return r }
func now() string           { return time.Now().UTC().Format(time.RFC3339Nano) }
func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
