// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"fmt"
	"sort"
	"time"

	"veduta.dev/veduta/internal/widgets"
)

// MaxHistoryPoints is the most points one signal's history carries, matching a series line's
// schema maximum: a day at five-minute resolution.
const MaxHistoryPoints = 288

// maxHistorySignals matches card-state.v1's history maxProperties.
const maxHistorySignals = 16

// gapFactor decides what counts as missing data. A spacing between two readings more than this
// many times the series' median spacing is a gap - the card was failing, disabled, or the server
// was down - and is drawn as one, rather than as a straight line claiming a trend nobody measured.
// Relative to the median rather than to the card's refresh interval, because the retained record
// is what it is: a refresh interval changed last week would otherwise mark every older point a gap.
const gapFactor = 3

// Sample is one retained reading.
type Sample struct {
	T time.Time
	V float64
}

// Downsample turns the samples inside [from, to] into at most MaxHistoryPoints points: unchanged
// when they fit, otherwise the mean of each of MaxHistoryPoints equal time buckets, stamped with
// the time of the bucket's last sample so every timestamp is one that was really recorded. Long
// silences become a null point halfway across them. samples need not be sorted.
func Downsample(samples []Sample, from, to time.Time) []widgets.SeriesPoint {
	in := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if !s.T.Before(from) && !s.T.After(to) {
			in = append(in, s)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].T.Before(in[j].T) })
	// Two samples at the same instant would break "strictly increasing"; keep the later-written.
	dedup := in[:0]
	for _, s := range in {
		if n := len(dedup); n > 0 && dedup[n-1].T.Equal(s.T) {
			dedup[n-1] = s
			continue
		}
		dedup = append(dedup, s)
	}
	in = dedup

	// Leave room for the gap markers inserted below, so the result still fits the schema.
	budget := MaxHistoryPoints / 2
	if len(in) > budget {
		width := to.Sub(from) / time.Duration(budget)
		if width <= 0 {
			width = 1
		}
		var out []Sample
		var sum float64
		var n int
		bucket := int64(-1)
		var last time.Time
		flush := func() {
			if n > 0 {
				out = append(out, Sample{T: last, V: sum / float64(n)})
			}
			sum, n = 0, 0
		}
		for _, s := range in {
			b := int64(s.T.Sub(from) / width)
			if b != bucket {
				flush()
				bucket = b
			}
			sum += s.V
			n++
			last = s.T
		}
		flush()
		in = out
	}

	gap := medianSpacing(in) * gapFactor
	points := make([]widgets.SeriesPoint, 0, len(in)+len(in)/2)
	for i, s := range in {
		if i > 0 && gap > 0 && s.T.Sub(in[i-1].T) > gap && len(points) < MaxHistoryPoints-1 {
			mid := in[i-1].T.Add(s.T.Sub(in[i-1].T) / 2)
			points = append(points, widgets.SeriesPoint{T: mid.UTC().Format(time.RFC3339Nano)})
		}
		if len(points) == MaxHistoryPoints {
			break
		}
		v := s.V
		points = append(points, widgets.SeriesPoint{T: s.T.UTC().Format(time.RFC3339Nano), V: &v})
	}
	return points
}

// medianSpacing is the median time between consecutive samples, or 0 with fewer than three
// samples - too few to say what "usual" is, so nothing is called a gap.
func medianSpacing(in []Sample) time.Duration {
	if len(in) < 3 {
		return 0
	}
	d := make([]time.Duration, len(in)-1)
	for i := 1; i < len(in); i++ {
		d[i-1] = in[i].T.Sub(in[i-1].T)
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2]
}

// validateHistory keeps a stored or served card state inside card-state.v1's history bounds.
func validateHistory(h map[string][]widgets.SeriesPoint) error {
	if len(h) > maxHistorySignals {
		return fmt.Errorf("history: %d signals, more than %d", len(h), maxHistorySignals)
	}
	for name, points := range h {
		if len(points) > MaxHistoryPoints {
			return fmt.Errorf("history %s: %d points, more than %d", name, len(points), MaxHistoryPoints)
		}
	}
	return nil
}
