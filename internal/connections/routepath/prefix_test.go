// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath_test

import (
	"testing"

	"veduta.dev/veduta/internal/connections/routepath"
)

func TestHasPathPrefix(t *testing.T) {
	cases := []struct {
		path, prefix string
		want         bool
	}{
		{"/api", "/api", true},
		{"/api/x", "/api", true},
		{"/api/x/y", "/api", true},
		{"/apievil", "/api", false}, // the exact bug: a naive strings.HasPrefix would say true
		{"/apievil/x", "/api", false},
		{"/other", "/api", false},
		{"/api", "/", true},
		{"/", "/", true},
		{"/anything/at/all", "/", true},
		{"/api/", "/api", true}, // trailing slash on the path side is still the same subtree
	}
	for _, c := range cases {
		if got := routepath.HasPathPrefix(c.path, c.prefix); got != c.want {
			t.Errorf("HasPathPrefix(%q, %q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}
