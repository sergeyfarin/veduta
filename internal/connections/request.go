// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"veduta.dev/veduta/internal/connections/routepath"
)

// Request is what a caller (builtin runtime, declarative runtime, WASM host, asset proxy) asks a
// Connection to do. Every caller goes through the same Registry.Do, so rate limiting and the
// circuit breaker apply identically regardless of who is asking (docs/01-architecture.md
// section 3).
type Request struct {
	Method  string
	Path    string // relative to the connection's BaseURL - see joinPath
	Query   map[string]string
	Headers map[string]string
	Body    []byte
}

// Response is what came back. Body is already read and size-capped - see doRequest.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// ErrPathTraversal is returned for a Path this package refuses to join onto a BaseURL.
var ErrPathTraversal = errors.New("connections: path is absolute, scheme-bearing, or unsafe")

// joinPath resolves p against base. Two independent things make this safe: p must not be an
// absolute URL or scheme-bearing path (http://, //host - a request "path" that is secretly a
// full URL to somewhere else), and once that is ruled out, p's actual path component is run
// through routepath.Canonicalise - milestone D1b's one shared route-canonicalisation routine,
// used here instead of an ad-hoc check (docs/02-implementation-plan.md's D1b AC: "no other
// package in the tree performs path comparison or unescaping"). Canonicalise rejects any ".."
// or encoded separator on its own, in isolation, for both base's path and p - so the two
// canonical strings can simply be concatenated afterwards with no further cleaning step and no
// way for the result to escape base: there is nothing left in either operand that a join could
// clean away a traversal from, unlike path.Join, which silently resolves "/a" + "../../etc" to
// "/etc" with no error of its own.
func joinPath(base *url.URL, p string) (*url.URL, error) {
	if p == "" {
		out := *base
		return &out, nil
	}
	if strings.Contains(p, "://") || strings.HasPrefix(p, "//") {
		return nil, ErrPathTraversal
	}
	parsed, err := url.Parse(p)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return nil, ErrPathTraversal
	}

	reqPath := parsed.Path
	if reqPath == "" {
		reqPath = "/"
	}
	canonicalReq, err := routepath.Canonicalise(reqPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPathTraversal, err)
	}

	basePath := base.Path
	if basePath == "" {
		basePath = "/"
	}
	canonicalBase, err := routepath.Canonicalise(basePath)
	if err != nil {
		// The connection's own configured base path is not canonical - a config problem, not
		// something this specific request did.
		return nil, fmt.Errorf("connections: connection's own base path %q is not canonical: %w", basePath, err)
	}

	joined := canonicalBase
	if canonicalReq != "/" {
		joined = strings.TrimSuffix(canonicalBase, "/") + canonicalReq
	}

	out := *base
	out.Path = joined
	out.RawQuery = parsed.RawQuery
	return &out, nil
}
