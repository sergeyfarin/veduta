// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"

	"veduta.dev/veduta/internal/secrets"
)

// Stage names which layer of a connectivity probe failed - milestone D5's own AC: "test endpoint
// distinguishes DNS / TCP / TLS / auth / HTTP-status failures." Empty means the probe succeeded.
type Stage string

// Stage values.
const (
	StageDNS    Stage = "dns"
	StageTCP    Stage = "tcp"
	StageTLS    Stage = "tls"
	StageAuth   Stage = "auth"
	StageStatus Stage = "http-status"
)

// Health is one connection's reachability, for GET /api/v1/connections and
// POST /api/v1/connections/{id}/test. Reachable is true only for a successful, 2xx round trip;
// otherwise Stage says which layer failed and Error carries the underlying detail - never a
// credential, since the only inputs to this classification are network-level errors and an HTTP
// status code, neither of which can carry a resolved secrets.Value.
type Health struct {
	Reachable  bool   `json:"reachable"`
	Stage      Stage  `json:"stage,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Health implements Registry: a real request against the connection's base URL, through the same
// Do path (rate limiting, auth injection, size caps) every other caller uses - a manual "test
// connection" click is not a reason to bypass the limits protecting the upstream service - with
// the resulting error classified by stage rather than reported as one opaque string.
func (r *registry) Health(ctx context.Context, id string) Health {
	conn, ok := r.Get(id)
	if !ok {
		return Health{Error: ErrUnknownConnection.Error()}
	}
	if conn.Kind != KindHTTP {
		return Health{Error: ErrNotHTTP.Error()}
	}
	resp, err := r.Do(ctx, id, Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		// A query-type auth connection injects its credential into the request URL
		// (injectAuth); a transport-level failure's *url.Error formats as `Op "URL": Err`, so
		// the raw resolved value would otherwise appear verbatim in this response. Scrubbed
		// through the same process-wide registry the log handler uses - every secrets.Value
		// ever constructed already tracks itself there, active before this ever runs.
		return Health{Stage: classifyError(err), Error: secrets.DefaultRegistry().Scrub(err.Error())}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Health{Stage: StageAuth, StatusCode: resp.StatusCode, Error: http.StatusText(resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Health{Stage: StageStatus, StatusCode: resp.StatusCode, Error: http.StatusText(resp.StatusCode)}
	}
	return Health{Reachable: true, StatusCode: resp.StatusCode}
}

// classifyError walks err's cause chain (errors.As, so it sees through *url.Error and any other
// wrapping) and reports the earliest layer that could have produced it: DNS resolution
// (pinnedDialer's own net.DefaultResolver.LookupIP call), the TCP dial itself, or the TLS
// handshake http.Transport performs over the resulting connection - see dial.go and client.go for
// where each of these actually happens. Falls back to StageTCP for a dial-shaped net.OpError this
// project's own error types don't further distinguish, and "" (no stage) only when nothing here
// recognises the error at all - still reported via Health.Error, just not staged.
func classifyError(err error) Stage {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return StageDNS
	}

	var certErr *tls.CertificateVerificationError
	var hostErr x509.HostnameError
	var authErr x509.UnknownAuthorityError
	var invalidErr x509.CertificateInvalidError
	var recordErr tls.RecordHeaderError
	if errors.As(err, &certErr) || errors.As(err, &hostErr) || errors.As(err, &authErr) ||
		errors.As(err, &invalidErr) || errors.As(err, &recordErr) {
		return StageTLS
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		// A timeout with no more specific cause identified above happened establishing the
		// connection (dial) rather than performing the TLS handshake or reading a response -
		// classified as TCP, the layer a connect-timeout actually belongs to.
		return StageTCP
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return StageTCP
	}

	return ""
}
