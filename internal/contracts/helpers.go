// SPDX-License-Identifier: AGPL-3.0-or-later

// Package contracts holds the cross-document checks that neither JSON Schema nor a regular
// expression can express, plus the small helpers those checks delegate to.
//
// These are the same checks the configuration loader must perform (milestones C1, D2b, D3), so
// they live in internal/ as ordinary code rather than in a throwaway script. The contract tests
// drive them against the checked-in examples and against the negative fixtures in
// testdata/semantic-cases, so deleting a check fails the build.
package contracts

import (
	"fmt"
	"mime"
	"sort"
	"strings"
)

// NormaliseMediaType canonicalises a Content-Type for route comparison. Splitting on ";" is
// wrong for quoted parameter values such as profile="a;b", so this goes through
// mime.ParseMediaType and mime.FormatMediaType: type and subtype and parameter names are
// lowercased, the charset value is lowercased, parameters are sorted, and values that need
// quoting keep their quotes. ALL parameters are retained - dropping them would collapse
// application/vnd.api+json;profile=... into its base type.
func NormaliseMediaType(v string) (string, error) {
	mt, params, err := mime.ParseMediaType(v)
	if err != nil {
		return "", err
	}
	if cs, ok := params["charset"]; ok {
		params["charset"] = strings.ToLower(cs)
	}
	out := mime.FormatMediaType(strings.ToLower(mt), params)
	if out == "" {
		return "", fmt.Errorf("cannot format media type %q", v)
	}
	return out, nil
}

// ValidSemver implements SemVer 2.0.0 exactly: no leading zeroes in numeric identifiers, no
// empty identifiers. The schema pattern is a pre-filter for editor feedback; this is the check
// that decides.
func ValidSemver(v string) bool {
	core, pre, build := v, "", ""
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core, build = core[:i], core[i+1:]
	}
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core, pre = core[:i], core[i+1:]
	}
	nums := strings.Split(core, ".")
	if len(nums) != 3 {
		return false
	}
	for _, n := range nums {
		if !numericIdent(n) {
			return false
		}
	}
	if strings.Contains(v, "-") && pre == "" {
		return false
	}
	for _, id := range splitNonEmpty(pre) {
		if id == "" || !alphanumericIdent(id) {
			return false
		}
		if isAllDigits(id) && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	if strings.Contains(v, "+") && build == "" {
		return false
	}
	for _, id := range splitNonEmpty(build) {
		if id == "" || !alphanumericIdent(id) {
			return false
		}
	}
	return true
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ".")
}

func numericIdent(n string) bool {
	if n == "" || (len(n) > 1 && n[0] == '0') {
		return false
	}
	return isAllDigits(n)
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func alphanumericIdent(s string) bool {
	for _, c := range s {
		digit := c >= '0' && c <= '9'
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !digit && !letter && c != '-' {
			return false
		}
	}
	return true
}

// sortedKeys is a small convenience used when reporting deterministic diagnostics.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
