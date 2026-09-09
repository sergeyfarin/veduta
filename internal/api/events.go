// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"strconv"
)

func (s *Server) routeEvents(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := s.cfg.EventStore.RecentEvents(r.Context(), limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load events"})
			return
		}
		writeJSON(w, http.StatusOK, events)
	})
}
