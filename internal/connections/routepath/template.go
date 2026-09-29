// SPDX-License-Identifier: AGPL-3.0-or-later

package routepath

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Errors a path template can fail with. Distinct values so the loader, the contract suite and the
// runtime report the same rule the same way.
var (
	ErrTemplateBrace     = errors.New("routepath: a { or } must enclose a whole segment, as {name}")
	ErrPlaceholderName   = errors.New("routepath: a placeholder name must match ^[A-Za-z][A-Za-z0-9_]{0,31}$")
	ErrPlaceholderValue  = errors.New("routepath: a placeholder value must be 1-128 characters from A-Z a-z 0-9 - . _ ~ : @, and not a dot segment")
	ErrPlaceholderAbsent = errors.New("routepath: no value for a placeholder")
)

var placeholderName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

// Template is a request path in which whole segments may be named placeholders -
// "/api/environments/{environmentId}/containers" - so that a card parameter can select an upstream
// object without making the path an expression.
//
// Why a template and not an expression: a route is approved as a glob, and the loader has to be
// able to prove, before anything runs, that every request an operation can make falls inside one.
// An expression can build any string, so that proof is impossible and the check degrades to "the
// broker will refuse it at runtime", which finds a manifest bug only when a card fails. A
// placeholder fills exactly one segment, and a value is refused unless it can only ever be one
// segment - no separator, no escape, no "?" or "#", no dot segment - so a template is covered by a
// route exactly when each placeholder lands on a segment that is "*" in the route's glob. That is
// decidable, and it is checked when the manifest loads.
//
// A value is restricted rather than escaped on purpose. Percent-encoding would make "a/b" one
// segment on the wire, and then the upstream's own router decides what it means - some decode
// %2F before routing. Refusing it keeps the meaning of an approved route in the approver's hands.
type Template struct {
	raw      string
	segments []templateSegment
}

type templateSegment struct {
	literal     string // canonical literal segment, when name is empty
	placeholder string
}

// ParseTemplate parses and validates a request path template. A path with no placeholders is a
// valid template whose Expand returns its canonical form, so every pipeline path can go through
// this one parser.
func ParseTemplate(raw string) (Template, error) {
	if raw == "/" {
		if _, err := Canonicalise(raw); err != nil {
			return Template{}, err
		}
		return Template{raw: raw}, nil
	}
	// Validate everything a template shares with a plain path by canonicalising it with each
	// placeholder replaced by a stand-in segment. Canonicalise's rules then hold for the literal
	// parts, and the segment count is fixed before any value is known.
	if !strings.HasPrefix(raw, "/") {
		return Template{}, ErrNotAbsolute
	}
	parts := strings.Split(raw[1:], "/")
	t := Template{raw: raw, segments: make([]templateSegment, len(parts))}
	standIn := make([]string, len(parts))
	for i, part := range parts {
		if strings.ContainsAny(part, "{}") {
			if len(part) < 3 || part[0] != '{' || part[len(part)-1] != '}' || strings.ContainsAny(part[1:len(part)-1], "{}") {
				return Template{}, fmt.Errorf("%w: segment %q", ErrTemplateBrace, part)
			}
			name := part[1 : len(part)-1]
			if !placeholderName.MatchString(name) {
				return Template{}, fmt.Errorf("%w: %q", ErrPlaceholderName, name)
			}
			t.segments[i] = templateSegment{placeholder: name}
			standIn[i] = "x"
			continue
		}
		standIn[i] = part
	}
	canonical, err := Canonicalise("/" + strings.Join(standIn, "/"))
	if err != nil {
		return Template{}, err
	}
	for i, seg := range strings.Split(canonical[1:], "/") {
		if t.segments[i].placeholder == "" {
			t.segments[i].literal = seg
		}
	}
	return t, nil
}

// Placeholders returns the placeholder names in path order, repeats included.
func (t Template) Placeholders() []string {
	var names []string
	for _, s := range t.segments {
		if s.placeholder != "" {
			names = append(names, s.placeholder)
		}
	}
	return names
}

// String returns the template as written.
func (t Template) String() string { return t.raw }

// CoveredBy reports whether every path this template can expand to matches pattern, a route glob.
// A literal segment must match the glob's segment as an ordinary path would; a placeholder
// segment is covered only by a glob segment that is exactly "*", since a value may be any
// single segment and a narrower glob such as "v*" would admit only some of them. pattern is
// canonicalised here, so callers pass it as declared.
func (t Template) CoveredBy(pattern string) bool {
	canonical, err := Canonicalise(pattern)
	if err != nil {
		return false
	}
	if len(t.segments) == 0 {
		return canonical == "/"
	}
	globSegments := strings.Split(strings.TrimPrefix(canonical, "/"), "/")
	if canonical == "/" || len(globSegments) != len(t.segments) {
		return false
	}
	for i, s := range t.segments {
		if s.placeholder != "" {
			if globSegments[i] != "*" {
				return false
			}
			continue
		}
		if !matchSegment(globSegments[i], s.literal) {
			return false
		}
	}
	return true
}

// Expand substitutes values into the template and returns the canonical request path. Every
// placeholder must have a value, and every value must satisfy ValidPlaceholderValue.
func (t Template) Expand(values map[string]string) (string, error) {
	if len(t.segments) == 0 {
		return "/", nil
	}
	out := make([]string, len(t.segments))
	for i, s := range t.segments {
		if s.placeholder == "" {
			out[i] = s.literal
			continue
		}
		v, ok := values[s.placeholder]
		if !ok {
			return "", fmt.Errorf("%w {%s}", ErrPlaceholderAbsent, s.placeholder)
		}
		if !ValidPlaceholderValue(v) {
			return "", fmt.Errorf("{%s} = %q: %w", s.placeholder, v, ErrPlaceholderValue)
		}
		out[i] = v
	}
	// The value set cannot produce anything Canonicalise rejects or rewrites, but the result is
	// canonicalised anyway: this is the path the broker will authorise, and it costs nothing to
	// have the one routine confirm it rather than trust the reasoning above.
	return Canonicalise("/" + strings.Join(out, "/"))
}

// ValidPlaceholderValue reports whether v can fill one path segment with no escaping and no
// possible second reading: 1 to 128 characters, each unreserved (RFC 3986) or ':' or '@', and
// not the dot segments "." or "..".
func ValidPlaceholderValue(v string) bool {
	if v == "" || len(v) > 128 || v == "." || v == ".." {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !isUnreserved(v[i]) && v[i] != ':' && v[i] != '@' {
			return false
		}
	}
	return true
}
