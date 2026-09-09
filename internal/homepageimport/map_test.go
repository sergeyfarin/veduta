// SPDX-License-Identifier: AGPL-3.0-or-later

package homepageimport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/homepageimport"
)

func TestMapAndApplyRepresentativeCorpus(t *testing.T) {
	model, _, err := homepageimport.ParseDir(filepath.Join("..", "..", "testdata", "homepage"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "veduta.yaml")
	writeFile(t, configPath, `# existing comment
version: 1
auth: {mode: none}
dashboard: {title: Existing}
connections: {}
integrations: []
sections: []
rules: []
notifications: {channels: {}}
`)
	existing, diagnostics := config.LoadPath(configPath)
	if diagnostics.HasErrors() {
		t.Fatal(diagnostics.String())
	}
	patch, report, warnings := homepageimport.Map(model, existing, homepageimport.MapOptions{})
	if report.Services != 20 || report.Complete != 19 || report.WithoutWidgets != 1 || report.NeedManual != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.EnvironmentVariables) == 0 || len(warnings) == 0 {
		t.Fatalf("environment=%v warnings=%v", report.EnvironmentVariables, warnings)
	}
	if len(patch.Integrations) != 3 {
		t.Fatalf("plugin declarations = %#v", patch.Integrations)
	}
	if err = config.Apply(configPath, patch); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "jellyfin-secret") || !strings.Contains(text, "${secret:HOMEPAGE_JELLYFIN_KEY}") {
		t.Fatalf("source credential leaked or secret reference missing:\n%s", text)
	}
	if !strings.Contains(text, "enabled: false") || !strings.Contains(text, "# existing comment") {
		t.Fatalf("review gate or existing comment missing:\n%s", text)
	}
	applied, diagnostics := config.LoadPath(configPath)
	if applied == nil || diagnostics.HasErrors() {
		t.Fatalf("applied import is invalid: %s", diagnostics.String())
	}
}

func TestMapDeduplicatesBaseURLsAndExistingIDs(t *testing.T) {
	falseValue := false
	existing := &config.Snapshot{Config: config.Config{
		Connections: map[string]config.Connection{"media": {Kind: "http", Enabled: &falseValue, HTTP: &config.HTTPConnection{BaseURL: "http://media:8096"}}},
		Sections:    []config.Section{{Cards: []config.Card{{ID: "jellyfin"}}}},
	}}
	model := homepageimport.Model{Groups: []homepageimport.Group{{Name: "Media", Services: []homepageimport.Service{
		{Name: "Jellyfin", Widgets: []homepageimport.Widget{{Type: "jellyfin", URL: "http://media:8096/"}}},
		{Name: "Jellyfin", Widgets: []homepageimport.Widget{{Type: "jellyfin", URL: "http://media:8096"}}},
	}}}}
	patch, _, _ := homepageimport.Map(model, existing, homepageimport.MapOptions{})
	if len(patch.Connections) != 0 {
		t.Fatalf("existing base URL was duplicated: %#v", patch.Connections)
	}
	cards := patch.Sections[0]["cards"].([]any)
	if cards[0].(map[string]any)["id"] != "jellyfin-2" || cards[1].(map[string]any)["id"] != "jellyfin-3" {
		t.Fatalf("card ids were not made unique: %#v", cards)
	}
}
