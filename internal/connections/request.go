// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
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
var ErrPathTraversal = errors.New("connections: path is absolute, scheme-bearing, or escapes the connection's base URL")

// joinPath resolves p against base, refusing anything that could leave base's authority: an
// absolute URL or scheme-bearing path (http://, //host, and similar), or a path that traverses
// above base's own path via "..". This is deliberately conservative and self-contained for D1;
// milestone D1b centralises the general-purpose version of this exact concern
// (internal/connections/routepath) and this function is refactored to call into it then - see
// docs/02-implementation-plan.md's D1b entry ("no other package in the tree performs path
// comparison or unescaping").
func joinPath(base *url.URL, p string) (*url.URL, error) {
	if p == "" {
		out := *base
		return &out, nil
	}
	if strings.Contains(p, "://") {
		return nil, ErrPathTraversal
	}
	if strings.HasPrefix(p, "//") {
		return nil, ErrPathTraversal
	}
	// url.Parse would happily accept an absolute path with a scheme via Opaque forms too - reject
	// anything that parses with a non-empty Scheme or Host outright, rather than trusting the
	// prefix checks above alone.
	parsed, err := url.Parse(p)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return nil, ErrPathTraversal
	}

	basePath := base.Path
	if basePath == "" {
		basePath = "/"
	}
	joined := path.Join(basePath, parsed.Path)
	// path.Join cleans "..", but joining "/a" with "../../etc" cleans to "/etc" silently -
	// exactly the traversal this guards against. A joined result that does not stay under
	// basePath (or equal it) escaped.
	if joined != basePath && !strings.HasPrefix(joined, strings.TrimSuffix(basePath, "/")+"/") {
		return nil, ErrPathTraversal
	}
	if parsed.Path != "" && strings.HasSuffix(parsed.Path, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}

	out := *base
	out.Path = joined
	out.RawQuery = parsed.RawQuery
	return &out, nil
}
