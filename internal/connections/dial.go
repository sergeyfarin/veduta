// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"errors"
	"net"
	"time"
)

// pinnedDialer resolves a hostname exactly once per dial and connects to that specific IP,
// rather than letting the standard library's dialer resolve the name itself. http.Transport
// calls DialContext once per new TCP connection, and everything sent or received over that
// connection afterwards uses it directly - no second lookup happens mid-request - so a DNS
// answer that changes between one request and the next cannot redirect where an in-flight
// request's data goes. TLS (when used) still verifies against the real hostname: this only
// changes DialContext, so http.Transport wraps the resulting raw connection in TLS itself using
// its own ServerName, unaffected by which IP the TCP socket actually reached.
type pinnedDialer struct {
	dialer *net.Dialer
	// lookup is a field, not a direct call to net.DefaultResolver.LookupIP, so a test can
	// substitute a stub and both count calls and control exactly what each one answers - see
	// dial_test.go's stubResolver. Production code always gets newPinnedDialer's real resolver.
	lookup func(ctx context.Context, network, host string) ([]net.IP, error)
}

func newPinnedDialer() *pinnedDialer {
	return &pinnedDialer{
		dialer: &net.Dialer{Timeout: 10 * time.Second}, // overridden per-request by context deadline
		lookup: net.DefaultResolver.LookupIP,
	}
}

var errNoAddresses = errors.New("connections: host resolved to no addresses")

// DialContext implements the signature http.Transport.DialContext expects.
func (d *pinnedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		// Already a literal IP: nothing to resolve, nothing to pin.
		return d.dialer.DialContext(ctx, network, addr)
	}

	ips, err := d.lookup(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errNoAddresses
	}
	// The first answer, resolved exactly once for this dial - not re-resolved for retries within
	// the same call, and never consulted again for the lifetime of the resulting connection.
	pinned := net.JoinHostPort(ips[0].String(), port)
	return d.dialer.DialContext(ctx, network, pinned)
}
