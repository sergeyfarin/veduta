// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

func every(n int, step time.Duration, from time.Time) []Sample {
	out := make([]Sample, n)
	for i := range out {
		out[i] = Sample{T: from.Add(time.Duration(i) * step), V: float64(i)}
	}
	return out
}

func TestDownsample_KeepsASmallSeriesAsIs(t *testing.T) {
	pts := Downsample(every(10, time.Minute, t0), t0, t0.Add(time.Hour))
	if len(pts) != 10 {
		t.Fatalf("got %d points, want the 10 recorded", len(pts))
	}
	for i, p := range pts {
		if p.V == nil || *p.V != float64(i) {
			t.Fatalf("point %d = %v, want %d unchanged", i, p.V, i)
		}
	}
}

func TestDownsample_BoundsALongSeriesAndStaysIncreasing(t *testing.T) {
	// A week of one-minute readings: 10080 samples.
	pts := Downsample(every(7*24*60, time.Minute, t0), t0, t0.Add(7*24*time.Hour))
	if len(pts) > MaxHistoryPoints || len(pts) < 100 {
		t.Fatalf("got %d points, want at most %d and a useful number", len(pts), MaxHistoryPoints)
	}
	var prev time.Time
	for i, p := range pts {
		ts, err := time.Parse(time.RFC3339Nano, p.T)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !ts.After(prev) {
			t.Fatalf("point %d at %s is not after %s", i, ts, prev)
		}
		if p.V == nil {
			t.Fatalf("point %d is a gap, but the series had no silence", i)
		}
		prev = ts
	}
}

// Averaging must not move a bucket's value outside what was recorded in it - a mean of 0..9 is
// 4.5, never 10.
func TestDownsample_AveragesWithinABucket(t *testing.T) {
	samples := every(1000, time.Second, t0)
	pts := Downsample(samples, t0, t0.Add(1000*time.Second))
	first := *pts[0].V
	if first < 0 || first > 10 {
		t.Fatalf("first bucket mean = %v, want the mean of its first few readings", first)
	}
}

// The point of the whole binding: a stretch with no readings - the card failing, the server down -
// is shown as missing, not bridged by a line that claims a trend.
func TestDownsample_MarksALongSilenceAsAGap(t *testing.T) {
	samples := append(every(20, time.Minute, t0), every(20, time.Minute, t0.Add(3*time.Hour))...)
	pts := Downsample(samples, t0, t0.Add(4*time.Hour))
	gaps := 0
	for _, p := range pts {
		if p.V == nil {
			gaps++
		}
	}
	if gaps != 1 {
		t.Fatalf("got %d gaps, want exactly one across the silence", gaps)
	}
	if len(pts) != 41 {
		t.Fatalf("got %d points, want 40 readings and one gap", len(pts))
	}
}

func TestDownsample_NoGapsForAnEvenSeriesOrTooFewToJudge(t *testing.T) {
	for _, samples := range [][]Sample{
		every(50, 5*time.Minute, t0),
		{{T: t0, V: 1}, {T: t0.Add(10 * time.Hour), V: 2}},
	} {
		for _, p := range Downsample(samples, t0, t0.Add(24*time.Hour)) {
			if p.V == nil {
				t.Fatalf("a gap in %v", samples)
			}
		}
	}
}

func TestDownsample_DropsSamplesOutsideTheWindowAndDuplicates(t *testing.T) {
	samples := []Sample{
		{T: t0.Add(-time.Hour), V: 99},
		{T: t0, V: 1},
		{T: t0, V: 2},
		{T: t0.Add(time.Minute), V: 3},
		{T: t0.Add(2 * time.Hour), V: 99},
	}
	pts := Downsample(samples, t0, t0.Add(time.Hour))
	if len(pts) != 2 || *pts[0].V != 2 || *pts[1].V != 3 {
		t.Fatalf("got %+v, want [2 3]: outside samples dropped, duplicate instant keeps the later", pts)
	}
}
