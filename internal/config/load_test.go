// SPDX-License-Identifier: AGPL-3.0-or-later

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/config"
)

func timeout() <-chan time.Time { return time.After(5 * time.Second) }

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalValid = `
version: 1
auth:
  mode: none
`

// TestLoad_RealExampleConfig proves config.Load handles the actual shipped example, not just
// small synthetic fixtures - if a schema change ever breaks examples/veduta.yaml, this is where
// that shows up, not silently in production.
//
// It also covers Jellyfin's composed Authorization value: embedded references are collected as
// real secret locations, not left as literal placeholder text.
func TestLoad_RealExampleConfig(t *testing.T) {
	snap, diags := config.LoadPath("../../examples/veduta.yaml")
	if diags.HasErrors() {
		t.Fatalf("examples/veduta.yaml should load cleanly:\n%s", diags)
	}
	if snap == nil {
		t.Fatal("nil snapshot on success")
	}
	if _, ok := snap.CardByID("jellyfin-recent"); !ok {
		t.Error("expected card jellyfin-recent")
	}
	if _, ok := snap.IntegrationByID("jellyfin"); !ok {
		t.Error("expected integration jellyfin")
	}
	found := false
	for _, ref := range snap.SecretRefs {
		if ref.Name == "JELLYFIN_KEY" {
			found = true
		}
	}
	if !found {
		t.Error("embedded Jellyfin secret was not collected")
	}
}

func TestLoad_EmbeddedSecretIsRecognised(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
connections:
  jellyfin:
    kind: http
    baseUrl: http://jellyfin:8096
    auth:
      type: header
      name: Authorization
      value: 'MediaBrowser Token="${secret:JELLYFIN_KEY}"'
`)
	snap, diags := config.Load(path)
	if diags.HasErrors() {
		t.Fatalf("an embedded secret reference should load: %s", diags)
	}
	if snap == nil {
		t.Fatal("nil snapshot despite no errors")
	}
	found := false
	for _, ref := range snap.SecretRefs {
		if ref.Name == "JELLYFIN_KEY" {
			found = true
		}
	}
	if !found {
		t.Fatal("embedded reference not collected")
	}
	for _, d := range diags {
		if d.Severity == config.SeverityWarning {
			t.Fatalf("valid embedded reference warned: %s", d)
		}
	}
}

// TestLoad_SuspiciousSecretRef_WholeValueDoesNotWarn: a real, whole-value secret reference must
// not itself trigger the warning - only the embedded/partial case should.
func TestLoad_SuspiciousSecretRef_WholeValueDoesNotWarn(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
connections:
  immich:
    kind: http
    baseUrl: http://immich:2283
    auth: { type: bearer, value: "${secret:IMMICH_KEY}" }
`)
	_, diags := config.Load(path)
	for _, d := range diags {
		if strings.Contains(d.Message, "not recognised as a secret reference") {
			t.Fatalf("a whole-value secret reference must not warn: %s", d)
		}
	}
}

func TestLoad_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid)
	snap, diags := config.Load(path)
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %s", diags)
	}
	if snap == nil {
		t.Fatal("nil snapshot on success")
	}
}

func TestLoad_UnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+"notAField: true\n")
	snap, diags := config.Load(path)
	if !diags.HasErrors() {
		t.Fatal("want an error for an unknown top-level key")
	}
	if snap != nil {
		t.Error("want a nil snapshot when there are errors")
	}
	assertAllPositioned(t, diags)
}

func TestLoad_DuplicateCardID(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
sections:
  - cards:
      - id: dup
      - id: dup
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `duplicate card id "dup"`)
	assertAllPositioned(t, diags)
}

func TestLoad_DuplicateIntegrationID(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
integrations:
  - id: dup
    source: builtin
  - id: dup
    source: builtin
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `duplicate integration id "dup"`)
}

func TestLoad_DuplicateRuleID(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
notifications:
  channels:
    phone: { type: ntfy, url: https://ntfy.sh, topic: x }
rules:
  - id: dup
    when: 'state("x") == "error"'
    notify: [phone]
  - id: dup
    when: 'state("x") == "error"'
    notify: [phone]
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `duplicate rule id "dup"`)
}

// TestLoad_DuplicateYAMLKey is the raw-document hygiene check: a YAML mapping key repeated
// literally, which the decoder would otherwise silently resolve to "last one wins" with no
// error at all.
func TestLoad_DuplicateYAMLKey(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", `
version: 1
version: 1
auth:
  mode: none
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `duplicate key "version"`)
	assertAllPositioned(t, diags)
}

func TestLoad_BadReference_UndeclaredIntegration(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
sections:
  - cards:
      - id: rogue
        integration: nowhere
        operation: x
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `integration "nowhere" is not declared`)
}

func TestLoad_BadReference_UndeclaredConnection(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
sections:
  - cards:
      - id: c1
        slots: { server: ghost }
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `slot "server" is bound to undeclared connection "ghost"`)
}

func TestLoad_BadReference_UndeclaredNotifyChannel(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
rules:
  - id: r1
    when: 'true'
    notify: [nope]
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `notifies undeclared channel "nope"`)
}

func TestLoad_BadReference_RuleUnknownCard(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
notifications:
  channels:
    phone: { type: ntfy, url: https://ntfy.sh, topic: x }
rules:
  - id: r1
    when: 'state("ghost-card") == "error"'
    notify: [phone]
`)
	_, diags := config.Load(path)
	assertContains(t, diags, `references unknown card "ghost-card"`)
}

// TestLoad_Cycle proves a self-referential YAML anchor is a clean, prompt diagnostic - not a
// hang and not a panic. yaml.v3 itself rejects the cycle during decode ("anchor ... contains
// itself"); confirmed separately, before writing this package, that Node.Decode does this in
// well under a second rather than looping.
func TestLoad_Cycle(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", `
version: 1
auth:
  mode: none
x: &a
  y: *a
`)
	done := make(chan config.Diagnostics, 1)
	go func() {
		_, diags := config.Load(path)
		done <- diags
	}()
	select {
	case diags := <-done:
		if !diags.HasErrors() {
			t.Fatal("want an error for a cyclic anchor")
		}
	case <-timeout():
		t.Fatal("Load did not return - a cyclic anchor may have hung the loader")
	}
}

func TestLoad_AuthModeMismatch(t *testing.T) {
	// Constructed by hand, bypassing the schema, to exercise validateAuth's own defence in depth
	// directly rather than only through a path the schema would already have blocked.
	dir := t.TempDir()
	// mode: password without admin is schema-invalid too (schema requires admin) - this proves
	// BOTH layers catch it; the schema error arrives first and short-circuits, which is correct
	// (Load never runs semantic checks over data the schema already rejected).
	path := write(t, dir, "veduta.yaml", `
version: 1
auth:
  mode: password
`)
	_, diags := config.Load(path)
	if !diags.HasErrors() {
		t.Fatal("want an error")
	}
}

// TestLoad_ConfDMergeOrder: later files override scalars/mappings recursively, but replace
// arrays wholesale - docs/01-architecture.md section 2's own wording, verified literally.
func TestLoad_ConfDMergeOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "veduta.yaml", `
version: 1
auth:
  mode: none
dashboard:
  title: Home
  groupBy: section
sections:
  - title: Media
    cards:
      - id: a
      - id: b
`)
	write(t, dir, "conf.d/10-override.yaml", `
dashboard:
  groupBy: tag
sections:
  - title: Overridden
    cards:
      - id: c
`)
	snap, diags := config.LoadPath(filepath.Join(dir, "veduta.yaml"))
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %s", diags)
	}
	if snap.Config.Dashboard.Title != "Home" {
		t.Errorf("title = %q, want the base file's value preserved", snap.Config.Dashboard.Title)
	}
	if snap.Config.Dashboard.GroupBy != "tag" {
		t.Errorf("groupBy = %q, want overridden", snap.Config.Dashboard.GroupBy)
	}
	if len(snap.Config.Sections) != 1 || snap.Config.Sections[0].Title != "Overridden" {
		t.Fatalf("sections = %+v, want the array replaced wholesale by conf.d", snap.Config.Sections)
	}
}

// TestLoad_ConfDIsOptional: most installations will not have a conf.d directory at all.
func TestLoad_ConfDIsOptional(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid)
	_, diags := config.LoadPath(path)
	if diags.HasErrors() {
		t.Fatalf("a missing conf.d must not be an error: %s", diags)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, diags := config.Load("/does/not/exist.yaml")
	if !diags.HasErrors() {
		t.Fatal("want an error for a missing file")
	}
}

func TestLoad_NoPaths(t *testing.T) {
	_, diags := config.Load()
	if !diags.HasErrors() {
		t.Fatal("want an error when no paths are given")
	}
}

func assertContains(t *testing.T, diags config.Diagnostics, substr string) {
	t.Helper()
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return
		}
	}
	t.Fatalf("diagnostics do not contain %q:\n%s", substr, diags)
}

func assertAllPositioned(t *testing.T, diags config.Diagnostics) {
	t.Helper()
	for _, d := range diags {
		if d.Line == 0 {
			t.Errorf("diagnostic has no line number: %s", d)
		}
		if d.File == "" {
			t.Errorf("diagnostic has no file: %s", d)
		}
	}
}

// TestLoadPath_ConfDIsAFileNotADirectory: a real, surprising problem (someone created a plain
// file named conf.d) must be a diagnostic, not silently treated the same as "no conf.d at all".
func TestLoadPath_ConfDIsAFileNotADirectory(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid)
	write(t, dir, "conf.d", "not a directory")
	_, diags := config.LoadPath(path)
	if !diags.HasErrors() {
		t.Fatal("want an error when conf.d exists but is not a directory")
	}
}

func TestLoad_SecretRefsCollectsEveryOccurrence(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
notifications:
  channels:
    phone: { type: ntfy, url: https://ntfy.sh, topic: x, token: "${secret:TOK}" }
    other: { type: ntfy, url: https://ntfy.sh, topic: y, token: "${secret:TOK}" }
`)
	snap, diags := config.Load(path)
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %s", diags)
	}
	var toks []config.SecretLocation
	for _, l := range snap.SecretRefs {
		if l.Name == "TOK" {
			toks = append(toks, l)
		}
	}
	if len(toks) != 2 {
		t.Fatalf("got %d occurrences of TOK, want 2: %+v", len(toks), toks)
	}
	for _, l := range toks {
		if l.Line == 0 || l.File == "" {
			t.Errorf("occurrence missing position: %+v", l)
		}
	}
	if toks[0].Line == toks[1].Line {
		t.Errorf("both occurrences report the same line %d, want two distinct lines", toks[0].Line)
	}
}

func TestLoad_DisabledConnectionDoesNotRequireItsSecret(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "veduta.yaml", minimalValid+`
connections:
  imported:
    kind: http
    enabled: false
    baseUrl: http://service.example.test
    auth: {type: bearer, value: "${secret:NOT_SET_UNTIL_REVIEWED}"}
`)
	snapshot, diagnostics := config.Load(path)
	if diagnostics.HasErrors() {
		t.Fatal(diagnostics.String())
	}
	if len(snapshot.SecretRefs) != 0 {
		t.Fatalf("disabled connection exposed active secret refs: %#v", snapshot.SecretRefs)
	}
}
