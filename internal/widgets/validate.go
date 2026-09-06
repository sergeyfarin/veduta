// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"veduta.dev/veduta/schemas"
)

// Hard limits the JSON Schema cannot express by itself: total serialised size (schema has no
// "total bytes" constraint) and raw HTML inside markdown (schema can only bound content LENGTH,
// not its structure). Both are documented as requirements in docs/01-architecture.md section 4
// and enforced here, not merely described.
const (
	maxDocumentBytes = 64 * 1024
	maxMarkdownBytes = 2048 // matches the schema's content maxLength; enforced again defensively
)

var (
	schemaOnce sync.Once
	schemaErr  error
	compiled   *jsonschema.Schema
)

func widgetSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		body, err := schemas.FS.ReadFile("widget-document.v1.schema.json")
		if err != nil {
			schemaErr = fmt.Errorf("widgets: reading embedded schema: %w", err)
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			schemaErr = fmt.Errorf("widgets: parsing embedded schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		const id = "https://veduta.dev/schemas/widget-document.v1.schema.json"
		if err := c.AddResource(id, doc); err != nil {
			schemaErr = fmt.Errorf("widgets: loading embedded schema: %w", err)
			return
		}
		compiled, schemaErr = c.Compile(id)
	})
	return compiled, schemaErr
}

// ValidationError reports every problem found, rather than only the first - an integration
// author fixing a manifest one round-trip at a time is exactly what this project is trying not
// to be like.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return "widgets: invalid document: " + e.Problems[0]
	}
	return fmt.Sprintf("widgets: invalid document (%d problems): %s", len(e.Problems), e.Problems[0])
}

// Validate is the single gate every runtime (builtin, declarative, wasm) passes an integration's
// output through before it is ever stored or rendered. It never returns a Document on error - a
// caller cannot accidentally use a partially-validated value. Enforces the core default byte
// ceiling; a caller with its own approved, possibly narrower or wider `outputKB` (schema range
// 1-256 KiB) should call ValidateWithLimit instead.
func Validate(raw []byte) (Document, error) {
	return ValidateWithLimit(raw, maxDocumentBytes)
}

// ValidateWithLimit is Validate with an explicit byte ceiling in place of the core default -
// found missing in review: a manifest's approved `outputKB` was reconciled into EffectiveLimits
// and carried all the way to the declarative runtime, but Validate only ever checked the
// hardcoded core default, so a narrower approval was never actually tighter and a wider one could
// never take effect. maxBytes <= 0 falls back to the core default, same as every other limit in
// this project treating "unset" as "the documented default," never "unlimited."
func ValidateWithLimit(raw []byte, maxBytes int) (Document, error) {
	if maxBytes <= 0 {
		maxBytes = maxDocumentBytes
	}
	if len(raw) > maxBytes {
		return Document{}, &ValidationError{Problems: []string{
			fmt.Sprintf("document is %d bytes, exceeding the %d byte limit", len(raw), maxBytes),
		}}
	}

	sch, err := widgetSchema()
	if err != nil {
		return Document{}, err
	}

	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return Document{}, &ValidationError{Problems: []string{"not valid JSON: " + err.Error()}}
	}

	var problems []string
	if err := sch.Validate(generic); err != nil {
		problems = append(problems, flattenSchemaError(err)...)
	}

	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		// The schema already rejected anything this decoder cannot handle in practice - reaching
		// here with schema-clean input would mean document.go's UnmarshalJSON disagrees with the
		// schema, which is itself a bug worth surfacing distinctly from an author's mistake.
		problems = append(problems, "internal: schema-valid document failed to decode: "+err.Error())
	} else {
		problems = append(problems, validateSemantics(&doc)...)
	}

	if len(problems) > 0 {
		return Document{}, &ValidationError{Problems: problems}
	}
	return doc, nil
}

// validateSemantics checks everything true structural JSON Schema validation cannot express:
// real timestamp parsing (a pattern accepts 2026-99-99T99:99:99Z; time.Parse does not - see the
// round-6/round-8 review in docs/00), and that markdown contains no raw HTML, which the schema
// can only bound by length, not forbid by content.
func validateSemantics(doc *Document) []string {
	var problems []string
	checkTime := func(field, value string) {
		if value == "" {
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %q is pattern-shaped but not a real timestamp", field, value))
		}
	}
	if doc.Status != nil {
		checkTime("status.since", doc.Status.Since)
	}
	for i, b := range doc.Blocks {
		switch v := b.(type) {
		case BlockStatus:
			for j, it := range v.Items {
				checkTime(fmt.Sprintf("blocks[%d].items[%d].since", i, j), it.Since)
			}
		case BlockList:
			for j, it := range v.Items {
				checkTime(fmt.Sprintf("blocks[%d].items[%d].timestamp", i, j), it.Timestamp)
			}
		case BlockMedia:
			for j, it := range v.Items {
				checkTime(fmt.Sprintf("blocks[%d].items[%d].timestamp", i, j), it.Timestamp)
			}
		case BlockText:
			if v.Kind == TextMarkdown {
				if len(v.Content) > maxMarkdownBytes {
					problems = append(problems, fmt.Sprintf("blocks[%d]: markdown content exceeds %d bytes", i, maxMarkdownBytes))
				}
				if tag := rawHTMLTag.FindString(v.Content); tag != "" {
					problems = append(problems, fmt.Sprintf(
						"blocks[%d]: markdown contains raw HTML (%q); only emphasis, links, inline code and lists are permitted", i, tag))
				}
			}
		}
	}
	return problems
}

// rawHTMLTag matches an HTML tag opener. Markdown is a restricted subset - emphasis, links,
// inline code, lists - specifically so that "raw HTML is rejected" (the schema's own
// description for blockText) is an enforced rule and not just documentation.
var rawHTMLTag = regexp.MustCompile(`<[a-zA-Z][a-zA-Z0-9-]*[\s/>]`)

func flattenSchemaError(err error) []string {
	var ve *jsonschema.ValidationError
	if !asValidationError(err, &ve) {
		return []string{err.Error()}
	}
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "#"
			if len(e.InstanceLocation) > 0 {
				loc = "#/" + joinPath(e.InstanceLocation)
			}
			out = append(out, fmt.Sprintf("%s: %s", loc, e.Error()))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}

func asValidationError(err error, target **jsonschema.ValidationError) bool {
	return errors.As(err, target)
}

func joinPath(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "/"
		}
		out += p
	}
	return out
}
