// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Registry is the frozen interface from docs/01-architecture.md section 3: every caller -
// builtin, declarative, WASM, asset proxy - goes through Do, so rate limiting and the circuit
// breaker apply identically no matter who is asking.
type Registry interface {
	Get(id string) (*Connection, bool)
	Do(ctx context.Context, id string, req Request) (*Response, error)
	Health(ctx context.Context, id string) Health
}

type registry struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	clients     map[string]*client // http-kind connections only
}

// ErrUnknownConnection is returned by Do and Health for an id the registry has no Connection for.
var ErrUnknownConnection = errors.New("connections: unknown connection id")

// ErrNotHTTP is returned by Do for a Connection whose Kind is not http - the only kind Do knows
// how to speak to; Docker connections are a different transport entirely.
var ErrNotHTTP = errors.New("connections: not an http connection")

// Get implements Registry.
func (r *registry) Get(id string) (*Connection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connections[id]
	return c, ok
}

// Do runs one request against connection id: acquires the rate limiter and concurrency
// semaphore, joins Path onto the connection's BaseURL (refusing traversal - see joinPath),
// injects the connection's own auth (never something the caller can override by naming), and
// reads the response body up to MaxResponseBytes - anything larger is an error, not a silent
// truncation.
func (r *registry) Do(ctx context.Context, id string, req Request) (*Response, error) {
	r.mu.RLock()
	c, ok := r.clients[id]
	r.mu.RUnlock()
	if !ok {
		if _, exists := r.Get(id); exists {
			return nil, ErrNotHTTP
		}
		return nil, ErrUnknownConnection
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("connections: rate limit: %w", err)
	}
	if err := c.sem.acquire(ctx); err != nil {
		return nil, fmt.Errorf("connections: concurrency limit: %w", err)
	}
	defer c.sem.release()

	target, err := joinPath(c.base, req.Path)
	if err != nil {
		return nil, err
	}

	query := target.Query()
	for k, v := range req.Query {
		query.Set(k, v)
	}

	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, target.String(), body)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	// The connection's own static headers (connections.*.headers in config) always win over
	// anything a caller supplied - the broker already treats their names as connection-owned and
	// strips a plugin-supplied value for them (see capabilities/headers.go's ownership model),
	// but Do itself is the actual authority: not every caller goes through the broker (Health,
	// for one), so this cannot assume that filtering already happened. Found in review: these
	// were computed into HTTPConfig.Headers at build time and never actually added to a request -
	// a configured static header silently never went out on the wire at all.
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	injectAuth(httpReq, query, c.cfg.Auth)
	httpReq.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	limit := c.cfg.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	data, err := readLimited(resp.Body, limit)
	if err != nil {
		return nil, err
	}

	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

const defaultMaxResponseBytes = 8 << 20 // 8 MiB, matching docs/01-architecture.md section 3

// ErrResponseTooLarge is returned by Do when a response exceeds the connection's
// MaxResponseBytes. The body is truncated (never buffered past the limit), not returned partial.
var ErrResponseTooLarge = errors.New("connections: response exceeded the connection's maximum size")

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}
