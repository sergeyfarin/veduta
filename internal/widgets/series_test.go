// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func TestSeriesBlock_HistoryBinding(t *testing.T) {
	bound := func(window, signal string) string {
		w := ""
		if window != "" {
			w = `"window":"` + window + `",`
		}
		return `{"type":"series","history":{` + w + `"lines":[{"signal":"` + signal + `","label":"CPU"}]}}`
	}
	for name, blocks := range map[string]string{
		"bound":                                 bound("24h", "cpu.percent"),
		"default window":                        bound("", "cpu.percent"),
		"same signal, same window twice":        bound("1h", "cpu.percent") + "," + bound("1h", "cpu.percent"),
		"default and explicit 24h are the same": bound("", "cpu.percent") + "," + bound("24h", "cpu.percent"),
	} {
		doc, err := widgets.Validate(seriesDoc(blocks))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := widgets.HistoryBindings(doc)["cpu.percent"]; got == 0 {
			t.Errorf("%s: no binding for cpu.percent", name)
		}
	}
	for name, blocks := range map[string]string{
		"both series and history": `{"type":"series","series":[{"label":"x","points":[]}],"history":{"lines":[{"signal":"cpu","label":"x"}]}}`,
		"neither":                 `{"type":"series"}`,
		"unknown window":          bound("30d", "cpu.percent"),
		"bad signal name":         bound("1h", "CPU Percent"),
		"no lines":                `{"type":"series","history":{"lines":[]}}`,
		"five lines":              `{"type":"series","history":{"lines":[` + strings.TrimSuffix(strings.Repeat(`{"signal":"a","label":"a"},`, 5), ",") + `]}}`,
		// One signal, two windows: the card state has one point set per signal, so this would be
		// two different answers under one key.
		"one signal at two windows": bound("1h", "cpu.percent") + "," + bound("24h", "cpu.percent"),
	} {
		if _, err := widgets.Validate(seriesDoc(blocks)); err == nil {
			t.Errorf("%s: accepted %s", name, blocks)
		}
	}
}

func TestHistoryWindowsMatchSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "schemas", "widget-document.v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	enum := schema.Defs["blockSeries"].Properties["history"].Properties["window"].Enum
	if len(enum) == 0 || len(enum) != len(widgets.HistoryWindows) {
		t.Fatalf("schema windows %v, Go windows %v", enum, widgets.HistoryWindows)
	}
	for _, w := range enum {
		if widgets.HistoryWindows[w] == 0 {
			t.Errorf("schema window %q has no duration in widgets.HistoryWindows", w)
		}
	}
}
