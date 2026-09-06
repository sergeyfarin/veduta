// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/secrets"
)

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func singleHTTPRegistry(t *testing.T, id string, cfg *HTTPConfig) Registry {
	t.Helper()
	c, err := newHTTPClient(id, cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	return &registry{
		connections: map[string]*Connection{id: {ID: id, Kind: KindHTTP, HTTP: cfg}},
		clients:     map[string]*client{id: c},
	}
}

func TestDo_AuthInjectedEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "supersecretkey12" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := &HTTPConfig{
		BaseURL: srv.URL,
		Auth:    Auth{Type: AuthHeader, Name: "X-Api-Key", Value: secrets.New("supersecretkey12")},
	}
	reg := singleHTTPRegistry(t, "x", cfg)
	resp, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestDo_UnknownConnection(t *testing.T) {
	reg := &registry{connections: map[string]*Connection{}, clients: map[string]*client{}}
	_, err := reg.Do(context.Background(), "ghost", Request{Method: http.MethodGet})
	if !errors.Is(err, ErrUnknownConnection) {
		t.Fatalf("err = %v, want ErrUnknownConnection", err)
	}
}

func TestDo_NotHTTPConnection(t *testing.T) {
	reg := &registry{
		connections: map[string]*Connection{"d": {ID: "d", Kind: KindDocker, Docker: &DockerConfig{}}},
		clients:     map[string]*client{},
	}
	_, err := reg.Do(context.Background(), "d", Request{Method: http.MethodGet})
	if !errors.Is(err, ErrNotHTTP) {
		t.Fatalf("err = %v, want ErrNotHTTP", err)
	}
}

// TestDo_RedirectToAnotherHostRefused is the D1 AC: "redirect to another host refused."
func TestDo_RedirectToAnotherHostRefused(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, MaxRedirects: 3}
	reg := singleHTTPRegistry(t, "x", cfg)
	_, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/"})
	if err == nil {
		t.Fatal("want an error: redirect to a different host must be refused")
	}
}

// TestDo_RedirectWithinDefaultZeroIsRefused: MaxRedirects defaults to 0 - docs/01-architecture.md
// section 8 - so even a same-host redirect is refused unless explicitly allowed.
func TestDo_RedirectWithinDefaultZeroIsRefused(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, srv.URL+"/end", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL} // MaxRedirects: 0 (default)
	reg := singleHTTPRegistry(t, "x", cfg)
	_, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/start"})
	if err == nil {
		t.Fatal("want an error: default MaxRedirects is 0, no redirect should be followed")
	}
}

// TestDo_SameHostRedirectAllowedWithinLimit proves the refusal above is about the default limit
// and cross-host safety, not redirects being unconditionally broken.
func TestDo_SameHostRedirectAllowedWithinLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, srv.URL+"/end", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("landed"))
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, MaxRedirects: 1}
	reg := singleHTTPRegistry(t, "x", cfg)
	resp, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/start"})
	if err != nil {
		t.Fatalf("a same-host redirect within the limit should succeed: %v", err)
	}
	if string(resp.Body) != "landed" {
		t.Fatalf("body = %q", resp.Body)
	}
}

// TestDo_OversizedResponseIsAnError is the D1 AC: "oversized response truncated with an error."
func TestDo_OversizedResponseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, MaxResponseBytes: 100}
	reg := singleHTTPRegistry(t, "x", cfg)
	_, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/"})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestDo_ResponseWithinLimitSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("small"))
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, MaxResponseBytes: 100}
	reg := singleHTTPRegistry(t, "x", cfg)
	resp, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "small" {
		t.Fatalf("body = %q", resp.Body)
	}
}

// TestDo_TimeoutHonoured is the D1 AC: "timeout honoured."
func TestDo_TimeoutHonoured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, Timeout: 20 * time.Millisecond}
	reg := singleHTTPRegistry(t, "x", cfg)
	start := time.Now()
	_, err := reg.Do(context.Background(), "x", Request{Method: http.MethodGet, Path: "/"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("Do took %s, want it to time out around 20ms, well before the server's 200ms sleep", elapsed)
	}
}

// TestDo_StaticHeadersAreSentAndOverrideTheCaller is the regression test for a real bug found in
// review: HTTPConfig.Headers (connections.*.headers in config) was computed and stored at build
// time, and the broker already treats these names as connection-owned (stripping any
// plugin-supplied value for them), but Do itself never actually added them to the outgoing
// request - a configured static header silently never went out on the wire at all. Also proves
// precedence: a connection header always overrides whatever a caller supplied for the same name,
// matching the ownership model the broker's own filtering already assumes.
func TestDo_StaticHeadersAreSentAndOverrideTheCaller(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Static")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, Headers: map[string]string{"X-Static": "from-config"}}
	reg := singleHTTPRegistry(t, "x", cfg)

	_, err := reg.Do(context.Background(), "x", Request{
		Method: http.MethodGet, Path: "/",
		Headers: map[string]string{"X-Static": "from-caller"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-config" {
		t.Fatalf("upstream saw X-Static=%q, want the connection's own configured value to win", got)
	}
}

// TestDo_RateLimiterEnforced is the D1 AC: "rate limiter enforced." A limiter of 1 request per
// (effectively) never, burst 1, means a second immediate request must wait - proven by giving it
// a context that expires before the limiter would ever let it through.
func TestDo_RateLimiterEnforced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &HTTPConfig{BaseURL: srv.URL, RateLimit: RateLimit{RPS: 0.001, Burst: 1}}
	reg := singleHTTPRegistry(t, "x", cfg)

	ctx := context.Background()
	if _, err := reg.Do(ctx, "x", Request{Method: http.MethodGet, Path: "/"}); err != nil {
		t.Fatalf("first request (within burst) should succeed: %v", err)
	}

	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := reg.Do(shortCtx, "x", Request{Method: http.MethodGet, Path: "/"})
	if err == nil {
		t.Fatal("second request should have been rate-limited and blocked past the short deadline")
	}
}
