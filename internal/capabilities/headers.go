// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import "strings"

// pluginHeaderAllowlist is the ONLY headers a plugin-supplied value may keep - allowlisted, not
// denylisted, because the first draft denied a list of known auth headers, which fails open for
// anything not on the list (docs/01-architecture.md's own account of that mistake).
var pluginHeaderAllowlist = map[string]bool{
	"Accept":            true,
	"Accept-Language":   true,
	"Content-Type":      true,
	"If-None-Match":     true,
	"If-Modified-Since": true,
	"Range":             true,
}

// alwaysStrippedHeaders are request-smuggling primitives, never settable by a plugin regardless
// of the allowlist above - docs/01-architecture.md's own list.
var alwaysStrippedHeaders = map[string]bool{
	"Host":              true,
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Connection":        true,
	"Upgrade":           true,
}

// connectionHeaderNames is a connection's own auth header (if auth.type: header) plus every
// name in its static `headers` map - all connection-owned, always stripped.
type connectionOwnership struct {
	headers   map[string]bool // canonical (Title-cased-ish, compared case-insensitively) header names
	queryKeys map[string]bool
}

// newConnectionOwnership builds the set of headers/query keys a connection owns. authType and
// authName are the connection's own auth.type and auth.name (the same field serves as the
// header name for "header" auth and the parameter name for "query" auth - never both at once,
// since a connection has exactly one auth type). bearer/basic need no entry here: both send
// their credential via the "Authorization" header, which is never on pluginHeaderAllowlist in
// the first place, so it is already unconditionally dropped regardless of connection ownership.
func newConnectionOwnership(authType, authName string, staticHeaders map[string]string) connectionOwnership {
	headers := make(map[string]bool, len(staticHeaders)+1)
	for name := range staticHeaders {
		headers[strings.ToLower(name)] = true
	}
	queryKeys := make(map[string]bool, 1)
	switch authType {
	case "header":
		if authName != "" {
			headers[strings.ToLower(authName)] = true
		}
	case "query":
		if authName != "" {
			queryKeys[authName] = true
		}
	}
	return connectionOwnership{headers: headers, queryKeys: queryKeys}
}

// filterHeaders returns a new header map containing only plugin-supplied headers that are on
// the allowlist AND not connection-owned AND not one of the always-stripped names. Nothing here
// mutates the input.
func filterHeaders(in map[string]string, owned connectionOwnership) map[string]string {
	out := make(map[string]string, len(in))
	for name, value := range in {
		lower := strings.ToLower(name)
		if alwaysStrippedHeaders[canonicalHeaderCase(lower)] {
			continue
		}
		if owned.headers[lower] {
			continue
		}
		if !pluginHeaderAllowlist[canonicalHeaderCase(lower)] {
			continue
		}
		out[name] = value
	}
	return out
}

// filterQuery returns a new query map with any connection-owned key discarded - "the same rule
// applies to query parameters, which the draft overlooked" (docs/01-architecture.md section 6).
// This is independent of a route's queryKeys allowlist (Authorize's own job): a plugin cannot
// smuggle a value for the connection's own auth parameter regardless of what a route permits.
func filterQuery(in map[string]string, owned connectionOwnership) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if owned.queryKeys[k] {
			continue
		}
		out[k] = v
	}
	return out
}

// canonicalHeaderCase maps a lowercased header name back to the exact case used in
// pluginHeaderAllowlist/alwaysStrippedHeaders' keys, so both maps can be looked up
// case-insensitively without maintaining two casings of every name by hand.
func canonicalHeaderCase(lower string) string {
	switch lower {
	case "accept":
		return "Accept"
	case "accept-language":
		return "Accept-Language"
	case "content-type":
		return "Content-Type"
	case "if-none-match":
		return "If-None-Match"
	case "if-modified-since":
		return "If-Modified-Since"
	case "range":
		return "Range"
	case "host":
		return "Host"
	case "content-length":
		return "Content-Length"
	case "transfer-encoding":
		return "Transfer-Encoding"
	case "connection":
		return "Connection"
	case "upgrade":
		return "Upgrade"
	default:
		return "" // matches neither map; the header is dropped by the allowlist check regardless
	}
}
