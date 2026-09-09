// SPDX-License-Identifier: AGPL-3.0-or-later

package homepageimport_test

import (
	"os"
	"path/filepath"
	"testing"

	"veduta.dev/veduta/internal/homepageimport"
)

func TestParseDirRepresentativeCorpus(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "homepage")
	model, warnings, err := homepageimport.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Groups) != 3 || len(model.Groups[0].Groups) != 1 {
		t.Fatalf("groups were not preserved: %#v", model.Groups)
	}
	if got := countServices(model.Groups); got != 20 {
		t.Fatalf("parsed %d services, want 20", got)
	}
	if len(model.Bookmarks) != 1 || len(model.Bookmarks[0].Bookmarks) != 2 {
		t.Fatalf("bookmarks were not preserved: %#v", model.Bookmarks)
	}
	if len(model.Widgets) != 2 || model.Widgets[0].Type != "resources" {
		t.Fatalf("global widgets were not preserved: %#v", model.Widgets)
	}
	if model.Settings["title"] != "Home" {
		t.Fatalf("settings were not preserved: %#v", model.Settings)
	}
	if len(warnings) != 1 {
		t.Fatalf("got warnings %#v, want the one unknown service widget", warnings)
	}
}

func TestParseDockerLabelsIndexedWidgetsAndDottedHeaders(t *testing.T) {
	parsed, warnings, ok := homepageimport.ParseDockerLabels(map[string]string{
		"homepage.group":                           "Media",
		"homepage.name":                            "Jellyfin",
		"homepage.href":                            "https://media.example.test",
		"homepage.server":                          "docker",
		"homepage.container":                       "jellyfin",
		"homepage.widgets[0].type":                 "jellyfin",
		"homepage.widgets[0].url":                  "http://jellyfin:8096",
		"homepage.widgets[0].headers.X.Auth.Token": "secret",
		"homepage.widgets[1].type":                 "future-widget",
	})
	if !ok || parsed.Group != "Media" || parsed.Service.Name != "Jellyfin" {
		t.Fatalf("labels were not recognised: %#v", parsed)
	}
	if len(parsed.Service.Widgets) != 2 || len(warnings) != 1 {
		t.Fatalf("widgets=%#v warnings=%#v", parsed.Service.Widgets, warnings)
	}
	headers, ok := parsed.Service.Widgets[0].Options["headers"].(map[string]any)
	if !ok || headers["X.Auth.Token"] != "secret" {
		t.Fatalf("dotted header was not preserved: %#v", parsed.Service.Widgets[0].Options)
	}
}

func TestParseDirRejectsMalformedStructure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "services.yaml"), "Media: {}\n")
	if _, _, err := homepageimport.ParseDir(dir); err == nil {
		t.Fatal("mapping top level accepted")
	}

	writeFile(t, filepath.Join(dir, "services.yaml"), "- Media:\n  - Broken: scalar\n")
	if _, _, err := homepageimport.ParseDir(dir); err == nil {
		t.Fatal("scalar service accepted")
	}
}

func countServices(groups []homepageimport.Group) int {
	total := 0
	for _, group := range groups {
		total += len(group.Services) + countServices(group.Groups)
	}
	return total
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
