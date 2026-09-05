// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fixtures loads the checked-in showcase dashboard the --fixtures dev flag and the
// visual regression suite serve (milestone B5). It is a deliberately narrow, dev-only substitute
// for the real data path: Phase C adds configuration loading, Phase F the scheduler and real
// integrations, and their output will carry the same GET /dashboard and GET /cards shapes
// documented in docs/01-architecture.md section 9. Until then this package exists so that
// surface can be exercised end-to-end, deterministically, with no network and no clock drift.
package fixtures

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/widgets"
)

//go:embed showcase.json images
var files embed.FS

// Span is a card's footprint in grid units. Mirrors the `span` object in
// schemas/config.v1.schema.json, which is where it belongs once configuration loading exists.
type Span struct {
	Columns int `json:"columns,omitempty"`
	Rows    int `json:"rows,omitempty"`
}

// CardDescriptor is a card's layout metadata: everything GET /dashboard answers with, and
// nothing GET /cards does. Title, icon and href are configuration, never data - a card's health
// and content come only from its CardState.
type CardDescriptor struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Href  string `json:"href,omitempty"`
	Span  Span   `json:"span,omitempty"`
}

// Section groups cards under a heading. Multi-page layout is configuration (Phase C); a fixture
// dashboard needs only a flat list of sections.
type Section struct {
	Title string           `json:"title,omitempty"`
	Cards []CardDescriptor `json:"cards"`
}

// Dashboard is exactly the body of GET /dashboard: pages, sections and card descriptors, no
// data (docs/01-architecture.md section 9).
type Dashboard struct {
	Sections []Section `json:"sections"`
}

// Bundle is everything --fixtures serves: the layout, every card's current envelope, and the
// local, checked-in images its media blocks reference - no network fetch, ever.
type Bundle struct {
	Dashboard Dashboard
	Cards     []state.CardState
	images    map[string][]byte
}

// document is the on-disk shape of showcase.json.
type document struct {
	Dashboard Dashboard         `json:"dashboard"`
	Cards     []state.CardState `json:"cards"`
}

// Load reads and validates the checked-in showcase. It fails loudly rather than serving a
// silently-broken fixture: the whole point of --fixtures is that a screenshot taken against it
// means something, so every document is re-validated against the real widget schema (not just
// decoded), and every descriptor is cross-checked against the card list it describes.
func Load() (Bundle, error) {
	return loadFrom(files)
}

// loadFrom does the real work against an arbitrary fs.FS, so the cross-checks below can be
// mutation-tested against a small in-memory fixture instead of only ever running once against
// the real showcase.json at startup.
func loadFrom(fsys fs.FS) (Bundle, error) {
	raw, err := fs.ReadFile(fsys, "showcase.json")
	if err != nil {
		return Bundle{}, err
	}

	var doc document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if decodeErr := dec.Decode(&doc); decodeErr != nil {
		return Bundle{}, fmt.Errorf("showcase.json: %w", decodeErr)
	}

	descriptorIDs := make(map[string]bool)
	for _, section := range doc.Dashboard.Sections {
		for _, card := range section.Cards {
			if card.ID == "" {
				return Bundle{}, fmt.Errorf("showcase.json: a card descriptor is missing id")
			}
			if descriptorIDs[card.ID] {
				return Bundle{}, fmt.Errorf("showcase.json: duplicate card descriptor %q", card.ID)
			}
			descriptorIDs[card.ID] = true
		}
	}

	cardIDs := make(map[string]bool)
	for i, cs := range doc.Cards {
		if cardIDs[cs.CardID] {
			return Bundle{}, fmt.Errorf("showcase.json: duplicate card state %q", cs.CardID)
		}
		cardIDs[cs.CardID] = true

		if validateErr := cs.Validate(); validateErr != nil {
			return Bundle{}, fmt.Errorf("showcase.json: cards[%d] %q: %w", i, cs.CardID, validateErr)
		}
		if cs.Document != nil {
			docRaw, marshalErr := json.Marshal(cs.Document)
			if marshalErr != nil {
				return Bundle{}, fmt.Errorf("showcase.json: cards[%d] %q: %w", i, cs.CardID, marshalErr)
			}
			if _, validateErr := widgets.Validate(docRaw); validateErr != nil {
				return Bundle{}, fmt.Errorf("showcase.json: cards[%d] %q: document: %w", i, cs.CardID, validateErr)
			}
		}
	}

	for id := range descriptorIDs {
		if !cardIDs[id] {
			return Bundle{}, fmt.Errorf("showcase.json: card %q has a descriptor but no state", id)
		}
	}
	for id := range cardIDs {
		if !descriptorIDs[id] {
			return Bundle{}, fmt.Errorf("showcase.json: card %q has state but no descriptor", id)
		}
	}

	images, err := loadImages(fsys)
	if err != nil {
		return Bundle{}, err
	}

	return Bundle{Dashboard: doc.Dashboard, Cards: doc.Cards, images: images}, nil
}

// loadImages keys every embedded image by the asset ref it stands in for: the file
// "v1.Zml4dHVyZQ.ZHVuZQ.jpg" answers for ref "v1.Zml4dHVyZQ.ZHVuZQ". One filename convention
// keeps the fixture JSON and the image directory in lock-step without a separate manifest.
func loadImages(fsys fs.FS) (map[string][]byte, error) {
	entries, err := fs.ReadDir(fsys, "images")
	if err != nil {
		return nil, err
	}
	images := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, "images/"+e.Name())
		if err != nil {
			return nil, err
		}
		ext := extOf(e.Name())
		ref := strings.TrimSuffix(e.Name(), ext)
		images[ref] = data
	}
	return images, nil
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// Image returns the checked-in bytes for a fixture asset ref, or false if the fixture has none
// for it. Every fixture image is a JPEG; content type is not stored per-file because it does not
// vary.
func (b Bundle) Image(ref string) ([]byte, bool) {
	data, ok := b.images[ref]
	return data, ok
}

// CardByID is a small convenience for tests and the API handlers: O(n) is fine at fixture size.
func (b Bundle) CardByID(id string) (state.CardState, bool) {
	for _, cs := range b.Cards {
		if cs.CardID == id {
			return cs, true
		}
	}
	return state.CardState{}, false
}

// SortedIDs returns every card id the fixture declares, for tests that want a stable order.
func (b Bundle) SortedIDs() []string {
	ids := make([]string, 0, len(b.Cards))
	for _, cs := range b.Cards {
		ids = append(ids, cs.CardID)
	}
	sort.Strings(ids)
	return ids
}
