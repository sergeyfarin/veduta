// SPDX-License-Identifier: AGPL-3.0-or-later

package icons

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/storage/assetcache"
)

func TestOfflinePackResolvesWithoutCacheOrNetwork(t *testing.T) {
	resolver := New(nil)
	for _, spec := range []string{"mdi:home", "si:github", "sh:immich", "sh:jellyfin", "sh:glances", "sh:beszel", "sh:code"} {
		result, err := resolver.Resolve(context.Background(), spec)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", spec, err)
		}
		if result.ContentType != "image/svg+xml" || len(result.Body) == 0 {
			t.Fatalf("Resolve(%q) = type %q, %d bytes", spec, result.ContentType, len(result.Body))
		}
	}
	specs, err := OfflineSpecs()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(specs) {
		t.Fatalf("offline specs are not deterministic: %v", specs)
	}
}

func TestRemoteIconIsValidatedAndPersistedInDiskCache(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`)),
		}, nil
	})}

	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cache, err := assetcache.New(store, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	resolver := New(cache)
	resolver.client = client

	const spec = "https://icons.example/icon.svg"
	first, err := resolver.Resolve(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	resolver.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected cache miss")
	})}
	second, err := resolver.Resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("cached resolve after upstream stopped: %v", err)
	}
	if calls.Load() != 1 || string(first.Body) != string(second.Body) {
		t.Fatalf("calls=%d first=%q second=%q", calls.Load(), first.Body, second.Body)
	}
}

func TestRejectsInvalidSpecsAndPrivateAddresses(t *testing.T) {
	resolver := New(nil)
	for _, spec := range []string{"mdi:../home", "si:UPPER", "file:///etc/passwd", "https://user:pass@example.com/icon.svg", " https://example.com/icon.svg"} {
		if _, err := resolver.Resolve(context.Background(), spec); !errors.Is(err, ErrInvalid) {
			t.Errorf("Resolve(%q) error = %v, want ErrInvalid", spec, err)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.1.2", "198.18.0.1", "::1", "fe80::1", "224.0.0.1"} {
		if publicAddress(mustAddress(t, raw)) {
			t.Errorf("publicAddress(%q) = true", raw)
		}
	}
	if !publicAddress(mustAddress(t, "8.8.8.8")) {
		t.Error("public address rejected")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSVGValidationRejectsActiveOrExternalContent(t *testing.T) {
	valid := `<svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="g"/></defs><path style="fill:url(#g)" d="M0 0"/></svg>`
	if contentType, ok := validateImage([]byte(valid)); !ok || contentType != "image/svg+xml" {
		t.Fatalf("safe SVG rejected: type=%q ok=%v", contentType, ok)
	}
	for _, malicious := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><image href="http://127.0.0.1/x"/></svg>`,
		`<html></html>`,
	} {
		if _, ok := validateImage([]byte(malicious)); ok {
			t.Errorf("unsafe SVG accepted: %s", malicious)
		}
	}
}

func mustAddress(t *testing.T, raw string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return address
}
