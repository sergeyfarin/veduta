// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import "testing"

// A manifest may call a core function by name and nothing else: not an unknown name, and not a
// method on a Go value an expression happens to produce, which would open every exported method
// of time.Time (and whatever else a builtin returns) to manifests without anyone choosing that.
func TestExpressionCallsAreLimitedToCoreFunctions(t *testing.T) {
	for _, src := range []string{
		`fromUnix(r.data.result[0].values[0][0])`,
		`string(unix(now() - duration("24h")))`,
		`unix("2026-10-04T00:00:00Z")`,
	} {
		if _, err := compileExprSource(src, 1, 1, 512); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
	for _, src := range []string{
		`now().Unix()`,
		`date(x).Format("2006-01-02T15:04:05Z07:00")`,
		`fromUnix.call(1)`,
		`bogus(1)`,
	} {
		if _, err := compileExprSource(src, 1, 1, 512); err == nil {
			t.Errorf("%s: accepted, want refused", src)
		}
	}
}
