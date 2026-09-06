// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations_test

import (
	"testing"

	"veduta.dev/veduta/internal/integrations"
)

func TestEvaluate(t *testing.T) {
	m := &integrations.Manifest{Digest: "abc"}
	if got := integrations.Evaluate(m, nil); got != integrations.StatusUnapproved {
		t.Fatalf("got %v, want unapproved", got)
	}
	if got := integrations.Evaluate(m, &integrations.LockEntry{ManifestSHA256: "abc"}); got != integrations.StatusApproved {
		t.Fatalf("got %v, want approved", got)
	}
	if got := integrations.Evaluate(m, &integrations.LockEntry{ManifestSHA256: "different"}); got != integrations.StatusChanged {
		t.Fatalf("got %v, want changed", got)
	}
}
