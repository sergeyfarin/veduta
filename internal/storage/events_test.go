// SPDX-License-Identifier: AGPL-3.0-or-later

package storage_test

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"veduta.dev/veduta/internal/storage"
)

func TestSignalHistoryAndRecentEventsPersist(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err = store.PutSignalHistory(context.Background(), "card", "2026-09-09T00:00:00Z", map[string]float64{"cpu": 12.5}); err != nil {
		t.Fatal(err)
	}
	if err = store.AppendEvent(context.Background(), storage.Event{Type: "plugin.notice", Severity: "info", Source: "example", Data: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	if err = store.AppendEvent(context.Background(), storage.Event{Type: "plugin.empty", Severity: "info"}); err != nil {
		t.Fatal(err)
	}
	var value float64
	err = store.DB().QueryRow(`SELECT value FROM signal_history WHERE card_id='card' AND signal='cpu'`).Scan(&value)
	if err != nil || value != 12.5 {
		t.Fatalf("value=%v err=%v", value, err)
	}
	events, err := store.RecentEvents(context.Background(), 10)
	if err != nil || len(events) != 2 || events[0].Type != "plugin.empty" || events[0].Data != nil || string(events[1].Data) != `{"ok":true}` {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestAppendEventRejectsInvalidJSON(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	err = store.AppendEvent(context.Background(), storage.Event{Type: "plugin.notice", Severity: "info", Data: json.RawMessage(`{`)})
	if err == nil {
		t.Fatal("invalid event JSON was accepted")
	}
}

func TestRecentEventsBoundsUntrustedLimit(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err = store.AppendEvent(context.Background(), storage.Event{Type: "test", Severity: "info"}); err != nil {
		t.Fatal(err)
	}

	events, err := store.RecentEvents(context.Background(), math.MaxInt)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d want=1", len(events))
	}
}
