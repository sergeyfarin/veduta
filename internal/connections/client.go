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
		CheckRedirect: redirectPolicy(base, cfg.MaxRedirects, cfg.AllowedPaths),
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
// redirect beyond maxRedirects (default 0 - docs/01-architecture.md section 8), and - the fix for
// a real gap found in review - a redirect to a path outside the connection's own allowedPaths
// when one is configured.
//
// Same-host was, before this fix, believed sufficient: the broker authorises the *original*
// request's path against the manifest, the lock and allowedPaths, but a followed redirect never
// re-runs any of those three checks against the *new* path - an authorised `/api/public` could
// redirect to `/api/admin` on the identical host and the credentialed request would simply follow
// it. Checking allowedPaths here closes that for any connection that has one configured, which
// docs/01's own "http-json escape hatch" language already calls "the only thing standing between
// a card and every path on that connection" - it is not yet a fix for a connection with no
// allowedPaths configured (nil/empty there means "any path is fine" by design), where a redirect
// could still reach a path the manifest/lock did not approve; that residual gap needs the broker
// itself to re-run Grant.Authorize against the redirect target, which needs the Grant plumbed
// into this policy and is tracked in docs/03-backlog.md rather than attempted here.
func redirectPolicy(base *url.URL, maxRedirects int, allowedPaths []string) func(req *http.Request, via []*http.Request) error {
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
		if len(allowedPaths) == 0 {
			return nil
		}
		canonicalPath, err := routepath.Canonicalise(req.URL.Path)
		if err != nil {
			return fmt.Errorf("connections: redirect target path %q: %w", req.URL.Path, err)
		}
		for _, allowed := range allowedPaths {
			canonicalAllowed, err := routepath.Canonicalise(allowed)
			if err != nil {
				continue
			}
			if routepath.HasPathPrefix(canonicalPath, canonicalAllowed) {
				return nil
			}
		}
		return fmt.Errorf("connections: redirect to %q is outside the connection's allowedPaths", req.URL.Path)
	}
}
