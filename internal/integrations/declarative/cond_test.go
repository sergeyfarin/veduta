// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// fixtureBroker answers /api/assets/statistics with {"images":1,"videos":2,"total":3}, so
// `stats.total > 100` is reliably false and `stats.total > 0` reliably true.
const condManifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata: { id: cond, name: Cond, version: 0.1.0 }
spec:
  runtime: declarative
  capabilities: [http]
  slots:
    - { name: server, kind: http }
  operations:
    - id: check
      name: Check
      routes:
        - { slot: server, method: GET, path: /api/assets/statistics, reason: numbers }
      pipeline:
        - as: stats
          request: { slot: server, method: GET, path: /api/assets/statistics }
      output:
        title: Cond
        blocks:
          - type: metrics
            items:
              - label: Always
                format: number
                value: { expr: stats.images }
                unit: %s
              - if: { expr: %s }
                then: { label: Sometimes, format: number, value: { expr: stats.videos } }
`

func runCond(t *testing.T, unit, condition string) (map[string]any, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	body := fmt.Sprintf(condManifest, unit, condition)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifestload.Load(path)
	if err != nil {
		return nil, err
	}
	inst, err := New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		return nil, err
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "check", Params: []byte(`{}`)})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(resp.Document)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, nil
}

func condItems(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	blocks, _ := doc["blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("want one block, got %v", doc["blocks"])
	}
	raw, _ := blocks[0].(map[string]any)["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, m)
	}
	return out
}

// The gap this node exists for: an absent optional field had no safe spelling. A bare {expr}
// yields null, which fails validation for any typed field and takes down the whole document; the
// obvious workaround, string(), yields the literal text "<nil>" and renders it to the user.
func TestCondOmitsAnObjectKeyRatherThanEmittingNull(t *testing.T) {
	doc, err := runCond(t, `{ if: { expr: "stats.total > 100" }, then: "TB" }`, "false")
	if err != nil {
		t.Fatal(err)
	}
	item := condItems(t, doc)[0]
	if _, present := item["unit"]; present {
		t.Errorf("unit should be absent entirely, got %v", item["unit"])
	}
	if item["label"] != "Always" {
		t.Errorf("the rest of the object should be untouched, got %v", item)
	}
}

func TestCondKeepsTheValueWhenTheConditionHolds(t *testing.T) {
	doc, err := runCond(t, `{ if: { expr: "stats.total > 0" }, then: "TB" }`, "false")
	if err != nil {
		t.Fatal(err)
	}
	if got := condItems(t, doc)[0]["unit"]; got != "TB" {
		t.Errorf("unit = %v, want TB", got)
	}
}

func TestCondOmitsAnArrayElement(t *testing.T) {
	for _, c := range []struct {
		condition string
		want      int
	}{
		{`"stats.total > 100"`, 1},
		{`"stats.total > 0"`, 2},
	} {
		t.Run(c.condition, func(t *testing.T) {
			doc, err := runCond(t, `"GB"`, c.condition)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(condItems(t, doc)); got != c.want {
				t.Errorf("%d items, want %d", got, c.want)
			}
		})
	}
}

// Truthiness would make `if: {expr: item.name}` quietly mean "when the name is non-empty", which
// is exactly the sort of near-miss a closed grammar should refuse.
func TestCondRejectsANonBooleanCondition(t *testing.T) {
	_, err := runCond(t, `{ if: { expr: "stats.total" }, then: "TB" }`, "false")
	if err == nil {
		t.Fatal("expected a non-boolean condition to fail")
	}
	if !strings.Contains(err.Error(), "want a boolean") {
		t.Errorf("error should name the problem, got %v", err)
	}
}

// An omitted value has no meaning in a request: a path, query value or header either has a value
// or the request is different. Silently sending an empty one would be worse than refusing.
func TestCondCannotOmitARequestValue(t *testing.T) {
	body := strings.Replace(condManifest,
		"request: { slot: server, method: GET, path: /api/assets/statistics }",
		"request:\n            slot: server\n            method: GET\n            path: /api/assets/statistics\n            query: { size: { if: { expr: \"false\" }, then: \"preview\" } }",
		1)
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(body, `"GB"`, "false")), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifestload.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := New(fixtureBroker{}).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "check", Params: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected an omitted query value to fail the invocation")
	}
	if !strings.Contains(err.Error(), "cannot omit") {
		t.Errorf("error should explain the boundary, got %v", err)
	}
}
