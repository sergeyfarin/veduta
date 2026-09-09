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
