// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	assettokens "veduta.dev/veduta/internal/capabilities/assets"
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
	Log(g Grant, level, msg string, fields map[string]any) error
	Emit(ctx context.Context, g Grant, e Event) error
}

type broker struct {
	registry         connections.Registry
	cache            Cache
	audit            Audit
	logger           *slog.Logger
	assets           *assettokens.Service
	events           func(context.Context, string, Event) error
	fallbackRevision [16]byte
}

// NewBrokerWithAssetsAndEvents also persists authorised plugin events through sink.
func NewBrokerWithAssetsAndEvents(registry connections.Registry, cache Cache, audit Audit, logger *slog.Logger, service *assettokens.Service, sink func(context.Context, string, Event) error) Broker {
	b := NewBrokerWithAssets(registry, cache, audit, logger, service).(*broker)
	b.events = sink
	return b
}

// NewBroker builds a standalone Broker with an ephemeral asset authority for tests and isolated
// runtimes. Production uses NewBrokerWithAssets with the key persisted by storage.
func NewBroker(registry connections.Registry, cache Cache, audit Audit, logger *slog.Logger) Broker {
	if logger == nil {
		logger = slog.Default()
	}
	b := &broker{registry: registry, cache: cache, audit: audit, logger: logger, assets: assettokens.NewEphemeral()}
	if _, err := rand.Read(b.fallbackRevision[:]); err != nil {
		panic("capabilities: could not generate a signing key: " + err.Error()) // no crypto/rand means nothing here can be trusted
	}
	return b
}

// NewBrokerWithAssets uses the persisted asset-token authority owned by the application.
func NewBrokerWithAssets(registry connections.Registry, cache Cache, audit Audit, logger *slog.Logger, service *assettokens.Service) Broker {
	b := NewBroker(registry, cache, audit, logger).(*broker)
	if service != nil {
		b.assets = service
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

	// A redirect the connection follows is re-checked against this same Grant, not just
	// host/scheme/allowedPaths (connections.redirectPolicy's own, connection-level checks) -
	// found in review: only the broker holds the Grant, so closing this gap needs the Grant
	// threaded down through ctx to where the redirect is actually decided.
	slot := req.Slot
	ctx = connections.WithRedirectAuthorizer(ctx, func(method, path string) error {
		return g.AuthorizesRedirect(slot, method, path)
	})
	// g.Limits.ResponseMB is the manifest's own per-response ceiling, narrower than (never wider
	// than) the connection's own MaxResponseBytes. Found in review, in two parts: (1) this field
	// was carried on Limits and documented as enforced here, but nothing ever actually read it;
	// (2) the first fix for that read it only as a POST-HOC check, after Do had already read up
	// to the connection's own wider limit - so a manifest approved for a small ResponseMB still
	// let a misbehaving upstream's response be fully buffered before being rejected. Passing it
	// as Request.MaxResponseBytes makes registry.Do itself stop reading at
	// min(connection limit, grant limit), so an oversized body is never fully buffered in the
	// first place, not read then discarded.
	var maxResponseBytes int64
	if g.Limits.ResponseMB > 0 {
		maxResponseBytes = int64(g.Limits.ResponseMB) << 20
	}
	resp, err := b.registry.Do(ctx, connID, connections.Request{
		Method:           req.Method,
		Path:             req.Path,
		Query:            filterQuery(req.Query, owned),
		Headers:          filterHeaders(req.Header, owned),
		Body:             req.Body,
		MaxResponseBytes: maxResponseBytes,
	})
	if err != nil {
		if errors.Is(err, connections.ErrResponseTooLarge) {
			b.audit.Denied(g.PluginID, "HTTP", ErrBudgetExceeded)
			return HTTPResponse{}, fmt.Errorf("%w: %w", ErrBudgetExceeded, err)
		}
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
func (b *broker) Log(g Grant, level, msg string, fields map[string]any) error {
	if err := b.authorize(g, "Log", func() error {
		if !g.Caps.Has("log") {
			return ErrCapDenied
		}
		return g.consumeHostCall()
	}); err != nil {
		return err
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
	return nil
}

// Emit implements Broker. Production supplies the persistent event sink; isolated runtimes keep
// the structured log fallback.
func (b *broker) Emit(ctx context.Context, g Grant, e Event) error {
	if err := b.authorize(g, "Emit", func() error {
		if !g.Caps.Has("events") {
			return ErrCapDenied
		}
		return g.consumeHostCall()
	}); err != nil {
		return err
	}
	if e.Type == "" {
		return errors.New("capabilities: event type is required")
	}
	if b.events != nil {
		return b.events(ctx, g.PluginID, e)
	}
	b.logger.Info("event emitted", "plugin", g.PluginID, "type", e.Type)
	return nil
}
