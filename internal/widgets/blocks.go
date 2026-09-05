// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets

import (
	"encoding/json"
	"fmt"
)

// Block is the discriminated union of the nine v1 block types. There is no way to construct an
// arbitrary Block from outside the package - only the nine concrete types below implement it, so
// a Document's blocks are exhaustively one of exactly these, matching the schema's closed `oneOf`.
type Block interface {
	// BlockType returns the JSON "type" discriminator this block serialises as.
	BlockType() string
	isBlock()
}

// BlockMetrics is a grid of small labelled values. 1-12 items.
type BlockMetrics struct {
	Title    string       `json:"title,omitempty"`
	Emphasis Emphasis     `json:"emphasis,omitempty"`
	Columns  int          `json:"columns,omitempty"`
	Items    []MetricItem `json:"items"`
}

// BlockKeyValue is metrics rendered as label/value rows rather than columns - the S4 rule that
// one or two figures read better as rows than as a mostly-empty column grid. 1-24 items.
type BlockKeyValue struct {
	Title    string       `json:"title,omitempty"`
	Emphasis Emphasis     `json:"emphasis,omitempty"`
	Items    []MetricItem `json:"items"`
}

// BlockProgress is a set of labelled progress bars. 1-16 items.
type BlockProgress struct {
	Title    string         `json:"title,omitempty"`
	Emphasis Emphasis       `json:"emphasis,omitempty"`
	Items    []ProgressItem `json:"items"`
}

// BlockStatus is a set of named statuses (e.g. one row per monitored host). 1-24 items.
type BlockStatus struct {
	Title    string       `json:"title,omitempty"`
	Emphasis Emphasis     `json:"emphasis,omitempty"`
	Items    []StatusItem `json:"items"`
}

// BlockList is a vertical list of rows. Items may legally be empty - Empty is then the message
// shown, e.g. "No active streams", which is a state, not a bug. 0-100 items.
type BlockList struct {
	Title    string     `json:"title,omitempty"`
	Emphasis Emphasis   `json:"emphasis,omitempty"`
	Empty    string     `json:"empty,omitempty"`
	Items    []ListItem `json:"items"`
}

// MediaKind selects which of the three media presentations a BlockMedia renders as.
type MediaKind string

// MediaKind values.
const (
	MediaImage      MediaKind = "image"
	MediaImageGrid  MediaKind = "image-grid"
	MediaPosterGrid MediaKind = "poster-grid"
)

// BlockMedia covers image, image-grid and poster-grid - they share one shape and differ only in
// aspect ratio and grid density, both of which the renderer decides from Kind. 0-48 items.
type BlockMedia struct {
	Kind     MediaKind   `json:"-"`
	Title    string      `json:"title,omitempty"`
	Emphasis Emphasis    `json:"emphasis,omitempty"`
	Columns  int         `json:"columns,omitempty"`
	Empty    string      `json:"empty,omitempty"`
	Items    []MediaItem `json:"items"`
}

// TextKind selects plain text or the restricted markdown subset.
type TextKind string

// TextKind values.
const (
	TextPlain    TextKind = "text"
	TextMarkdown TextKind = "markdown"
)

// BlockText covers text and markdown. Markdown is a restricted subset - emphasis, links, inline
// code, lists - and raw HTML is rejected by Validate, not merely discouraged.
type BlockText struct {
	Kind     TextKind `json:"-"`
	Title    string   `json:"title,omitempty"`
	Emphasis Emphasis `json:"emphasis,omitempty"`
	Level    Level    `json:"level,omitempty"`
	Content  string   `json:"content"`
}

// BlockTable is a plain data table. 1-8 columns, 0-100 rows, up to 8 cells per row.
type BlockTable struct {
	Title    string              `json:"title,omitempty"`
	Emphasis Emphasis            `json:"emphasis,omitempty"`
	Columns  []TableColumn       `json:"columns"`
	Rows     []map[string]Scalar `json:"rows"`
}

// BlockActions references 1-6 action ids declared in configuration. A Widget Document can never
// express a command, only which already-declared action a button triggers.
type BlockActions struct {
	Title    string   `json:"title,omitempty"`
	Emphasis Emphasis `json:"emphasis,omitempty"`
	Actions  []Action `json:"actions"`
}

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockMetrics) BlockType() string { return "metrics" }

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockKeyValue) BlockType() string { return "key-value" }

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockProgress) BlockType() string { return "progress" }

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockStatus) BlockType() string { return "status" }

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockList) BlockType() string { return "list" }

// BlockType returns this block's JSON "type" discriminator (image, image-grid or poster-grid).
func (b BlockMedia) BlockType() string { return string(b.Kind) }

// BlockType returns this block's JSON "type" discriminator (text or markdown).
func (b BlockText) BlockType() string { return string(b.Kind) }

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockTable) BlockType() string { return "table" }

// BlockType returns this block's JSON "type" discriminator, satisfying the Block interface.
func (b BlockActions) BlockType() string { return "actions" }

// isBlock is unexported: it exists only to close the Block interface to this package's nine
// concrete types, so a Document's blocks are exhaustively one of exactly these.
func (BlockMetrics) isBlock()  {}
func (BlockKeyValue) isBlock() {}
func (BlockProgress) isBlock() {}
func (BlockStatus) isBlock()   {}
func (BlockList) isBlock()     {}
func (BlockMedia) isBlock()    {}
func (BlockText) isBlock()     {}
func (BlockTable) isBlock()    {}
func (BlockActions) isBlock()  {}

// withType marshals v (via an alias type, to avoid recursing back into MarshalJSON) and splices
// in the "type" discriminator every block variant needs on the wire but none of them stores as
// a Go field - the field is implied by which concrete type it is.
func withType[T any](typ string, v T) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["type"] = json.RawMessage(`"` + typ + `"`)
	return json.Marshal(m)
}

type blockMetricsAlias BlockMetrics

// MarshalJSON adds the "metrics" discriminator this type does not otherwise store as a field.
func (b BlockMetrics) MarshalJSON() ([]byte, error) { return withType("metrics", blockMetricsAlias(b)) }

type blockKeyValueAlias BlockKeyValue

// MarshalJSON adds the "key-value" discriminator this type does not otherwise store as a field.
func (b BlockKeyValue) MarshalJSON() ([]byte, error) {
	return withType("key-value", blockKeyValueAlias(b))
}

type blockProgressAlias BlockProgress

// MarshalJSON adds the "progress" discriminator this type does not otherwise store as a field.
func (b BlockProgress) MarshalJSON() ([]byte, error) {
	return withType("progress", blockProgressAlias(b))
}

type blockStatusAlias BlockStatus

// MarshalJSON adds the "status" discriminator this type does not otherwise store as a field.
func (b BlockStatus) MarshalJSON() ([]byte, error) { return withType("status", blockStatusAlias(b)) }

type blockListAlias BlockList

// MarshalJSON adds the "list" discriminator this type does not otherwise store as a field.
func (b BlockList) MarshalJSON() ([]byte, error) { return withType("list", blockListAlias(b)) }

type blockMediaAlias struct {
	Title    string      `json:"title,omitempty"`
	Emphasis Emphasis    `json:"emphasis,omitempty"`
	Columns  int         `json:"columns,omitempty"`
	Empty    string      `json:"empty,omitempty"`
	Items    []MediaItem `json:"items"`
}

// MarshalJSON adds the "type" discriminator from b.Kind, which is not itself a JSON field.
func (b BlockMedia) MarshalJSON() ([]byte, error) {
	return withType(string(b.Kind), blockMediaAlias{b.Title, b.Emphasis, b.Columns, b.Empty, b.Items})
}

type blockTextAlias struct {
	Title    string   `json:"title,omitempty"`
	Emphasis Emphasis `json:"emphasis,omitempty"`
	Level    Level    `json:"level,omitempty"`
	Content  string   `json:"content"`
}

// MarshalJSON adds the "type" discriminator from b.Kind, which is not itself a JSON field.
func (b BlockText) MarshalJSON() ([]byte, error) {
	return withType(string(b.Kind), blockTextAlias{b.Title, b.Emphasis, b.Level, b.Content})
}

type blockTableAlias BlockTable

// MarshalJSON adds the "table" discriminator this type does not otherwise store as a field.
func (b BlockTable) MarshalJSON() ([]byte, error) { return withType("table", blockTableAlias(b)) }

type blockActionsAlias BlockActions

// MarshalJSON adds the "actions" discriminator this type does not otherwise store as a field.
func (b BlockActions) MarshalJSON() ([]byte, error) {
	return withType("actions", blockActionsAlias(b))
}

// UnmarshalJSON dispatches on the "type" discriminator to decode each element of Document.Blocks
// into its concrete Go type. This is the ONLY place block decoding happens; a type this package
// does not know about becomes an error here, at decode time, rather than a silently-wrong zero
// value discovered later in a renderer.
func (d *Document) UnmarshalJSON(data []byte) error {
	type alias struct {
		SchemaVersion int               `json:"schemaVersion"`
		Title         string            `json:"title,omitempty"`
		Subtitle      string            `json:"subtitle,omitempty"`
		Link          string            `json:"link,omitempty"`
		Status        *Status           `json:"status,omitempty"`
		Blocks        []json.RawMessage `json:"blocks"`
		Signals       map[string]Signal `json:"signals,omitempty"`
		Notices       []Notice          `json:"notices,omitempty"`
		Hints         *Hints            `json:"hints,omitempty"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if a.SchemaVersion != 0 && a.SchemaVersion != 1 {
		return fmt.Errorf("widgets: unsupported schemaVersion %d", a.SchemaVersion)
	}
	blocks := make([]Block, 0, len(a.Blocks))
	for i, raw := range a.Blocks {
		b, err := decodeBlock(raw)
		if err != nil {
			return fmt.Errorf("widgets: blocks[%d]: %w", i, err)
		}
		blocks = append(blocks, b)
	}
	d.Title = a.Title
	d.Subtitle = a.Subtitle
	d.Link = a.Link
	d.Status = a.Status
	d.Blocks = blocks
	d.Signals = a.Signals
	d.Notices = a.Notices
	d.Hints = a.Hints
	return nil
}

// MarshalJSON emits the wire shape, including "schemaVersion": Document has no such Go field -
// this type simply IS v1 - but the schema requires the property, and UnmarshalJSON above checks
// it on the way in. A round-trip test (internal/state) caught the absence of this method: without
// it, every constructed CardState failed schema validation because its embedded document was
// missing schemaVersion entirely.
func (d Document) MarshalJSON() ([]byte, error) {
	type alias struct {
		SchemaVersion int               `json:"schemaVersion"`
		Title         string            `json:"title,omitempty"`
		Subtitle      string            `json:"subtitle,omitempty"`
		Link          string            `json:"link,omitempty"`
		Status        *Status           `json:"status,omitempty"`
		Blocks        []Block           `json:"blocks"`
		Signals       map[string]Signal `json:"signals,omitempty"`
		Notices       []Notice          `json:"notices,omitempty"`
		Hints         *Hints            `json:"hints,omitempty"`
	}
	blocks := d.Blocks
	if blocks == nil {
		blocks = []Block{}
	}
	return json.Marshal(alias{
		SchemaVersion: 1,
		Title:         d.Title,
		Subtitle:      d.Subtitle,
		Link:          d.Link,
		Status:        d.Status,
		Blocks:        blocks,
		Signals:       d.Signals,
		Notices:       d.Notices,
		Hints:         d.Hints,
	})
}

func decodeBlock(raw json.RawMessage) (Block, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case "metrics":
		var v BlockMetrics
		return v, json.Unmarshal(raw, (*blockMetricsAlias)(&v))
	case "key-value":
		var v BlockKeyValue
		return v, json.Unmarshal(raw, (*blockKeyValueAlias)(&v))
	case "progress":
		var v BlockProgress
		return v, json.Unmarshal(raw, (*blockProgressAlias)(&v))
	case "status":
		var v BlockStatus
		return v, json.Unmarshal(raw, (*blockStatusAlias)(&v))
	case "list":
		var v BlockList
		return v, json.Unmarshal(raw, (*blockListAlias)(&v))
	case "image", "image-grid", "poster-grid":
		var a blockMediaAlias
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		return BlockMedia{Kind: MediaKind(head.Type), Title: a.Title, Emphasis: a.Emphasis,
			Columns: a.Columns, Empty: a.Empty, Items: a.Items}, nil
	case "text", "markdown":
		var a blockTextAlias
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		return BlockText{Kind: TextKind(head.Type), Title: a.Title, Emphasis: a.Emphasis,
			Level: a.Level, Content: a.Content}, nil
	case "table":
		var v BlockTable
		return v, json.Unmarshal(raw, (*blockTableAlias)(&v))
	case "actions":
		var v BlockActions
		return v, json.Unmarshal(raw, (*blockActionsAlias)(&v))
	default:
		return nil, fmt.Errorf("unknown block type %q", head.Type)
	}
}
