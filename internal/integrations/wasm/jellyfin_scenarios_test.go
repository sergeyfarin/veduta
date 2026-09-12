// SPDX-License-Identifier: AGPL-3.0-or-later

package wasm_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The golden document covers only the happy path: every fixture item in testdata/recent.json has
// both a primary image and a production year, so the branches that decide whether a declarative
// rewrite is feasible were never exercised. These scenarios pin the plugin's actual behaviour on
// the shapes a real library produces - a poster-less item, an item with no year, a server that
// answers in PascalCase, and a sub-request that fails - so that any reimplementation can be
// diffed against a contract rather than an assumption. See docs/03-backlog.md, "does Jellyfin
// still earn being the WASM proof case?".
func TestJellyfinPluginUpstreamShapes(t *testing.T) {
	scenario := func(name string) string {
		return filepath.Join(jellyfinPluginDir, "testdata", "scenarios", name)
	}
	for _, c := range []struct {
		name    string
		dir     string
		want    string
		posters []string
	}{
		{
			name:    "items without a primary image are dropped and counted in a notice",
			dir:     scenario("missing-image"),
			posters: []string{"/Items/b1/Images/Primary"},
			want: `{
              "schemaVersion": 1,
              "title": "Jellyfin",
              "status": {"level": "ok", "text": "Online"},
              "blocks": [
                {"type": "poster-grid", "columns": 5, "empty": "No recent items with posters",
                 "items": [{"id": "b1", "title": "Solaris", "subtitle": "1972",
                            "image": {"ref": "v1.test.b1", "aspect": "2:3", "alt": "Solaris poster"}}]},
                {"type": "metrics", "items": [
                  {"label": "Movies", "value": 438, "format": "number"},
                  {"label": "Shows", "value": 61, "format": "number"},
                  {"label": "Streams", "value": 2, "format": "number", "level": "ok"}]}
              ],
              "notices": [{"level": "info", "message": "2 recent items had no primary image"}],
              "signals": {
                "movies": {"value": 438, "unit": "count"},
                "shows": {"value": 61, "unit": "count"},
                "active_streams": {"value": 2, "unit": "count", "level": "ok"}
              },
              "hints": {"ttlSeconds": 300}
            }`,
		},
		{
			// The load-bearing case for a declarative rewrite: the plugin OMITS subtitle rather
			// than emitting null, and widget-document.v1 types subtitle as shortText (a string
			// with no null member), so a template that always emits the key fails validation for
			// the whole document - not just that one item.
			name:    "an item with no production year omits subtitle entirely",
			dir:     scenario("missing-year"),
			posters: []string{"/Items/c1/Images/Primary", "/Items/c2/Images/Primary"},
			want: `{
              "schemaVersion": 1,
              "title": "Jellyfin",
              "status": {"level": "ok", "text": "Online"},
              "blocks": [
                {"type": "poster-grid", "columns": 5, "empty": "No recent items with posters",
                 "items": [
                   {"id": "c1", "title": "Metropolis", "subtitle": "1927",
                    "image": {"ref": "v1.test.c1", "aspect": "2:3", "alt": "Metropolis poster"}},
                   {"id": "c2", "title": "Untitled Archive Reel",
                    "image": {"ref": "v1.test.c2", "aspect": "2:3", "alt": "Untitled Archive Reel poster"}}]},
                {"type": "metrics", "items": [
                  {"label": "Movies", "value": 438, "format": "number"},
                  {"label": "Shows", "value": 61, "format": "number"},
                  {"label": "Streams", "value": 2, "format": "number", "level": "ok"}]}
              ],
              "signals": {
                "movies": {"value": 438, "unit": "count"},
                "shows": {"value": 61, "unit": "count"},
                "active_streams": {"value": 2, "unit": "count", "level": "ok"}
              },
              "hints": {"ttlSeconds": 300}
            }`,
		},
		{
			// Spike S2 confirmed a real server honours the explicit CamelCase profile, so this is
			// belt and braces rather than a required path - but it is behaviour the plugin has
			// today, and dropping it is a decision, not an oversight.
			name:    "a server answering in PascalCase is still understood",
			dir:     scenario("pascal-case"),
			posters: []string{"/Items/d1/Images/Primary", "/Items/d2/Images/Primary"},
			want: `{
              "schemaVersion": 1,
              "title": "Jellyfin",
              "status": {"level": "ok", "text": "Online"},
              "blocks": [
                {"type": "poster-grid", "columns": 5, "empty": "No recent items with posters",
                 "items": [
                   {"id": "d1", "title": "Nostalghia", "subtitle": "1983",
                    "image": {"ref": "v1.test.d1", "aspect": "2:3", "alt": "Nostalghia poster"}},
                   {"id": "d2", "title": "The Sacrifice", "subtitle": "1986",
                    "image": {"ref": "v1.test.d2", "aspect": "2:3", "alt": "The Sacrifice poster"}}]},
                {"type": "metrics", "items": [
                  {"label": "Movies", "value": 12, "format": "number"},
                  {"label": "Shows", "value": 3, "format": "number"},
                  {"label": "Streams", "value": 1, "format": "number", "level": "ok"}]}
              ],
              "signals": {
                "movies": {"value": 12, "unit": "count"},
                "shows": {"value": 3, "unit": "count"},
                "active_streams": {"value": 1, "unit": "count", "level": "ok"}
              },
              "hints": {"ttlSeconds": 300}
            }`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			broker := &jellyfinBroker{t: t, dataDir: filepath.Join(jellyfinPluginDir, "testdata"), scenarioDir: c.dir}
			response, err := invokeJellyfin(t, broker, `{"limit":5}`)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(response.Document)
			if err != nil {
				t.Fatal(err)
			}
			var gotValue, wantValue any
			if json.Unmarshal(got, &gotValue) != nil || json.Unmarshal([]byte(c.want), &wantValue) != nil {
				t.Fatal("scenario expectation or plugin output is not JSON")
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Errorf("document mismatch\ngot:  %s\nwant: %s", got, c.want)
			}
			if !reflect.DeepEqual(broker.assetPaths, c.posters) {
				t.Errorf("asset refs minted for %v, want %v", broker.assetPaths, c.posters)
			}
		})
	}
}

// A non-2xx sub-request fails the whole invocation rather than producing a partial document. The
// declarative runtime does not do this today - it never reads StatusCode and decodes whatever body
// arrived - which is the fidelity gap recorded in docs/03-backlog.md.
func TestJellyfinPluginFailsOnUpstreamError(t *testing.T) {
	for _, path := range []string{"/Items", "/Sessions"} {
		t.Run(path, func(t *testing.T) {
			broker := &jellyfinBroker{
				t: t, dataDir: filepath.Join(jellyfinPluginDir, "testdata"),
				failPath: path, failStatus: 500,
			}
			_, err := invokeJellyfin(t, broker, `{"limit":5}`)
			if err == nil {
				t.Fatal("invocation succeeded despite an upstream 500")
			}
			if !strings.Contains(err.Error(), "500") {
				t.Errorf("error does not name the status: %v", err)
			}
		})
	}
}
