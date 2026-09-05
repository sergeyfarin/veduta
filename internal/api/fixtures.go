// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
)

// routeFixtures mounts GET /dashboard, /cards, /cards/{id} and /assets/{token} against the
// checked-in showcase. It is only ever called when Config.Fixtures is set - a dev flag, never
// production - so these handlers can be simple: no auth, no ETags, no rate limiting. Phase D/F
// give the real thing those properties when they replace this data source.
func (s *Server) routeFixtures(mux *http.ServeMux) {
	b := s.cfg.Fixtures

	mux.HandleFunc("GET /api/v1/dashboard", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, b.Dashboard)
	})

	mux.HandleFunc("GET /api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, b.Cards)
	})

	mux.HandleFunc("GET /api/v1/cards/{id}", func(w http.ResponseWriter, r *http.Request) {
		cs, ok := b.CardByID(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such card"})
			return
		}
		writeJSON(w, http.StatusOK, cs)
	})

	mux.HandleFunc("GET /api/v1/assets/{token}", func(w http.ResponseWriter, r *http.Request) {
		data, ok := b.Image(r.PathValue("token"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		// Every fixture image is a checked-in JPEG (internal/fixtures/images) and immutable for
		// the life of the ref, same as the real asset proxy's cache contract (section 7).
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		// #nosec G705 -- data is a checked-in local fixture image (internal/fixtures/images),
		// never request input; the token only selects which one, and a miss already returned above.
		_, _ = w.Write(data)
	})
}
