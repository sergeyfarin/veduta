// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"veduta.dev/veduta/internal/connections"
)

// Broker is the frozen interface every host-function implementation goes through -
// docs/01-architecture.md section 6. It is the only place a plugin's request becomes a real
// upstream call, a cache read/write, an asset reference, a log line or an event.
type Broker interface {
	HTTP(ctx context.Context, g Grant, req HTTPRequest) (HTTPResponse, error)
	CacheGet(ctx context.Context, g Grant, key string) ([]byte, bool, error)
	CachePut(ctx context.Context, g Grant, key string, val []byte, ttl time.Duration) error
	AssetRef(ctx context.Context, g Grant, slot, path string, query url.Values, t Transform) (string, error)
	Log(g Grant, level, msg string, fields map[string]any)
	Emit(ctx context.Context, g Grant, e Event) error
}

type broker struct {
	registry   connections.Registry
	cache      Cache
	audit      Audit
	logger     *slog.Logger
	signingKey [32]byte // ephemeral, process-lifetime only - see AssetRef's own doc comment
}

// NewBroker builds a Broker. The asset-ref signing key is generated fresh on every process
// start, in memory only: a persisted, instance-keyed one (so tokens survive a restart) is
// milestone E1's job, once settings/connection_state storage exists at all
// (docs/01-architecture.md section 7). Until then, every restart invalidates outstanding asset
// refs - acceptable for D2's own scope, which is authorisation, not the asset proxy's lifecycle.
func NewBroker(registry connections.Registry, cache Cache, audit Audit, logger *slog.Logger) Broker {
	if logger == nil {
		logger = slog.Default()
	}
	b := &broker{registry: registry, cache: cache, audit: audit, logger: logger}
	if _, err := rand.Read(b.signingKey[:]); err != nil {
		panic("capabilities: could not generate a signing key: " + err.Error()) // no crypto/rand means nothing here can be trusted
	}
	return b
}

// authorize runs check, recording a denial (with method for the audit trail) if it fails - the
// one place every broker method's preamble reports to Audit, so no method can forget to.
func (b *broker) authorize(g Grant, method string, check func() error) error {
	if err := check(); err != nil {
		b.audit.Denied(g.PluginID, method, err)
		return err
	}
	return nil
}

// HTTP implements Broker. Preamble: capability, slot, route authorisation (against UseData),
// budgets (hostCalls, httpRequests) - all four checks docs/01-architecture.md section 6 names,
// exactly in that order, each returning immediately on the first failure.
func (b *broker) HTTP(ctx context.Context, g Grant, req HTTPRequest) (HTTPResponse, error) {
	var connID string
	if err := b.authorize(g, "HTTP", func() error {
		if !g.Caps.Has("http") {
			return ErrCapDenied
		}
		id, ok := g.Slots[req.Slot]
		if !ok {
			return ErrSlotDenied
		}
		connID = id
		if err := g.Authorize(req, UseData); err != nil {
			return err
		}
		if err := g.consumeHostCall(); err != nil {
			return err
		}
		return g.consumeHTTPRequest()
	}); err != nil {
		return HTTPResponse{}, err
	}

	conn, ok := b.registry.Get(connID)
	if !ok || conn.HTTP == nil {
		return HTTPResponse{}, fmt.Errorf("capabilities: connection %q is not an http connection", connID)
	}
	owned := newConnectionOwnership(string(conn.HTTP.Auth.Type), conn.HTTP.Auth.Name, conn.HTTP.Headers)

	resp, err := b.registry.Do(ctx, connID, connections.Request{
		Method:  req.Method,
		Path:    req.Path,
		Query:   filterQuery(req.Query, owned),
		Headers: filterHeaders(req.Header, owned),
		Body:    req.Body,
	})
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, nil
}

// CacheGet implements Broker. Cache has no slot or route - it is namespaced per plugin instance
// (cacheNamespace), not per connection - so the preamble here is capability plus budget only.
func (b *broker) CacheGet(ctx context.Context, g Grant, key string) ([]byte, bool, error) {
	if err := b.authorize(g, "CacheGet", func() error {
		if !g.Caps.Has("cache") {
			return ErrCapDenied
		}
		return g.consumeHostCall()
	}); err != nil {
		return nil, false, err
	}
	val, ok := b.cache.Get(cacheNamespace(g), key)
	return val, ok, nil
}

// CachePut implements Broker.
func (b *broker) CachePut(ctx context.Context, g Grant, key string, val []byte, ttl time.Duration) error {
	if err := b.authorize(g, "CachePut", func() error {
		if !g.Caps.Has("cache") {
			return ErrCapDenied
		}
		if err := g.consumeHostCall(); err != nil {
			return err
		}
		return g.consumeCacheWrite(len(val))
	}); err != nil {
		return err
	}
	b.cache.Put(cacheNamespace(g), key, val, ttl)
	return nil
}

// Log implements Broker. No slot or route either - logging is not connection-scoped.
func (b *broker) Log(g Grant, level, msg string, fields map[string]any) {
	if err := b.authorize(g, "Log", func() error {
		if !g.Caps.Has("log") {
			return ErrCapDenied
		}
		return g.consumeHostCall()
	}); err != nil {
		return
	}
	args := make([]any, 0, len(fields)*2+2)
	args = append(args, "plugin", g.PluginID)
	for k, v := range fields {
		args = append(args, k, v)
	}
	logLevel := slog.LevelInfo
	switch level {
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	case "debug":
		logLevel = slog.LevelDebug
	}
	b.logger.Log(context.Background(), logLevel, msg, args...)
}

// Emit implements Broker. Rules/notifications (Phase J) are the real consumer; D2 only needs
// Emit authorised and budgeted, so this logs the event for now rather than inventing the outbox
// J will define.
func (b *broker) Emit(ctx context.Context, g Grant, e Event) error {
	if err := b.authorize(g, "Emit", func() error {
		if !g.Caps.Has("events") {
			return ErrCapDenied
		}
		return g.consumeHostCall()
	}); err != nil {
		return err
	}
	b.logger.Info("event emitted", "plugin", g.PluginID, "type", e.Type)
	return nil
}
