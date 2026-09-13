// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import "context"

// RedirectRequest is the destination of a redirect, described in the SAME coordinate system a
// caller's own Request uses: Path is relative to the connection's BaseURL, and Query and Header
// have already had the connection's own credential-bearing entries removed. Without both of
// those normalisations an authorizer would be handed values it cannot compare against the routes
// it holds - see relativeRedirectPath and redirectPolicy.
//
// Every field describes the request that is ABOUT to be sent, never the one that produced the
// redirect. Go rewrites the request across a hop: 301, 302 and 303 turn any method into GET and
// drop both the body and its Content-Type, while 307 and 308 preserve method, body and
// Content-Type exactly. Carrying the original request's values forward would therefore authorise
// a body that no longer exists on three of those five status codes.
type RedirectRequest struct {
	Method string
	Path   string            // relative to the connection's BaseURL, as Request.Path is
	Query  map[string]string // connection-owned keys already removed
	Header map[string]string // connection-owned names already removed
	// BodyLen is the destination body's size in bytes, or -1 when it is unknown (a streaming
	// body with no Content-Length). Unknown is not zero: a length nobody can state cannot be
	// shown to fit under a ceiling, so an authorizer must refuse it rather than treat it as
	// empty.
	BodyLen int64
}

// RedirectAuthorizer re-validates a redirect's destination against whatever policy authorised
// the original request. redirectPolicy's own host/scheme/allowedPaths checks are connection-level
// and know nothing about a specific invocation's manifest/lock route grant - closing that gap
// needs the grant plumbed in from the caller (capabilities.Broker, or the asset proxy), which is
// the one place that has it. Set via WithRedirectAuthorizer before calling Registry.Do;
// redirectPolicy looks it up from the redirected request's own context, which Go's http.Client
// carries unchanged from the original request through every redirect hop (each redirect request
// is built from the original via Request.Clone/WithContext, never a fresh context).
type RedirectAuthorizer func(RedirectRequest) error

type redirectAuthorizerKey struct{}

// WithRedirectAuthorizer attaches fn to ctx so a redirect during this call can be re-checked
// against it. A nil fn is a no-op, same as never calling this.
func WithRedirectAuthorizer(ctx context.Context, fn RedirectAuthorizer) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, redirectAuthorizerKey{}, fn)
}

func redirectAuthorizerFrom(ctx context.Context) RedirectAuthorizer {
	fn, _ := ctx.Value(redirectAuthorizerKey{}).(RedirectAuthorizer)
	return fn
}
