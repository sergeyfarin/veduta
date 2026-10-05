// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"encoding/json"
	"fmt"
	"testing"
)

// AssertPercentsAreFractions fails t for any percent-formatted value in doc outside 0..1. The
// renderer multiplies a percent value by 100 (web/src/lib/format.ts), the same 0-1 scale as a
// progress bar, so a manifest passing an upstream's 0-100 number showed 25% CPU as "2500.0%"
// beside a correctly quarter-full bar (#19). Every value these manifests format as percent is a
// share of a whole, so anything above 1 is that mistake rather than a real reading.
func AssertPercentsAreFractions(t *testing.T, doc any) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	checked := 0
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["format"] == "percent" {
				if n, ok := x["value"].(float64); ok {
					checked++
					if n < 0 || n > 1 {
						t.Errorf("%s: percent value %v is not a 0-1 fraction", path, n)
					}
				}
			}
			for k, c := range x {
				walk(path+"."+k, c)
			}
		case []any:
			for i, c := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), c)
			}
		}
	}
	walk("document", tree)
	if checked == 0 {
		t.Errorf("no percent-formatted values found; the assertion checked nothing")
	}
}
