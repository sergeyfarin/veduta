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
		CheckRedirect: redirectPolicy(base, cfg.MaxRedirects),
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

// redirectPolicy refuses a redirect to a different host outright, and any redirect beyond
// maxRedirects - docs/01-architecture.md section 8's default of 0 means "no other package" needs
// its own opinion about redirects, since Go's http.Client already has none once this is set.
func redirectPolicy(base *url.URL, maxRedirects int) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return fmt.Errorf("connections: exceeded the %d allowed redirect(s)", maxRedirects)
		}
		if req.URL.Host != base.Host {
			return fmt.Errorf("connections: redirect to a different host %q refused", req.URL.Host)
		}
		return nil
	}
}
