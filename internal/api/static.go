// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// staticHandler serves the embedded SPA.
//
// Two rules matter. Hashed assets under /assets/ are immutable and cached for a year, because
// their names change when their contents do. index.html is never cached, because it is the
// document that names those hashes - caching it is how a browser ends up asking for a bundle
// that no longer exists.
//
// Anything that is not a real file and does not start with /api falls back to index.html, so a
// deep link into a client-side route works on a cold load.
func (s *Server) staticHandler(assets fs.FS, present bool) http.Handler {
	var files http.Handler
	if present {
		files = http.FileServer(http.FS(assets))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := path.Clean("/" + r.URL.Path)

		// /api is the server's namespace: never shadow it, whether or not a frontend is
		// embedded. A typo in an endpoint must 404, or it returns a page with a 200 and looks
		// like a routing bug for an hour. This check comes first for that reason - an earlier
		// version put it inside the has-a-build branch, and an API-only binary answered 503
		// for every unknown endpoint.
		if upath == "/api" || strings.HasPrefix(upath, "/api/") {
			http.NotFound(w, r)
			return
		}

		if !present {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "no frontend build is embedded in this binary",
				"hint":  "run `pnpm build` (or `pnpm --filter veduta-web build`) and rebuild",
			})
			return
		}

		if upath != "/" {
			if f, err := assets.Open(strings.TrimPrefix(upath, "/")); err == nil {
				info, statErr := f.Stat()
				_ = f.Close()
				if statErr == nil && !info.IsDir() {
					if strings.HasPrefix(upath, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						w.Header().Set("Cache-Control", "no-cache")
					}
					files.ServeHTTP(w, r)
					return
				}
			}
		}

		s.serveIndex(w, r, assets)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	body, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The dashboard renders no plugin JS and no plugin HTML, so the policy can be this tight
	// from the first commit rather than loosened later. See docs/01-architecture.md section 8.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
			"script-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; "+
			"frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
