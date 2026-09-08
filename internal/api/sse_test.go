// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
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
	_, ch, reset, accepted, done := h.subscribe("unknown")
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
		_, ch, _, accepted, done := h.subscribe("")
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
	replay, _, reset, accepted, done := h.subscribe(last)
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
		_, _, _, accepted, done := h.subscribe("")
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
	_, _, _, accepted, _ := h.subscribe("")
	if accepted {
		t.Fatal("client above global cap accepted")
	}
}
