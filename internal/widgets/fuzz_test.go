// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets_test

import (
	"encoding/json"
	"testing"

	"veduta.dev/veduta/internal/widgets"
)

func FuzzValidate(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"blocks":[{"type":"text","content":"hello"}]}`))
	f.Add([]byte(`{"schemaVersion":1,"blocks":[{"type":"markdown","content":"<script>x</script>"}]}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"schemaVersion":1,"blocks":[{"type":"series","series":[{"label":"a","points":[{"t":"2026-09-29T00:00:00Z","v":1},{"t":"2026-09-29T00:05:00Z","v":null}]}]}]}`))
	f.Add([]byte(`{"schemaVersion":1,"blocks":[{"type":"series","min":1,"max":0,"series":[{"label":"a","points":[{"t":"2026-09-29T00:05:00Z","v":1},{"t":"2026-09-29T00:00:00Z","v":2}]}]}]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		document, err := widgets.Validate(raw)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("accepted document cannot be marshalled: %v", err)
		}
		if _, err = widgets.Validate(encoded); err != nil {
			t.Fatalf("accepted document fails after typed round trip: %v", err)
		}
	})
}
