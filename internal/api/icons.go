// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"errors"
	"net/http"

	"veduta.dev/veduta/internal/icons"
)

func (s *Server) routeIcons(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/icons/{spec}", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.cfg.IconProxy.Resolve(r.Context(), r.PathValue("spec"))
		if errors.Is(err, icons.ErrInvalid) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.log.Warn("icon unavailable", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "icon unavailable"})
			return
		}
		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Body)
	})
}
