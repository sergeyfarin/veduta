// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import "net/http"

// HTTPRequest is what a plugin asks the broker for - never a URL, only a slot name and a
// relative path, per docs/01-architecture.md section 3's frozen boundary.
type HTTPRequest struct {
	Slot   string
	Method string
	Path   string
	Query  map[string]string
	Header map[string]string // allowlisted, and connection-owned names dropped - see headers.go
	Body   []byte
}

// HTTPResponse is what came back, after the broker's own size cap (via
// internal/connections.Registry, which already enforces this per connection).
type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}
