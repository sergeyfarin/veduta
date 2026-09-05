// SPDX-License-Identifier: AGPL-3.0-or-later

package config_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
)

// TestLoad_500CardsUnder50ms is milestone C1's own stated performance AC. Timed three times and
// the minimum kept, since a single wall-clock sample on a shared/CI machine is noisy in ways that
// have nothing to do with this package's own cost - a cold GC or a neighbouring process stealing
// a scheduling slice does not mean the loader regressed.
func TestLoad_500CardsUnder50ms(t *testing.T) {
	if raceEnabled {
		t.Skip("skipped under -race: instrumentation overhead swamps the budget being measured")
	}
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", generate500CardConfig())

	best := time.Duration(1<<63 - 1)
	for i := 0; i < 3; i++ {
		start := time.Now()
		snap, diags := config.Load(path)
		elapsed := time.Since(start)
		if diags.HasErrors() {
			t.Fatalf("unexpected errors: %s", diags)
		}
		if snap == nil {
			t.Fatal("nil snapshot on success")
		}
		if elapsed < best {
			best = elapsed
		}
	}
	t.Logf("best of 3: %s", best)
	if best > 50*time.Millisecond {
		t.Errorf("Load took %s for 500 cards, want under 50ms", best)
	}
}

func generate500CardConfig() string {
	var b strings.Builder
	b.WriteString("version: 1\nauth:\n  mode: none\nintegrations:\n  - id: http-json\n    source: builtin\nsections:\n  - title: Generated\n    cards:\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "      - id: card-%d\n        integration: http-json\n        operation: request\n        params: { path: /x }\n        span: { columns: 1, rows: 1 }\n", i)
	}
	return b.String()
}
