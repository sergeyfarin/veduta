// SPDX-License-Identifier: AGPL-3.0-or-later

package storage_test

import (
	"context"
	"testing"
	"time"

	"veduta.dev/veduta/internal/storage"
)

func TestSignalHistory_BucketsTheWindowAndNothingElse(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	// Two hours of ten-second readings for the card, plus readings that must not come back: the
	// same signal on another card, another signal on this card, and this signal outside the window.
	for i := range 720 {
		ts := from.Add(time.Duration(i) * 10 * time.Second).Format(time.RFC3339Nano)
		if err = s.PutSignalHistory(ctx, "cpu-card", ts, map[string]float64{"cpu.percent": float64(i % 10), "mem.percent": 99}); err != nil {
			t.Fatal(err)
		}
		if err = s.PutSignalHistory(ctx, "other-card", ts, map[string]float64{"cpu.percent": 99}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.PutSignalHistory(ctx, "cpu-card", from.Add(-time.Minute).Format(time.RFC3339Nano), map[string]float64{"cpu.percent": 99}); err != nil {
		t.Fatal(err)
	}

	got, err := s.SignalHistory(ctx, "cpu-card", "cpu.percent", from, from.Add(2*time.Hour), 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 20 || len(got) > 25 {
		t.Fatalf("got %d samples, want about one per bucket (24)", len(got))
	}
	for i, s := range got {
		// Each reading cycles 0..9, so a five-minute bucket averages 4.5 - never 99, which only
		// the other card, the other signal, and the out-of-window reading carry.
		if s.V < 4 || s.V > 5 {
			t.Fatalf("sample %d = %v: a bucket mixed in a reading it should not have", i, s.V)
		}
		if i > 0 && !s.T.After(got[i-1].T) {
			t.Fatalf("sample %d is not after the one before", i)
		}
	}
}
