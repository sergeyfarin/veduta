// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"net/http"
)

// Health is one connection's reachability, for GET /api/v1/connections/{id}/test (milestone D5).
// D1 only needs a working, honest implementation to satisfy the Registry interface; D5 is where
// this grows to distinguish DNS/TCP/TLS/auth/HTTP-status failures from each other - this reports
// only whether the request succeeded at all, and the raw error otherwise.
type Health struct {
	Reachable bool
	Error     string
}

// Health implements Registry: a lightweight request against the connection's base URL.
func (r *registry) Health(ctx context.Context, id string) Health {
	conn, ok := r.Get(id)
	if !ok {
		return Health{Error: ErrUnknownConnection.Error()}
	}
	if conn.Kind != KindHTTP {
		return Health{Error: ErrNotHTTP.Error()}
	}
	resp, err := r.Do(ctx, id, Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		return Health{Error: err.Error()}
	}
	return Health{Reachable: resp.StatusCode < http.StatusInternalServerError}
}
