// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
)

// stubResolver lets a test control exactly what LookupIP returns and count how many times it was
// called - the real net.Resolver has no such hook, and testing DNS rebinding against real DNS
// would be unreliable in CI.
type stubResolver struct {
	calls atomic.Int32
	ips   []net.IP // returned on every call, in order; the last one repeats once exhausted
}

func (r *stubResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	i := int(r.calls.Add(1)) - 1
	if i >= len(r.ips) {
		i = len(r.ips) - 1
	}
	return []net.IP{r.ips[i]}, nil
}

// dialerFor lets the test substitute stubResolver for the real resolver a *pinnedDialer
// normally uses - see newPinnedDialer.
func dialerFor(resolver *stubResolver) *pinnedDialer {
	return &pinnedDialer{dialer: &net.Dialer{}, lookup: resolver.LookupIP}
}

// TestPinnedDialer_ResolvesExactlyOncePerDial is the D1 AC: "a rebinding DNS stub does not change
// the dialled IP mid-request." The property that actually prevents rebinding is this one: a single
// DialContext call performs exactly one lookup and connects to exactly that answer - there is no
// separate "check" step whose result could differ from what "use" then does, which is what makes
// a classic TOCTOU rebinding attack impossible here structurally, not by chance.
func TestPinnedDialer_ResolvesExactlyOncePerDial(t *testing.T) {
	lnA := mustListen(t)
	defer lnA.Close()
	lnB := mustListen(t)
	defer lnB.Close()

	hostA, portA := splitHostPort(t, lnA.Addr().String())
	hostB, _ := splitHostPort(t, lnB.Addr().String())
	_ = portA

	stub := &stubResolver{ips: []net.IP{net.ParseIP(hostA), net.ParseIP(hostB)}}
	d := dialerFor(stub)

	// First dial: the stub's first answer (lnA's address) - must connect to lnA, and must have
	// called LookupIP exactly once to get there.
	_, port := splitHostPort(t, lnA.Addr().String())
	conn1, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("rebinding.example", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Close()
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("LookupIP called %d times for one DialContext call, want 1", got)
	}
	if conn1.RemoteAddr().(*net.TCPAddr).IP.String() != hostA {
		t.Fatalf("first dial connected to %s, want the first resolved answer %s", conn1.RemoteAddr(), hostA)
	}

	// Second dial (simulating a later request against the same hostname, after a rebind): the
	// stub now answers with lnB's address. A NEW DialContext call is expected to reflect the new
	// answer - pinning is per-dial, not a permanent process-wide lock to the first IP ever seen -
	// but it must still be exactly one fresh lookup informing exactly one connection.
	_, portB := splitHostPort(t, lnB.Addr().String())
	conn2, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("rebinding.example", portB))
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("LookupIP called %d times across two DialContext calls, want 2", got)
	}
	if conn2.RemoteAddr().(*net.TCPAddr).IP.String() != hostB {
		t.Fatalf("second dial connected to %s, want the second resolved answer %s", conn2.RemoteAddr(), hostB)
	}
}

func TestPinnedDialer_LiteralIPSkipsResolution(t *testing.T) {
	ln := mustListen(t)
	defer ln.Close()
	stub := &stubResolver{ips: []net.IP{net.ParseIP("203.0.113.1")}} // TEST-NET-3, must never be used
	d := dialerFor(stub)

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := stub.calls.Load(); got != 0 {
		t.Fatalf("LookupIP called %d times for a literal IP address, want 0", got)
	}
}

func mustListen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return ln
}

func splitHostPort(t *testing.T, addr string) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
