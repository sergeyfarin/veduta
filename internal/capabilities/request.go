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

	// BodyLen states the body's size when its bytes are not in hand - the destination of a
	// redirect, where only the upcoming request's Content-Length is known. Zero means "use
	// len(Body)", which is never wrong for a caller-constructed request because the two agree
	// for an empty body. A NEGATIVE value means the length is unknown, which no ceiling can be
	// shown to accommodate and which bodySizeAllowed therefore always refuses.
	BodyLen int64
}

// bodyLen is the size Authorize judges against a route's ceiling: BodyLen when it was stated,
// len(Body) otherwise.
func (r HTTPRequest) bodyLen() int64 {
	if r.BodyLen != 0 {
		return r.BodyLen
	}
	return int64(len(r.Body))
}

// HTTPResponse is what came back, after the broker's own size cap (via
// internal/connections.Registry, which already enforces this per connection).
type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}
