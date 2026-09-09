// SPDX-License-Identifier: AGPL-3.0-or-later

// Package icons resolves card icon specifications through an offline pack and a bounded disk
// cache. Remote URL fetching rejects non-public destinations so the browser-facing endpoint does
// not become an SSRF primitive.
package icons

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"veduta.dev/veduta/internal/storage/assetcache"
)

const (
	maxIconBytes = 512 << 10
	cacheVersion = "icon-v1"
	mdiVersion   = "7.4.47"
	siVersion    = "16.29.0"
	shCommit     = "c1f7e6b91c9e23317ac72b81f0ee3f9a4ac14aec"
)

//go:embed offline/*.svg
var offline embed.FS

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// ErrInvalid reports a malformed or unsupported icon specification.
	ErrInvalid = errors.New("invalid icon specification")
	// ErrNoNetwork reports a remote-only icon requested from an offline resolver.
	ErrNoNetwork    = errors.New("icon is not in the offline pack and remote fetching is unavailable")
	blockedNetworks = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
)

// Resolver resolves icon specs. Its cache persists successful remote responses across restarts.
type Resolver struct {
	cache  *assetcache.Cache
	client *http.Client
}

// New constructs a resolver. A nil cache still serves the embedded offline pack.
func New(cache *assetcache.Cache) *Resolver {
	return &Resolver{cache: cache, client: safeClient()}
}

// Resolve returns a safe image for an mdi:, si:, sh:, or absolute HTTP(S) specification.
func (r *Resolver) Resolve(ctx context.Context, spec string) (assetcache.Result, error) {
	normalised, remoteURL, offlineName, err := parseSpec(spec)
	if err != nil {
		return assetcache.Result{}, err
	}
	if offlineName != "" {
		if body, readErr := offline.ReadFile("offline/" + offlineName); readErr == nil {
			return assetcache.Result{Body: body, ContentType: "image/svg+xml"}, nil
		}
	}
	if r.cache == nil || r.client == nil {
		return assetcache.Result{}, ErrNoNetwork
	}
	key := cacheKey(normalised)
	return r.cache.GetOrFetch(ctx, key, "icons", func(ctx context.Context) (assetcache.Result, error) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
		if requestErr != nil {
			return assetcache.Result{}, requestErr
		}
		request.Header.Set("Accept", "image/svg+xml,image/png,image/jpeg,image/gif,image/webp")
		request.Header.Set("User-Agent", "Veduta/0.1 icon-proxy")
		response, requestErr := r.client.Do(request)
		if requestErr != nil {
			return assetcache.Result{}, requestErr
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return assetcache.Result{}, fmt.Errorf("icon upstream returned HTTP %d", response.StatusCode)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxIconBytes+1))
		if readErr != nil {
			return assetcache.Result{}, readErr
		}
		contentType, valid := validateImage(body)
		if !valid {
			return assetcache.Result{}, errors.New("icon upstream did not return a safe image")
		}
		return assetcache.Result{Body: body, ContentType: contentType}, nil
	})
}

func parseSpec(spec string) (normalised, remoteURL, offlineName string, err error) {
	if len(spec) == 0 || len(spec) > 2048 || strings.TrimSpace(spec) != spec {
		return "", "", "", ErrInvalid
	}
	for _, prefix := range []string{"mdi:", "si:", "sh:"} {
		if !strings.HasPrefix(spec, prefix) {
			continue
		}
		name := strings.TrimPrefix(spec, prefix)
		if !slugPattern.MatchString(name) {
			return "", "", "", ErrInvalid
		}
		kind := strings.TrimSuffix(prefix, ":")
		switch kind {
		case "mdi":
			remoteURL = "https://cdn.jsdelivr.net/npm/@mdi/svg@" + mdiVersion + "/svg/" + name + ".svg"
		case "si":
			remoteURL = "https://cdn.jsdelivr.net/npm/simple-icons@" + siVersion + "/icons/" + name + ".svg"
		case "sh":
			remoteURL = "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/" + shCommit + "/svg/" + name + ".svg"
		}
		return spec, remoteURL, kind + "-" + name + ".svg", nil
	}

	parsed, parseErr := url.Parse(spec)
	if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", "", "", ErrInvalid
	}
	return parsed.String(), parsed.String(), "", nil
}

func cacheKey(spec string) string {
	digest := sha256.Sum256([]byte(cacheVersion + "\x00" + spec))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validateImage(body []byte) (string, bool) {
	if len(body) == 0 || len(body) > maxIconBytes {
		return "", false
	}
	detected := strings.Split(http.DetectContentType(body), ";")[0]
	switch detected {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return detected, true
	case "text/xml", "text/plain", "application/xml", "application/octet-stream":
		if safeSVG(body) {
			return "image/svg+xml", true
		}
	}
	return "", false
}

func safeSVG(body []byte) bool {
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	rootSeen := false
	inStyle := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return rootSeen
		}
		if err != nil {
			return false
		}
		if text, ok := token.(xml.CharData); ok && inStyle {
			if !safeStyle(string(text)) {
				return false
			}
		}
		if end, ok := token.(xml.EndElement); ok && strings.EqualFold(end.Name.Local, "style") {
			inStyle = false
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		name := strings.ToLower(start.Name.Local)
		inStyle = name == "style"
		if !rootSeen {
			if name != "svg" {
				return false
			}
			rootSeen = true
		}
		switch name {
		case "script", "foreignobject", "iframe", "object", "embed", "audio", "video":
			return false
		}
		for _, attr := range start.Attr {
			attrName := strings.ToLower(attr.Name.Local)
			value := strings.TrimSpace(strings.ToLower(attr.Value))
			if strings.HasPrefix(attrName, "on") {
				return false
			}
			if (attrName == "href" || attrName == "src") && value != "" && !strings.HasPrefix(value, "#") {
				return false
			}
			if attrName == "style" && !safeStyle(value) {
				return false
			}
		}
	}
}

func safeStyle(value string) bool {
	value = strings.ToLower(value)
	return !strings.Contains(value, "@import") &&
		!strings.Contains(strings.ReplaceAll(value, "url(#", ""), "url(")
}

func safeClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, address := range addresses {
				if !publicAddress(address) {
					continue
				}
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errors.New("icon host resolves only to non-public addresses")
		},
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("icon fetch exceeded redirect limit")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return ErrInvalid
			}
			if request.URL.User != nil {
				return ErrInvalid
			}
			return nil
		},
	}
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	for _, network := range blockedNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return true
}

// OfflineSpecs returns the sorted specs bundled into the binary. It is used by tests and makes
// the local-first promise observable without reaching into the embedded filesystem.
func OfflineSpecs() ([]string, error) {
	entries, err := offline.ReadDir("offline")
	if err != nil {
		return nil, err
	}
	specs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".svg" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".svg")
		prefix, slug, ok := strings.Cut(name, "-")
		if ok {
			specs = append(specs, prefix+":"+slug)
		}
	}
	return specs, nil
}
