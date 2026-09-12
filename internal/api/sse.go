// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"veduta.dev/veduta/internal/scheduler"
)

const replaySize = 256
const maxSSEClients = 128
const maxSSEClientsPerSession = 4

type sseEvent struct {
	id   string
	data []byte
}
type sseHub struct {
	mu    sync.Mutex
	nonce string
	next  uint64
	ring  []sseEvent
	// clients maps a live stream's channel to the session that owns it, so every removal site
	// can release the per-session slot as well as the client itself - see removeClient.
	clients  map[chan sseEvent]string
	sessions map[string]int
}

func newSSEHub(m *scheduler.Manager) *sseHub {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	h := &sseHub{nonce: hex.EncodeToString(b), clients: map[chan sseEvent]string{}, sessions: map[string]int{}}
	ch, _ := m.Subscribe(32)
	go func() {
		for ev := range ch {
			h.publish(ev)
		}
	}()
	return h
}
func (h *sseHub) publish(ev scheduler.Event) {
	data, _ := json.Marshal(ev.State)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	e := sseEvent{id: h.nonce + "-" + strconv.FormatUint(h.next, 10), data: data}
	h.ring = append(h.ring, e)
	if len(h.ring) > replaySize {
		h.ring = h.ring[len(h.ring)-replaySize:]
	}
	for ch := range h.clients {
		select {
		case ch <- e:
		default:
			h.removeClient(ch)
		}
	}
}

// removeClient closes ch and releases every slot it holds. Both removal sites - the
// drop-slow-consumer path above and the done closure subscribe returns - go through here, so a
// dropped consumer cannot leak its per-session slot the way it did when publish touched only
// h.clients and left h.sessions counting a stream that no longer exists. Caller holds h.mu.
func (h *sseHub) removeClient(ch chan sseEvent) {
	sessionID, ok := h.clients[ch]
	if !ok {
		return
	}
	delete(h.clients, ch)
	if sessionID != "" {
		h.sessions[sessionID]--
		if h.sessions[sessionID] <= 0 {
			delete(h.sessions, sessionID)
		}
	}
	close(ch)
}
func (h *sseHub) subscribe(last, sessionID string) ([]sseEvent, chan sseEvent, bool, bool, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= maxSSEClients || sessionID != "" && h.sessions[sessionID] >= maxSSEClientsPerSession {
		return nil, nil, false, false, func() {}
	}
	// A fresh stream has no event id tying its earlier GET /cards snapshot to the replay ring.
	// Register it first, then force one refetch; this closes the initial fetch/subscribe race.
	reset := last == ""
	start := len(h.ring)
	if last != "" {
		start = -1
		for i, e := range h.ring {
			if e.id == last {
				start = i + 1
				break
			}
		}
		if start < 0 {
			reset = true
			start = len(h.ring)
		}
	}
	replay := append([]sseEvent(nil), h.ring[start:]...)
	ch := make(chan sseEvent, 32)
	h.clients[ch] = sessionID
	if sessionID != "" {
		h.sessions[sessionID]++
	}
	return replay, ch, reset, true, func() {
		h.mu.Lock()
		h.removeClient(ch)
		h.mu.Unlock()
	}
}
func writeSSE(w http.ResponseWriter, event, id string, data []byte) {
	if id != "" {
		_, _ = fmt.Fprintf(w, "id: %s\n", id)
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	for _, line := range strings.Split(string(data), "\n") {
		_, _ = fmt.Fprintf(w, "data: %s\n", line)
	}
	_, _ = fmt.Fprint(w, "\n")
}
func (s *Server) routeSSE(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		identity, _ := identityFromContext(r.Context())
		replay, ch, reset, accepted, done := s.hub.subscribe(r.Header.Get("Last-Event-ID"), identity.SessionID)
		if !accepted {
			http.Error(w, "too many streams", http.StatusServiceUnavailable)
			return
		}
		defer done()
		if reset {
			writeSSE(w, "reset", "", []byte(`{}`))
			f.Flush()
		}
		for _, e := range replay {
			writeSSE(w, "card", e.id, e.data)
			f.Flush()
		}
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case e, open := <-ch:
				if !open {
					return
				}
				writeSSE(w, "card", e.id, e.data)
				f.Flush()
			case <-ticker.C:
				writeSSE(w, "ping", "", []byte(`{}`))
				f.Flush()
			}
		}
	})
}
