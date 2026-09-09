// SPDX-License-Identifier: AGPL-3.0-or-later

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/config"
)

func TestApplyPreservesCommentsAndMergesSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veduta.yaml")
	original := `# keep this operator comment
version: 1
auth: {mode: none}
dashboard:
  title: Home # and this title comment
  layout: {columns: 4, gap: normal}
connections: {}
integrations: []
sections:
  - title: Media
    cards:
      - {id: existing, title: Existing}
rules: []
notifications: {channels: {}}
`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	patch := config.ApplyPatch{
		Connections:  map[string]map[string]any{"photos": {"kind": "http", "enabled": false, "baseUrl": "http://photos:2283"}},
		Integrations: []map[string]any{{"id": "immich", "source": "path:plugins/immich", "enabled": false}},
		Sections:     []map[string]any{{"title": "Media", "cards": []any{map[string]any{"id": "photos", "title": "Photos", "integration": "immich", "operation": "recent-assets", "slots": map[string]any{"server": "photos"}}}}},
	}
	if err := config.Apply(path, patch); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, retained := range []string{"# keep this operator comment", "# and this title comment", "layout: {columns: 4, gap: normal}"} {
		if !strings.Contains(text, retained) {
			t.Fatalf("applied config lost %q:\n%s", retained, text)
		}
	}
	if strings.Count(text, "title: Media") != 1 {
		t.Fatalf("matching section was duplicated:\n%s", text)
	}
	snapshot, diagnostics := config.LoadPath(path)
	if snapshot == nil || diagnostics.HasErrors() || len(snapshot.Config.Sections[0].Cards) != 2 {
		t.Fatalf("applied config did not validate: %s", diagnostics.String())
	}
	if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode was not preserved: info=%v err=%v", info, statErr)
	}
}

func TestApplyValidationFailureLeavesOriginalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veduta.yaml")
	original := "version: 1\nauth: {mode: none}\nintegrations: []\nsections: []\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	err := config.Apply(path, config.ApplyPatch{Sections: []map[string]any{{"title": "Broken", "cards": []any{map[string]any{"id": "bad", "integration": "missing"}}}}})
	if err == nil {
		t.Fatal("invalid patch was written")
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil || string(body) != original {
		t.Fatalf("original changed after rejected patch: %q, %v", body, readErr)
	}
}
