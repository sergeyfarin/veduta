// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets_test

import (
	"fmt"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/widgets"
)

func seriesDoc(block string) []byte {
	return []byte(`{"schemaVersion":1,"blocks":[` + block + `]}`)
}

func points(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"t":"2026-09-29T%02d:%02d:00Z","v":%d}`, i/60, i%60, i)
	}
	return b.String()
}

func TestSeriesBlock_Accepted(t *testing.T) {
	doc, err := widgets.Validate(seriesDoc(`{"type":"series","title":"Climate","format":"temperature","min":-10,"max":40,
		"series":[
			{"label":"Inside","points":[{"t":"2026-09-29T00:00:00Z","v":21.5},{"t":"2026-09-29T01:00:00Z","v":21.0}]},
			{"label":"Outside","level":"info","points":[{"t":"2026-09-29T00:00:00Z","v":12},{"t":"2026-09-29T01:00:00Z","v":null},{"t":"2026-09-29T04:00:00+02:00","v":11}]},
			{"label":"Nothing yet","points":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := doc.Blocks[0].(widgets.BlockSeries)
	if !ok {
		t.Fatalf("block decoded as %T", doc.Blocks[0])
	}
	if s.Series[1].Points[1].V != nil {
		t.Errorf("a null reading decoded as %v; it must stay a gap", *s.Series[1].Points[1].V)
	}
	if *s.Min != -10 || *s.Max != 40 {
		t.Errorf("min/max = %v/%v", *s.Min, *s.Max)
	}
}

// Every bound the renderer relies on - how many lines, how many points, a line being a function of
// time, a usable axis - is enforced before a document is stored, not trusted in the component.
func TestSeriesBlock_Rejected(t *testing.T) {
	line := func(pts string) string { return `{"label":"x","points":[` + pts + `]}` }
	cases := map[string]string{
		"no series":            `{"type":"series","series":[]}`,
		"five series":          `{"type":"series","series":[` + strings.Repeat(line(``)+",", 4) + line(``) + `]}`,
		"289 points":           `{"type":"series","series":[` + line(points(289)) + `]}`,
		"string value":         `{"type":"series","series":[` + line(`{"t":"2026-09-29T00:00:00Z","v":"12"}`) + `]}`,
		"missing value":        `{"type":"series","series":[` + line(`{"t":"2026-09-29T00:00:00Z"}`) + `]}`,
		"impossible timestamp": `{"type":"series","series":[` + line(`{"t":"2026-13-40T25:00:00Z","v":1}`) + `]}`,
		"date only":            `{"type":"series","series":[` + line(`{"t":"2026-09-29","v":1}`) + `]}`,
		"duplicate time":       `{"type":"series","series":[` + line(`{"t":"2026-09-29T00:00:00Z","v":1},{"t":"2026-09-29T00:00:00Z","v":2}`) + `]}`,
		// The same instant written in two zones is still the same instant.
		"same instant, other zone": `{"type":"series","series":[` + line(`{"t":"2026-09-29T02:00:00+02:00","v":1},{"t":"2026-09-29T00:00:00Z","v":2}`) + `]}`,
		"backwards":                `{"type":"series","series":[` + line(`{"t":"2026-09-29T01:00:00Z","v":1},{"t":"2026-09-29T00:00:00Z","v":2}`) + `]}`,
		"min equals max":           `{"type":"series","min":5,"max":5,"series":[` + line(``) + `]}`,
		"min above max":            `{"type":"series","min":9,"max":5,"series":[` + line(``) + `]}`,
		"unknown property":         `{"type":"series","style":"area","series":[` + line(``) + `]}`,
		"point property":           `{"type":"series","series":[` + line(`{"t":"2026-09-29T00:00:00Z","v":1,"stroke":"red"}`) + `]}`,
		"long unit":                `{"type":"series","unit":"` + strings.Repeat("u", 17) + `","series":[` + line(``) + `]}`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := widgets.Validate(seriesDoc(block)); err == nil {
				t.Errorf("accepted %s", block)
			}
		})
	}
	if _, err := widgets.Validate(seriesDoc(`{"type":"series","series":[` + line(points(288)) + `]}`)); err != nil {
		t.Errorf("288 points, the documented maximum, was refused: %v", err)
	}
}
