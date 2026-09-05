// SPDX-License-Identifier: AGPL-3.0-or-later

package fixtures

import (
	"strings"
	"testing"
	"testing/fstest"

	"veduta.dev/veduta/internal/widgets"
)

// blockType and imageRefs exist only to let the tests below walk a widgets.Block generically;
// the closed Block interface intentionally has no such accessors on the production type itself.
func blockType(b widgets.Block) string {
	switch v := b.(type) {
	case widgets.BlockMetrics:
		return "metrics"
	case widgets.BlockKeyValue:
		return "key-value"
	case widgets.BlockProgress:
		return "progress"
	case widgets.BlockStatus:
		return "status"
	case widgets.BlockList:
		return "list"
	case widgets.BlockMedia:
		return string(v.Kind)
	case widgets.BlockText:
		return string(v.Kind)
	case widgets.BlockTable:
		return "table"
	case widgets.BlockActions:
		return "actions"
	default:
		return "unknown"
	}
}

func imageRefs(b widgets.Block) []string {
	media, ok := b.(widgets.BlockMedia)
	if !ok {
		return nil
	}
	refs := make([]string, 0, len(media.Items))
	for _, item := range media.Items {
		refs = append(refs, item.Image.Ref)
	}
	return refs
}

// minimalValid is the smallest fixture that passes loadFrom: one card with a descriptor and a
// pending state, no document, no images. Every mutation test below starts from this and breaks
// exactly one thing, so a failure proves the check that's supposed to catch it actually does.
func minimalValid() fstest.MapFS {
	return fstest.MapFS{
		"showcase.json": &fstest.MapFile{Data: []byte(`{
			"dashboard": {"sections": [{"title": "Home", "cards": [{"id": "a", "title": "A"}]}]},
			"cards": [{"cardId": "a", "document": null, "execution": {"state": "pending"}}]
		}`)},
		"images/.keep": &fstest.MapFile{Data: []byte{}},
	}
}

func TestLoad_RealShowcaseIsValid(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(b.Cards) != 13 {
		t.Fatalf("got %d cards, want 13", len(b.Cards))
	}
	if len(b.Dashboard.Sections) == 0 {
		t.Fatal("no sections")
	}

	seenTypes := map[string]bool{}
	for _, cs := range b.Cards {
		if cs.Document == nil {
			continue
		}
		for _, blk := range cs.Document.Blocks {
			seenTypes[blockType(blk)] = true
		}
	}
	wantTypes := []string{
		"poster-grid", "progress", "metrics", "key-value", "list", "status",
		"markdown", "image-grid", "table", "actions",
	}
	for _, want := range wantTypes {
		if !seenTypes[want] {
			t.Errorf("showcase.json exercises no %q block", want)
		}
	}
}

func TestLoad_EveryImageRefResolves(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, cs := range b.Cards {
		if cs.Document == nil {
			continue
		}
		for _, blk := range cs.Document.Blocks {
			for _, ref := range imageRefs(blk) {
				if _, ok := b.Image(ref); !ok {
					t.Errorf("card %q: block %s: ref %q has no checked-in image",
						cs.CardID, blockType(blk), ref)
				}
			}
		}
	}
}

func TestLoadFrom_MinimalFixtureIsValid(t *testing.T) {
	if _, err := loadFrom(minimalValid()); err != nil {
		t.Fatalf("minimal fixture should load: %v", err)
	}
}

func TestLoadFrom_RejectsDescriptorWithoutState(t *testing.T) {
	fsys := minimalValid()
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}, {"id": "orphan", "title": "B"}]}]},
		"cards": [{"cardId": "a", "document": null, "execution": {"state": "pending"}}]
	}`)}
	_, err := loadFrom(fsys)
	if err == nil || !strings.Contains(err.Error(), "orphan") {
		t.Fatalf("want an error naming the orphan descriptor, got %v", err)
	}
}

func TestLoadFrom_RejectsStateWithoutDescriptor(t *testing.T) {
	fsys := minimalValid()
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}]}]},
		"cards": [
			{"cardId": "a", "document": null, "execution": {"state": "pending"}},
			{"cardId": "orphan", "document": null, "execution": {"state": "pending"}}
		]
	}`)}
	_, err := loadFrom(fsys)
	if err == nil || !strings.Contains(err.Error(), "orphan") {
		t.Fatalf("want an error naming the orphan card state, got %v", err)
	}
}

func TestLoadFrom_RejectsDuplicateDescriptorID(t *testing.T) {
	fsys := minimalValid()
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}, {"id": "a", "title": "B"}]}]},
		"cards": [{"cardId": "a", "document": null, "execution": {"state": "pending"}}]
	}`)}
	if _, err := loadFrom(fsys); err == nil {
		t.Fatal("want an error for a duplicate descriptor id")
	}
}

func TestLoadFrom_RejectsDuplicateCardState(t *testing.T) {
	fsys := minimalValid()
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}]}]},
		"cards": [
			{"cardId": "a", "document": null, "execution": {"state": "pending"}},
			{"cardId": "a", "document": null, "execution": {"state": "pending"}}
		]
	}`)}
	if _, err := loadFrom(fsys); err == nil {
		t.Fatal("want an error for a duplicate card state")
	}
}

func TestLoadFrom_RejectsIllegalExecutionCombination(t *testing.T) {
	fsys := minimalValid()
	// state ok requires a document; this one has none, which state.CardState.Validate rejects.
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}]}]},
		"cards": [{"cardId": "a", "document": null, "execution": {"state": "ok"}}]
	}`)}
	if _, err := loadFrom(fsys); err == nil {
		t.Fatal("want an error for an ok state with no document")
	}
}

func TestLoadFrom_RejectsDocumentThatFailsSchemaValidation(t *testing.T) {
	fsys := minimalValid()
	// Raw HTML inside markdown is structurally valid JSON and decodes without error, but
	// widgets.Validate rejects it - exactly the kind of check JSON Schema alone cannot express,
	// and exactly what a fixture loader that only decoded (never re-validated) would miss.
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}]}]},
		"cards": [{"cardId": "a", "document": {
			"schemaVersion": 1,
			"blocks": [{"type": "markdown", "content": "<script>alert(1)</script>"}]
		}, "execution": {
			"state": "ok", "generatedAt": "2026-01-01T00:00:00Z",
			"ttlSeconds": 60, "expiresAt": "2026-01-01T00:01:00Z"
		}}]
	}`)}
	_, err := loadFrom(fsys)
	if err == nil || !strings.Contains(err.Error(), "document") {
		t.Fatalf("want a document validation error, got %v", err)
	}
}

func TestLoadFrom_RejectsUnknownField(t *testing.T) {
	fsys := minimalValid()
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}]}]},
		"cards": [{"cardId": "a", "document": null, "execution": {"state": "pending"}}],
		"unknownTopLevelField": true
	}`)}
	if _, err := loadFrom(fsys); err == nil {
		t.Fatal("want an error for an unknown top-level field")
	}
}

func TestLoadFrom_RejectsMissingImage(t *testing.T) {
	fsys := minimalValid()
	fsys["showcase.json"] = &fstest.MapFile{Data: []byte(`{
		"dashboard": {"sections": [{"cards": [{"id": "a", "title": "A"}]}]},
		"cards": [{"cardId": "a", "document": {
			"schemaVersion": 1, "blocks": [{"type": "image", "items": [
				{"id": "x", "image": {"ref": "v1.missing.ref"}}
			]}]
		}, "execution": {
			"state": "ok", "generatedAt": "2026-01-01T00:00:00Z",
			"ttlSeconds": 60, "expiresAt": "2026-01-01T00:01:00Z"
		}}]
	}`)}
	b, err := loadFrom(fsys)
	if err != nil {
		t.Fatalf("loadFrom should not itself fail on a dangling image ref: %v", err)
	}
	if _, ok := b.Image("v1.missing.ref"); ok {
		t.Fatal("should not have found an image for a ref no fixture declares")
	}
}

func TestBundle_CardByID(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := b.CardByID("does-not-exist"); ok {
		t.Fatal("found a card that should not exist")
	}
	cs, ok := b.CardByID("frigate")
	if !ok || cs.CardID != "frigate" {
		t.Fatalf("CardByID(frigate) = %+v, %v", cs, ok)
	}
}
