// SPDX-License-Identifier: AGPL-3.0-or-later

package homepageimport_test

import (
	"fmt"
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
	if len(patch.Integrations) != 5 {
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

// TestMapCredentialShapeThatCannotBeTranslated covers the two ends of the auth question the
// importer has to answer per widget. Home Assistant's Homepage key IS its bearer token, so the
// import is complete and the secret reference is written for it. Proxmox's is not: Proxmox wants
// one Authorization header assembled from Homepage's separate username and password fields, in a
// scheme that is neither bearer nor basic. The importer must then write NO auth block at all -
// a plausible-looking wrong one would fail at the first refresh with nothing in the file to
// explain why - and must say so in a warning.
func TestMapCredentialShapeThatCannotBeTranslated(t *testing.T) {
	model := homepageimport.Model{Groups: []homepageimport.Group{{Name: "Infrastructure", Services: []homepageimport.Service{
		{Name: "Proxmox", Widgets: []homepageimport.Widget{{Type: "proxmox", URL: "https://pve.example.test:8006", Key: "ignored"}}},
		{Name: "Home Assistant", Widgets: []homepageimport.Widget{{Type: "homeassistant", URL: "http://ha.example.test:8123", Key: "ha-token"}}},
	}}}}
	patch, report, warnings := homepageimport.Map(model, nil, homepageimport.MapOptions{})

	if _, hasAuth := patch.Connections["proxmox"]["auth"]; hasAuth {
		t.Errorf("proxmox connection carries a guessed auth block: %#v", patch.Connections["proxmox"])
	}
	for _, name := range report.EnvironmentVariables {
		if strings.Contains(name, "PROXMOX") {
			t.Errorf("a secret variable was promised for a credential that was not written: %q", name)
		}
	}
	var explained bool
	for _, warning := range warnings {
		if strings.Contains(warning.Message, "PVEAPIToken") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("no warning explains the Proxmox token the operator must write: %#v", warnings)
	}

	auth, ok := patch.Connections["home-assistant"]["auth"].(map[string]any)
	if !ok || auth["type"] != "bearer" || auth["value"] != "${secret:HOMEPAGE_HOME_ASSISTANT_KEY}" {
		t.Errorf("home assistant bearer token was not carried across: %#v", patch.Connections["home-assistant"])
	}
	if strings.Contains(fmt.Sprint(patch.Connections), "ha-token") {
		t.Errorf("the source credential was written into the configuration: %#v", patch.Connections)
	}

	cards := patch.Sections[0]["cards"].([]any)
	first := cards[0].(map[string]any)
	if first["integration"] != "proxmox" || first["operation"] != "cluster-overview" {
		t.Errorf("proxmox card = %#v", first)
	}
	second := cards[1].(map[string]any)
	if second["integration"] != "homeassistant" || second["operation"] != "overview" {
		t.Errorf("home assistant card = %#v", second)
	}
}
