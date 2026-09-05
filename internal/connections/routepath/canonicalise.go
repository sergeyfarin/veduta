// SPDX-License-Identifier: AGPL-3.0-or-later

// Package routepath implements milestone D1b: the one route-canonicalisation and glob-matching
// routine every authority check in this project shares - manifest load, lock load,
// runtime http.request, asset mint, asset serve and connection allowedPaths. See
// docs/01-architecture.md's "Route canonicalisation — one routine, six call sites" and decision
// D20. Encoding mismatches between the check and the use are the classic way route allowlists
// fail; having exactly one implementation is the whole point of this package existing at all.
package routepath

import (
	"errors"
	"fmt"
	"strings"
)

// Errors Canonicalise returns. Each is distinct so a caller (or a test) can tell exactly which
// rule rejected an input, without parsing a message string.
var (
	ErrNotAbsolute      = errors.New("routepath: path must begin with /")
	ErrBackslash        = errors.New("routepath: path contains a backslash")
	ErrControlOrSpace   = errors.New("routepath: path contains a control character or whitespace")
	ErrMalformedEscape  = errors.New("routepath: path contains a malformed percent-escape")
	ErrEncodedSeparator = errors.New("routepath: a percent-escape decodes to / or \\")
	ErrDotSegment       = errors.New("routepath: a segment is . or .. (before or after decoding)")
	ErrEmptySegment     = errors.New("routepath: path contains an empty segment (//)")
)

// Canonicalise implements the normative algorithm exactly:
//
//	reject if: it does not begin with "/"
//	           it contains a backslash, a control character, or whitespace
//	           it contains a malformed percent escape
//	           percent-decoding would yield "/" or "\" (%2f, %5c, and their mixed-case forms)
//	           any segment is "." or ".." before OR after decoding
//	           it contains an empty segment ("//")
//	then:      decode the remaining unreserved escapes exactly once, lowercase the
//	           percent-hex digits, and re-encode to a single normal form
//
// Canonicalise(Canonicalise(x)) == Canonicalise(x) for any x that passes the first call - the
// output only ever contains literal unreserved characters and lowercase-hex escapes of
// characters that are not unreserved, and re-running the same decode/re-encode rules over that
// output reproduces it unchanged. Decoding never changes the number of segments: no escape this
// function accepts can decode to '/' (that is exactly ErrEncodedSeparator's job), so the split
// on literal '/' before and after normalisation always has the same length.
func Canonicalise(raw string) (string, error) {
	if !strings.HasPrefix(raw, "/") {
		return "", ErrNotAbsolute
	}
	if strings.ContainsRune(raw, '\\') {
		return "", ErrBackslash
	}
	// Byte-wise, deliberately: this rejects every ASCII control character and the space
	// character (0x00-0x20, 0x7f) without decoding anything, and does not require raw to be
	// valid UTF-8 to reach a correct answer either way - a literal non-ASCII byte (valid UTF-8
	// or not) is not on the reject list the architecture doc gives, so it is left alone here.
	for i := 0; i < len(raw); i++ {
		if raw[i] <= 0x20 || raw[i] == 0x7f {
			return "", ErrControlOrSpace
		}
	}
	if err := checkEscapesWellFormed(raw); err != nil {
		return "", err
	}
	if raw == "/" {
		return "/", nil // the bare root: zero segments, not one empty segment
	}

	segments := strings.Split(raw[1:], "/")
	out := make([]string, len(segments))
	for i, seg := range segments {
		normalised, err := canonicaliseSegment(seg)
		if err != nil {
			return "", err
		}
		out[i] = normalised
	}
	return "/" + strings.Join(out, "/"), nil
}

// checkEscapesWellFormed rejects a '%' not followed by exactly two hex digits, and any escape
// whose decoded byte is '/' or '\' - checked over the whole raw string up front, once, so every
// later step can assume there is no encoded separator left to trip over.
func checkEscapesWellFormed(raw string) error {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			continue
		}
		if i+2 >= len(raw) || !isHex(raw[i+1]) || !isHex(raw[i+2]) {
			return ErrMalformedEscape
		}
		b := hexByte(raw[i+1], raw[i+2])
		if b == '/' || b == '\\' {
			return ErrEncodedSeparator
		}
		i += 2
	}
	return nil
}

// canonicaliseSegment validates and normalises one '/'-delimited segment (never containing '/'
// or '\' itself, per checkEscapesWellFormed and the ErrBackslash check above).
func canonicaliseSegment(seg string) (string, error) {
	if seg == "" {
		return "", ErrEmptySegment
	}
	if seg == "." || seg == ".." {
		return "", ErrDotSegment
	}

	var decodedForDotCheck strings.Builder
	var normalised strings.Builder
	for i := 0; i < len(seg); i++ {
		if seg[i] != '%' {
			decodedForDotCheck.WriteByte(seg[i])
			normalised.WriteByte(seg[i])
			continue
		}
		b := hexByte(seg[i+1], seg[i+2])
		decodedForDotCheck.WriteByte(b)
		if isUnreserved(b) {
			normalised.WriteByte(b)
		} else {
			fmt.Fprintf(&normalised, "%%%02x", b)
		}
		i += 2
	}

	if d := decodedForDotCheck.String(); d == "." || d == ".." {
		return "", ErrDotSegment
	}
	return normalised.String(), nil
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexVal(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	default: // 'A'-'F'
		return b - 'A' + 10
	}
}

func hexByte(hi, lo byte) byte { return hexVal(hi)<<4 | hexVal(lo) }

func isUnreserved(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '.' || b == '_' || b == '~':
		return true
	default:
		return false
	}
}
