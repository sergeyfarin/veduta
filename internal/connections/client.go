// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"veduta.dev/veduta/internal/connections/routepath"
)

const defaultTimeout = 10 * time.Second

// client is one connection's everything: its own *http.Client (pooled, with its own TLS config
// and IP-pinning dialer), rate limiter and concurrency semaphore. One per Connection, built once
// at registry construction - never per request.
type client struct {
	id         string
	cfg        *HTTPConfig
	base       *url.URL
	httpClient *http.Client
	limiter    *rate.Limiter
	sem        semaphore
}

func newHTTPClient(id string, cfg *HTTPConfig, logger *slog.Logger) (*client, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("connection %q: invalid baseUrl: %w", id, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("connection %q: baseUrl must be http or https", id)
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, //nolint:gosec // explicit opt-in, warned below
		ServerName:         cfg.TLS.ServerName,
	}
	if cfg.TLS.InsecureSkipVerify {
		logger.Warn("connection has TLS certificate verification disabled",
			"connection", id, "hint", "tls.insecureSkipVerify should only ever be set deliberately")
	}
	if cfg.TLS.CAFile != "" {
		pemBytes, err := os.ReadFile(cfg.TLS.CAFile) //nolint:gosec // operator-configured path, not request input
		if err != nil {
			return nil, fmt.Errorf("connection %q: reading tls.caFile: %w", id, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("connection %q: tls.caFile contains no usable certificates", id)
		}
		tlsConfig.RootCAs = pool
	}

	dialer := newPinnedDialer()
	transport := &http.Transport{
		DialContext:     dialer.DialContext,
		TLSClientConfig: tlsConfig,
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	httpClient := &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: redirectPolicy(base, cfg.MaxRedirects, cfg.AllowedPaths, cfg.Auth, cfg.Headers),
	}

	return &client{
		id:         id,
		cfg:        cfg,
		base:       base,
		httpClient: httpClient,
		limiter:    newLimiter(cfg.RateLimit),
		sem:        newSemaphore(concurrencyOrDefault(cfg.Concurrency)),
	}, nil
}

func concurrencyOrDefault(n int) int {
	if n < 1 {
		return 4
	}
	return n
}

// redirectPolicy refuses a redirect to a different host or a weaker scheme outright, any
// redirect beyond maxRedirects (default 0 - docs/01-architecture.md section 8), a redirect to a
// path outside the connection's own allowedPaths when one is configured, a redirect whose
// destination escapes the connection's own base path, and - the fix for the gap this function's
// doc comment used to describe as open - re-runs the caller's manifest/lock route grant against
// the redirect target via whatever RedirectAuthorizer the request's context carries (see
// redirectauth.go).
//
// Same-host was, before the allowedPaths fix, believed sufficient: the broker authorises the
// *original* request's path against the manifest, the lock and allowedPaths, but a followed
// redirect never re-ran any of those three checks against the *new* path - an authorised
// `/api/public` could redirect to `/api/admin` on the identical host and the credentialed request
// would simply follow it. allowedPaths closed part of that (any connection that has one
// configured); the manifest/lock recheck below closes the rest, for every connection regardless
// of whether allowedPaths is set.
//
// What reaches the authorizer is the destination request as it will actually be sent, normalised
// into the caller's own coordinate system: the path made relative to base (a route says
// `/items`, not the `/api/items` a base URL of .../api produces), and the connection's own
// credential-bearing query key and header names removed. That last step matters in the
// permissive direction rather than the restrictive one: an upstream that echoes the connection's
// api_key parameter back in its Location header would otherwise be denied by a route whose
// queryKeys legitimately never mentions a parameter the plugin cannot set in the first place.
func redirectPolicy(base *url.URL, maxRedirects int, allowedPaths []string, auth Auth, staticHeaders map[string]string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return fmt.Errorf("connections: exceeded the %d allowed redirect(s)", maxRedirects)
		}
		if req.URL.Host != base.Host {
			return fmt.Errorf("connections: redirect to a different host %q refused", req.URL.Host)
		}
		if req.URL.Scheme != base.Scheme {
			return fmt.Errorf("connections: redirect changes scheme %q -> %q, refused", base.Scheme, req.URL.Scheme)
		}
		canonicalPath, err := routepath.Canonicalise(req.URL.Path)
		if err != nil {
			return fmt.Errorf("connections: redirect target path %q: %w", req.URL.Path, err)
		}
		if len(allowedPaths) > 0 {
			allowed := false
			for _, p := range allowedPaths {
				canonicalAllowed, err := routepath.Canonicalise(p)
				if err != nil {
					continue
				}
				if routepath.HasPathPrefix(canonicalPath, canonicalAllowed) {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("connections: redirect to %q is outside the connection's allowedPaths", req.URL.Path)
			}
		}
		if authorize := redirectAuthorizerFrom(req.Context()); authorize != nil {
			relative, err := relativeRedirectPath(base, canonicalPath)
			if err != nil {
				return err
			}
			owned := ownedNames(auth, staticHeaders)
			if err := authorize(RedirectRequest{
				Method:  req.Method,
				Path:    relative,
				Query:   firstQueryValues(req.URL.Query(), owned.query),
				Header:  singleHeaderValues(req.Header, owned.header),
				BodyLen: redirectBodyLen(req),
			}); err != nil {
				return fmt.Errorf("connections: redirect to %s %q refused: %w", req.Method, relative, err)
			}
		}
		return nil
	}
}

// relativeRedirectPath maps an absolute destination path back into the coordinate system a
// caller's Request.Path uses, which is joinPath's inverse. A connection whose BaseURL carries a
// path - https://host/api - serves a plugin route of `/items` at `/api/items`, so handing the
// authorizer the absolute `/api/items` would compare it against routes written as `/items` and
// deny every legitimate redirect. A destination outside the base path is not expressible as a
// caller path at all, and is refused here rather than silently reshaped into one.
func relativeRedirectPath(base *url.URL, canonicalPath string) (string, error) {
	basePath := base.Path
	if basePath == "" {
		basePath = "/"
	}
	canonicalBase, err := routepath.Canonicalise(basePath)
	if err != nil {
		return "", fmt.Errorf("connections: connection's own base path %q is not canonical: %w", basePath, err)
	}
	if canonicalBase == "/" {
		return canonicalPath, nil
	}
	if !routepath.HasPathPrefix(canonicalPath, canonicalBase) {
		return "", fmt.Errorf("connections: redirect to %q escapes the connection's base path %q", canonicalPath, canonicalBase)
	}
	relative := strings.TrimPrefix(canonicalPath, canonicalBase)
	if relative == "" {
		relative = "/"
	}
	return relative, nil
}

// redirectBodyLen reports the destination body's size, or -1 when it cannot be stated. Go sets
// ContentLength to 0 on the hops that drop the body (301, 302, 303) and to the original length
// on those that preserve it (307, 308); a negative value means "unknown", which is passed
// through as unknown rather than flattened to zero.
func redirectBodyLen(req *http.Request) int64 {
	if req.ContentLength < 0 {
		return -1
	}
	return req.ContentLength
}

// ownedNameSet carries those two sets.
type ownedNameSet struct {
	query  map[string]bool
	header map[string]bool
}

// ownedNames is the set of query keys and header names the connection itself owns, and which a
// caller therefore never supplies and must never be judged on. It mirrors the equivalent set
// capabilities.newConnectionOwnership builds for the initial request.
func ownedNames(auth Auth, staticHeaders map[string]string) ownedNameSet {
	out := ownedNameSet{query: map[string]bool{}, header: map[string]bool{}}
	for name := range staticHeaders {
		out.header[strings.ToLower(name)] = true
	}
	switch auth.Type {
	case AuthHeader:
		if auth.Name != "" {
			out.header[strings.ToLower(auth.Name)] = true
		}
	case AuthQuery:
		if auth.Name != "" {
			out.query[auth.Name] = true
		}
	case AuthBearer, AuthBasic:
		out.header["authorization"] = true
	}
	return out
}

func firstQueryValues(q url.Values, owned map[string]bool) map[string]string {
	out := make(map[string]string, len(q))
	for k, v := range q {
		if owned[k] || len(v) == 0 {
			continue
		}
		out[k] = v[0]
	}
	return out
}

func singleHeaderValues(h http.Header, owned map[string]bool) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if owned[strings.ToLower(k)] || len(v) == 0 {
			continue
		}
		out[k] = v[0]
	}
	return out
}
