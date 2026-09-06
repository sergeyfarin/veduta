// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import "context"

// RedirectAuthorizer re-validates a redirect's destination against whatever policy authorised
// the original request. redirectPolicy's own host/scheme/allowedPaths checks are connection-level
// and know nothing about a specific invocation's manifest/lock route grant - closing that gap
// needs the grant plumbed in from the caller (capabilities.Broker), which is the one place that
// has it. Set via WithRedirectAuthorizer before calling Registry.Do; redirectPolicy looks it up
// from the redirected request's own context, which Go's http.Client carries unchanged from the
// original request through every redirect hop (each redirect request is built from the original
// via Request.Clone/WithContext, never a fresh context).
type RedirectAuthorizer func(method, path string) error

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
