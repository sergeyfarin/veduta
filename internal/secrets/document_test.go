// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets_test

import (
	"testing"

	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/widgets"
)

const leaked = "leakedcredential123"

func registryWithLeaked() *secrets.Registry {
	reg := secrets.NewRegistry()
	reg.Track(leaked)
	return reg
}

func TestContainsSecretInDocument_Clean(t *testing.T) {
	reg := registryWithLeaked()
	doc := widgets.Document{
		Title: "Jellyfin",
		Blocks: []widgets.Block{
			widgets.BlockMetrics{Items: []widgets.MetricItem{{Label: "Movies", Value: 438}}},
		},
	}
	if reg.ContainsSecretInDocument(doc) {
		t.Fatal("a clean document must not be flagged")
	}
}

func TestContainsSecretInDocument_TitleLeak(t *testing.T) {
	reg := registryWithLeaked()
	doc := widgets.Document{Title: "oops " + leaked}
	if !reg.ContainsSecretInDocument(doc) {
		t.Fatal("want a leak detected in the document title")
	}
}

func TestContainsSecretInDocument_StatusTextLeak(t *testing.T) {
	reg := registryWithLeaked()
	doc := widgets.Document{Status: &widgets.Status{Level: widgets.LevelOK, Text: leaked}}
	if !reg.ContainsSecretInDocument(doc) {
		t.Fatal("want a leak detected in status text")
	}
}

// TestContainsSecretInDocument_EveryBlockType proves the walker actually inspects each of the
// nine v1 block types, not just the ones easiest to test - a block type added to the switch
// later without a case here would otherwise silently stop being covered.
func TestContainsSecretInDocument_EveryBlockType(t *testing.T) {
	cases := map[string]widgets.Block{
		"metrics":      widgets.BlockMetrics{Items: []widgets.MetricItem{{Label: leaked, Value: 1}}},
		"key-value":    widgets.BlockKeyValue{Items: []widgets.MetricItem{{Label: leaked, Value: 1}}},
		"progress":     widgets.BlockProgress{Items: []widgets.ProgressItem{{Label: leaked, Progress: 0.5}}},
		"status":       widgets.BlockStatus{Items: []widgets.StatusItem{{Label: "x", Level: widgets.LevelOK, Text: leaked}}},
		"list":         widgets.BlockList{Items: []widgets.ListItem{{Title: leaked}}},
		"image-grid":   widgets.BlockMedia{Items: []widgets.MediaItem{{Title: leaked}}},
		"text":         widgets.BlockText{Content: leaked},
		"table":        widgets.BlockTable{Rows: []map[string]widgets.Scalar{{"cell": leaked}}},
		"actions":      widgets.BlockActions{Actions: []widgets.Action{{ID: "a", Label: leaked}}},
		"metric val":   widgets.BlockMetrics{Items: []widgets.MetricItem{{Label: "x", Value: leaked}}},
		"list val":     widgets.BlockList{Items: []widgets.ListItem{{Title: "x", Value: leaked}}},
		"progress val": widgets.BlockProgress{Items: []widgets.ProgressItem{{Label: "x", Progress: 0.1, Value: leaked}}},
	}
	reg := registryWithLeaked()
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			doc := widgets.Document{Blocks: []widgets.Block{block}}
			if !reg.ContainsSecretInDocument(doc) {
				t.Errorf("%s: leak not detected", name)
			}
		})
	}
}

// TestContainsSecretInDocument_EnvelopeFields covers the parts of the document that are served
// in GET /api/v1/cards without necessarily being drawn by a component. A value that reaches the
// client is exposed whether or not the SPA renders it, so the walker checks these too.
func TestContainsSecretInDocument_EnvelopeFields(t *testing.T) {
	cases := map[string]widgets.Document{
		"link":          {Link: "https://host/?token=" + leaked},
		"notice":        {Notices: []widgets.Notice{{Level: "warn", Message: "upstream rejected " + leaked}}},
		"signal value":  {Signals: map[string]widgets.Signal{"version": {Value: leaked}}},
		"media alt":     {Blocks: []widgets.Block{widgets.BlockMedia{Items: []widgets.MediaItem{{Image: widgets.Image{Ref: "r", Alt: leaked}}}}}},
		"list item alt": {Blocks: []widgets.Block{widgets.BlockList{Items: []widgets.ListItem{{Title: "x", Image: &widgets.Image{Ref: "r", Alt: leaked}}}}}},
		"table column":  {Blocks: []widgets.Block{widgets.BlockTable{Columns: []widgets.TableColumn{{Key: "k", Label: leaked}}}}},
	}
	reg := registryWithLeaked()
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if !reg.ContainsSecretInDocument(doc) {
				t.Errorf("%s: leak not detected", name)
			}
		})
	}
}

func TestContainsSecretInDocument_NoRegistryMatchesNothing(t *testing.T) {
	reg := secrets.NewRegistry()
	doc := widgets.Document{Title: leaked}
	if reg.ContainsSecretInDocument(doc) {
		t.Fatal("an empty registry should never report a leak")
	}
}
