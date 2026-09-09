// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"sort"
	"sync"

	"veduta.dev/veduta/internal/audit"
	"veduta.dev/veduta/internal/auth"
	"veduta.dev/veduta/internal/connections"
)

// Milestone D5: GET /api/v1/connections and POST /api/v1/connections/{id}/test -
// docs/01-architecture.md section 9: "GET /connections | admin: ids, kinds, health - never
// credentials" and "POST /connections/{id}/test | connectivity + auth probe". Both go through
// the exact same connections.Registry.Health every other caller would (see health.go's own
// scrubbing of a query-auth credential that could otherwise appear in a network-level error) -
// this file only shapes the response, it does not add a second probe.
//
// connectionSummary is deliberately minimal - id, kind, health, nothing else. There is no field
// here a credential could ever occupy, by construction, rather than by remembering to omit one:
// "never credentials" is enforced by this type's shape, not by a redaction step over a richer one.
type connectionSummary struct {
	ID     string             `json:"id"`
	Kind   string             `json:"kind"`
	Health connections.Health `json:"health"`
}

func (s *Server) routeConnections(mux *http.ServeMux) {
	store := s.cfg.ConfigStore
	reg := s.cfg.Registry

	mux.HandleFunc("GET /api/v1/connections", func(w http.ResponseWriter, r *http.Request) {
		snapshot := store.Snapshot()
		if snapshot == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no valid configuration loaded"})
			return
		}
		ids := make([]string, 0, len(snapshot.Config.Connections))
		for id := range snapshot.Config.Connections {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		// Checked concurrently, not in a sequential loop sharing one request-scoped context:
		// found in review (a CI-only failure, not reproducing locally) that a slow or
		// unreachable connection checked first could consume the whole remaining context
		// budget before a later, genuinely healthy connection ever got probed - reporting it
		// unreachable for a reason that had nothing to do with its own health. Every check now
		// starts against the same context at the same time, so one connection's slowness no
		// longer starves another's.
		health := make([]connections.Health, len(ids))
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				health[i] = reg.Health(r.Context(), id)
			}(i, id)
		}
		wg.Wait()
		out := make([]connectionSummary, 0, len(ids))
		for i, id := range ids {
			out = append(out, connectionSummary{
				ID:     id,
				Kind:   snapshot.Config.Connections[id].Kind,
				Health: health[i],
			})
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/v1/connections/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		identity, _ := identityFromContext(r.Context())
		snapshot := store.Snapshot()
		if snapshot == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no valid configuration loaded"})
			return
		}
		conn, ok := snapshot.Config.Connections[id]
		if !ok {
			s.audit(r.Context(), audit.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "connection.test", Target: id, Outcome: "failure"})
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such connection"})
			return
		}
		health := reg.Health(r.Context(), id)
		outcome := "success"
		if !health.Reachable {
			outcome = "failure"
		}
		s.audit(r.Context(), audit.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "connection.test", Target: id, Outcome: outcome})
		writeJSON(w, http.StatusOK, connectionSummary{ID: id, Kind: conn.Kind, Health: health})
	})
}
