// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/widgets"
)

func TestSSEUnknownIDResetsAndSlowConsumerDrops(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	_, ch, reset, accepted, done := h.subscribe("unknown", "session")
	if !accepted {
		t.Fatal("subscriber refused")
	}
	if !reset {
		t.Fatal("unknown id must reset")
	}
	done()
	_ = ch
	for i := 0; i < 300; i++ {
		h.publish(scheduler.Event{})
	}
	if len(h.ring) != replaySize {
		t.Fatalf("ring=%d", len(h.ring))
	}
}
func TestSSECardEncoding(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Apply(ctx, []scheduler.Definition{{ID: "x", Hash: "x", Refresh: time.Hour, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		n := len(h.ring)
		h.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for SSE event")
		}
		time.Sleep(time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.ring) == 0 || !strings.Contains(string(h.ring[0].data), `"cardId":"x"`) {
		t.Fatalf("events=%q", h.ring)
	}
}

func TestSSEOneHundredSubscribersAndReplay(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	type subscriber struct {
		ch   chan sseEvent
		done func()
	}
	subs := make([]subscriber, 0, 100)
	for range 100 {
		_, ch, _, accepted, done := h.subscribe("", fmt.Sprintf("session-%d", len(subs)))
		if !accepted {
			t.Fatal("subscriber refused")
		}
		subs = append(subs, subscriber{ch, done})
	}
	h.publish(scheduler.Event{State: schedulerState("x")})
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(s subscriber) {
			defer wg.Done()
			defer s.done()
			select {
			case <-s.ch:
			case <-time.After(time.Second):
				t.Error("subscriber blocked")
			}
		}(sub)
	}
	wg.Wait()
	h.mu.Lock()
	last := h.ring[len(h.ring)-1].id
	h.mu.Unlock()
	h.publish(scheduler.Event{State: schedulerState("y")})
	replay, _, reset, accepted, done := h.subscribe(last, "replay-session")
	defer done()
	if !accepted || reset || len(replay) != 1 || !strings.Contains(string(replay[0].data), `"cardId":"y"`) {
		t.Fatalf("accepted=%v reset=%v replay=%q", accepted, reset, replay)
	}
}

func schedulerState(id string) state.CardState { return state.Pending(id) }

func TestSSEGlobalCapRefusesExcessStream(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	dones := make([]func(), 0, maxSSEClients)
	for range maxSSEClients {
		_, _, _, accepted, done := h.subscribe("", fmt.Sprintf("session-%d", len(dones)))
		if !accepted {
			t.Fatal("client below cap refused")
		}
		dones = append(dones, done)
	}
	defer func() {
		for _, done := range dones {
			done()
		}
	}()
	_, _, _, accepted, _ := h.subscribe("", "overflow-session")
	if accepted {
		t.Fatal("client above global cap accepted")
	}
}

func TestSSEPerSessionCapRefusesExcessStream(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	dones := make([]func(), 0, maxSSEClientsPerSession)
	for range maxSSEClientsPerSession {
		_, _, _, accepted, done := h.subscribe("", "same-session")
		if !accepted {
			t.Fatal("stream below per-session cap refused")
		}
		dones = append(dones, done)
	}
	defer func() {
		for _, done := range dones {
			done()
		}
	}()
	_, _, _, accepted, _ := h.subscribe("", "same-session")
	if accepted {
		t.Fatal("stream above per-session cap accepted")
	}
}

// TestSSEDroppedSlowConsumerReleasesSessionSlot pins the interaction between the per-session cap
// and the drop-slow-consumers path: a stream removed by publish must release its session slot,
// not just its client entry. Before removeClient owned both, a session whose streams were all
// dropped this way kept a permanent count and was refused every later subscription for the life
// of the process - a live-update outage for one user that survived reconnecting.
func TestSSEDroppedSlowConsumerReleasesSessionSlot(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	for range maxSSEClientsPerSession {
		if _, _, _, accepted, _ := h.subscribe("", "slow-session"); !accepted {
			t.Fatal("stream below per-session cap refused")
		}
	}
	// 33 events overflow every 32-deep buffer, so publish drops all four streams.
	for range 33 {
		h.publish(scheduler.Event{})
	}
	h.mu.Lock()
	clients, sessions := len(h.clients), h.sessions["slow-session"]
	h.mu.Unlock()
	if clients != 0 {
		t.Fatalf("slow consumers not dropped: clients=%d", clients)
	}
	if sessions != 0 {
		t.Fatalf("dropped streams leaked %d per-session slots", sessions)
	}
	_, _, _, accepted, done := h.subscribe("", "slow-session")
	if !accepted {
		t.Fatal("session refused a new stream after all its streams were dropped")
	}
	done()
}

// TestSSEDoneAfterDropDoesNotDoubleRelease covers the ordering a real handler produces: publish
// drops the stream, then the handler's deferred done runs anyway. The second removal must be a
// no-op rather than decrementing a slot it no longer holds (or closing a closed channel).
func TestSSEDoneAfterDropDoesNotDoubleRelease(t *testing.T) {
	m := scheduler.New(nil)
	defer m.Close()
	h := newSSEHub(m)
	_, first, _, accepted, dropped := h.subscribe("", "shared-session")
	if !accepted {
		t.Fatal("first stream refused")
	}
	_, _, _, accepted, kept := h.subscribe("", "shared-session")
	if !accepted {
		t.Fatal("second stream refused")
	}
	defer kept()
	// Drop exactly the first of the two, as publish would for a single slow consumer.
	h.mu.Lock()
	h.removeClient(first)
	h.mu.Unlock()
	dropped()
	h.mu.Lock()
	sessions := h.sessions["shared-session"]
	h.mu.Unlock()
	if sessions != 1 {
		t.Fatalf("session count=%d, want 1 remaining stream", sessions)
	}
}

// TestSSEReplayPreservesEventTypes is the regression test for what makes a config notification
// survive a reconnect. Every ring entry used to be written back as "card" on replay, because the
// event name was assumed at write time rather than stored; a replayed config event would have
// arrived as a card and been parsed as a card state. A client that was briefly disconnected is
// exactly the client that most needs to hear its layout changed.
func TestSSEReplayPreservesEventTypes(t *testing.T) {
	h := &sseHub{nonce: "n", clients: map[chan sseEvent]string{}, sessions: map[string]int{}}
	h.broadcast("card", []byte(`{"cardId":"a"}`))
	h.publishConfig(7)

	replay, _, reset, accepted, done := h.subscribe("n-1", "session")
	defer done()
	if !accepted || reset {
		t.Fatalf("accepted=%v reset=%v, want a clean replay from a known event id", accepted, reset)
	}
	if len(replay) != 1 {
		t.Fatalf("replayed %d events, want 1", len(replay))
	}
	if replay[0].event != "config" {
		t.Fatalf("replayed event = %q, want %q", replay[0].event, "config")
	}
	if string(replay[0].data) != `{"generation":7}` {
		t.Fatalf("replayed data = %s", replay[0].data)
	}
}

// TestPublishConfigReachesLiveClients covers the ordinary path: a connected client is told.
func TestPublishConfigReachesLiveClients(t *testing.T) {
	h := &sseHub{nonce: "n", clients: map[chan sseEvent]string{}, sessions: map[string]int{}}
	_, ch, _, accepted, done := h.subscribe("", "session")
	if !accepted {
		t.Fatal("subscribe was refused")
	}
	defer done()
	h.publishConfig(3)
	select {
	case e := <-ch:
		if e.event != "config" || string(e.data) != `{"generation":3}` {
			t.Fatalf("event = %q data = %s", e.event, e.data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a live client was never told about the new generation")
	}
}
