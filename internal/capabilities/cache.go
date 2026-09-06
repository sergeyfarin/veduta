// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import (
	"sync"
	"time"
)

// Cache is the broker's cache backend. cacheStore is an in-memory, process-lifetime
// implementation - real persistence (SQLite's plugin_kv table, docs/01-architecture.md section
// 10) is a later milestone's job once storage exists at all; this package only needs the
// namespacing and budget behaviour to be real and testable, which does not require persistence.
type Cache interface {
	Get(namespace, key string) ([]byte, bool)
	Put(namespace, key string, val []byte, ttl time.Duration)
}

// cacheNamespace is "plugin:{id}:{manifest digest}:{instance}:" - docs/01-architecture.md
// section 6: "Cache keys are namespaced ... the digest is in the key so an upgraded plugin
// starts with a cold cache rather than consuming entries written by the version it replaced.
// Plugins cannot read each other's state."
func cacheNamespace(g Grant) string {
	return "plugin:" + g.PluginID + ":" + g.Ident.ManifestDigest + ":" + g.InstanceID
}

type cacheItem struct {
	value     []byte
	expiresAt time.Time
}

// memCache is a plain in-memory Cache: safe for concurrent use, entries pruned lazily on Get.
type memCache struct {
	mu    sync.Mutex
	items map[string]cacheItem
}

// NewMemCache builds an empty in-memory Cache.
func NewMemCache() Cache { return &memCache{items: make(map[string]cacheItem)} }

// Get implements Cache.
func (c *memCache) Get(namespace, key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	full := namespace + ":" + key
	item, ok := c.items[full]
	if !ok {
		return nil, false
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		delete(c.items, full)
		return nil, false
	}
	return item.value, true
}

// Put implements Cache.
func (c *memCache) Put(namespace, key string, val []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	c.items[namespace+":"+key] = cacheItem{value: val, expiresAt: expires}
}
