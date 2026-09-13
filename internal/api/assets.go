// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/connections/routepath"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/storage/assetcache"
)

const maxAssetBytes = 8 << 20

// AssetProxy contains the services required to authorise and serve signed assets.
type AssetProxy struct {
	Tokens    *assettokens.Service
	Store     *storage.Store
	Cache     *assetcache.Cache
	Registry  connections.Registry
	Authorize func(context.Context, assettokens.Payload) (bool, error)
}

func (s *Server) routeAssets(mux *http.ServeMux) {
	p := s.cfg.AssetProxy
	mux.HandleFunc("GET /api/v1/assets/{token}", func(w http.ResponseWriter, r *http.Request) {
		payload, err := p.Tokens.Verify(r.PathValue("token"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		revision, ok, err := p.Store.ConnectionRevision(r.Context(), payload.Connection)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "asset lookup failed"})
			return
		}
		if !ok || subtle.ConstantTimeCompare([]byte(revision), []byte(payload.ConnectionRevision)) != 1 {
			http.NotFound(w, r)
			return
		}
		canonical, err := routepath.Canonicalise(payload.Path)
		if err != nil || canonical != payload.Path {
			http.NotFound(w, r)
			return
		}
		query, err := url.ParseQuery(payload.Query)
		if err != nil || canonicalQueryValues(query) != payload.Query || !validTransform(payload.Transform) {
			http.NotFound(w, r)
			return
		}
		allowed, err := p.Authorize(r.Context(), payload)
		if err != nil {
			s.log.Error("asset authorization failed", "error", err)
			http.NotFound(w, r)
			return
		}
		if !allowed {
			http.NotFound(w, r)
			return
		}
		result, err := p.Cache.GetOrFetch(r.Context(), assettokens.CacheKey(payload), payload.Connection, func(ctx context.Context) (assetcache.Result, error) {
			// The signed token authorises one path; following a redirect would fetch a
			// different one under the same signature. Found in review: the broker attaches this
			// same re-check for a plugin's own HTTP calls, but the asset proxy never did, so an
			// approved thumbnail that redirected to an unapproved path was fetched with the
			// connection's credentials and served as an image. The destination is re-run through
			// the very authorisation this request already passed, against the asset permissions
			// as they stand now.
			ctx = connections.WithRedirectAuthorizer(ctx, func(dest connections.RedirectRequest) error {
				destination := payload
				destination.Path = dest.Path
				destination.Query = url.Values(valuesOf(dest.Query)).Encode()
				allowed, authErr := p.Authorize(ctx, destination)
				if authErr != nil {
					return authErr
				}
				if !allowed {
					return fmt.Errorf("no asset route permits a redirect to %s %s", dest.Method, dest.Path)
				}
				return nil
			})
			resp, requestErr := p.Registry.Do(ctx, payload.Connection, connections.Request{Method: "GET", Path: payload.Path, Query: firstQueryValues(query), MaxResponseBytes: maxAssetBytes})
			if requestErr != nil {
				return assetcache.Result{}, requestErr
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return assetcache.Result{}, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
			}
			contentType, ok := sniffImage(resp.Body)
			if !ok {
				return assetcache.Result{}, errNotImage
			}
			return assetcache.Result{Body: resp.Body, ContentType: contentType}, nil
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "asset unavailable"})
			return
		}
		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("Cache-Control", "private, max-age="+maxAge(payload.Expires)+", immutable")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Body)
	})
}

type assetError string

func (e assetError) Error() string { return string(e) }

const errNotImage assetError = "upstream response is not a safe image"

func sniffImage(body []byte) (string, bool) {
	if len(body) == 0 || len(body) > maxAssetBytes {
		return "", false
	}
	mime := strings.Split(http.DetectContentType(body), ";")[0]
	switch mime {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return "", false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return "", false
	}
	return mime, true
}
func firstQueryValues(q url.Values) map[string]string {
	out := map[string]string{}
	for k, v := range q {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
func canonicalQueryValues(q url.Values) string { return q.Encode() }

// valuesOf turns redirectPolicy's single-valued query map back into url.Values, the shape the
// asset payload and its authorisation both speak in.
func valuesOf(q map[string]string) map[string][]string {
	out := make(map[string][]string, len(q))
	for k, v := range q {
		out[k] = []string{v}
	}
	return out
}

var servedTransform = regexp.MustCompile(`^(w=(160|320|640|1280))?(,f=(webp|jpeg))?$`)

func validTransform(v string) bool { return servedTransform.MatchString(v) }
func maxAge(exp int64) string {
	n := exp - time.Now().Unix()
	if n < 0 {
		n = 0
	}
	return strconv.FormatInt(n, 10)
}
