// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// baseManifest is a minimal, schema-valid declarative manifest. Every adversarial test below
// mutates one specific piece of it and asserts Load rejects exactly that mutation - proving the
// load-bearing checks in load.go actually fire, not merely that they are present in the source.
const baseManifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata:
  id: test
  name: Test
  version: 0.1.0
spec:
  runtime: declarative
  slots:
    - name: server
      kind: http
  capabilities: [http]
  operations:
    - id: op
      routes:
        - { slot: server, method: GET, path: /a }
      signals:
        - { name: sig, type: number }
      pipeline:
        - as: r
          request: { slot: server, method: GET, path: /a }
      output:
        title: Test
        signals:
          sig: { value: { expr: r.value } }
`

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustReject(t *testing.T, body, wantSubstring string) {
	t.Helper()
	_, err := Load(writeManifest(t, body))
	if err == nil {
		t.Fatal("expected Load to reject this manifest, got nil error")
	}
	if wantSubstring != "" && !strings.Contains(err.Error(), wantSubstring) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), wantSubstring)
	}
}

func TestLoad_BaseManifestIsValid(t *testing.T) {
	if _, err := Load(writeManifest(t, baseManifest)); err != nil {
		t.Fatalf("the shared base fixture itself must load cleanly: %v", err)
	}
}

func TestLoad_RejectsOversizedManifest(t *testing.T) {
	body := baseManifest + "# " + strings.Repeat("x", maxManifestBytes+1) + "\n"
	mustReject(t, body, "")
}

func TestLoad_RejectsExcessiveYAMLDepth(t *testing.T) {
	// 40 levels of nesting under a throwaway top-level key - exceeds the depth-32 cap regardless
	// of what schema validation would later make of the shape.
	nested := "1"
	for range 40 {
		nested = "[" + nested + "]"
	}
	body := baseManifest + "throwaway: " + nested + "\n"
	mustReject(t, body, "")
}

func TestLoad_RejectsExcessiveYAMLNodeCount(t *testing.T) {
	var b strings.Builder
	b.WriteString(baseManifest)
	b.WriteString("throwaway:\n")
	for i := range 21000 {
		b.WriteString("  k" + strconv.Itoa(i) + ": 1\n")
	}
	mustReject(t, b.String(), "")
}

func TestLoad_RejectsAliasesOutright(t *testing.T) {
	body := baseManifest + "anchored: &a 1\naliased: *a\n"
	mustReject(t, body, "")
}

func TestLoad_RejectsDuplicateKeys(t *testing.T) {
	body := strings.Replace(baseManifest, "  name: Test\n", "  name: Test\n  name: Test\n", 1)
	mustReject(t, body, "")
}

// bigExpr builds a syntactically valid expr string with roughly n AST nodes (a long chain of
// "1+1+1+...").
func bigExpr(n int) string {
	var b strings.Builder
	b.WriteString("1")
	for range n {
		b.WriteString("+1")
	}
	return b.String()
}

func TestLoad_RejectsOversizedSingleExpression(t *testing.T) {
	body := strings.Replace(baseManifest, "value: { expr: r.value }", `value: { expr: "`+bigExpr(600)+`" }`, 1)
	mustReject(t, body, "AST nodes")
}

func TestLoad_RejectsAggregatePerOperationExpressionCeiling(t *testing.T) {
	// Fifteen distinct 300-ish-node expressions, individually well under the 512 per-expression
	// cap, summing past the 4096 per-operation aggregate ceiling.
	var fields strings.Builder
	for i := range 12 {
		fields.WriteString("          f" + strconv.Itoa(i) + `: { expr: "` + bigExpr(200) + `" }` + "\n")
	}
	body := strings.Replace(baseManifest, "        signals:\n          sig: { value: { expr: r.value } }\n",
		"        signals:\n          sig: { value: { expr: r.value } }\n        extra:\n"+fields.String(), 1)
	mustReject(t, body, "4096")
}

func TestLoad_RejectsLiteralStringByteCeiling(t *testing.T) {
	body := strings.Replace(baseManifest, "title: Test", "title: Test\n        subtitle: \""+strings.Repeat("x", 65<<10)+"\"", 1)
	mustReject(t, body, "64 KiB")
}

func TestLoad_RejectsUndeclaredSlot(t *testing.T) {
	body := strings.Replace(baseManifest, "request: { slot: server, method: GET, path: /a }",
		"request: { slot: nope, method: GET, path: /a }", 1)
	mustReject(t, body, "slot")
}

func TestLoad_RejectsPipelineRequestWithoutHTTPCapability(t *testing.T) {
	body := strings.Replace(baseManifest, "capabilities: [http]", "capabilities: []", 1)
	mustReject(t, body, "http capability")
}

func TestLoad_RejectsStaticPipelineRouteMismatch(t *testing.T) {
	body := strings.Replace(baseManifest, "request: { slot: server, method: GET, path: /a }",
		"request: { slot: server, method: GET, path: /not-declared }", 1)
	mustReject(t, body, "no declared route")
}

func TestLoad_RejectsStaticAssetMismatch(t *testing.T) {
	body := strings.Replace(baseManifest, "capabilities: [http]", "capabilities: [http, assets]", 1)
	body = strings.Replace(body, "title: Test",
		"title: Test\n        image: { asset: { slot: server, path: /not-an-asset-route } }", 1)
	mustReject(t, body, "no asset route")
}

func TestLoad_RejectsAssetNodeWithoutAssetsCapability(t *testing.T) {
	body := strings.Replace(baseManifest, "routes:\n        - { slot: server, method: GET, path: /a }",
		"routes:\n        - { slot: server, method: GET, path: /a }\n        - { slot: server, method: GET, path: /pic, use: asset }", 1)
	body = strings.Replace(body, "title: Test",
		"title: Test\n        image: { asset: { slot: server, path: /pic } }", 1)
	mustReject(t, body, "assets capability")
}

func TestLoad_RejectsUndeclaredSignal(t *testing.T) {
	body := strings.Replace(baseManifest, "sig: { value: { expr: r.value } }",
		"sig: { value: { expr: r.value } }\n          ghost: { value: { expr: 1 } }", 1)
	mustReject(t, body, "not declared")
}

func TestLoad_RejectsEnvAccess(t *testing.T) {
	body := strings.Replace(baseManifest, "expr: r.value", "expr: '$env'", 1)
	mustReject(t, body, "forbidden")
}

func TestLoad_RejectsMatchesOperator(t *testing.T) {
	body := strings.Replace(baseManifest, "expr: r.value", `expr: 'r.value matches "^x"'`, 1)
	mustReject(t, body, "matches")
}

// TestLoad_RejectsInternalIdentifierAccess is the regression test for a real, confirmed leak: a
// manifest expression referencing "__grant" (the internal env key the declarative runtime uses
// to thread capabilities.Grant into charged builtin calls) could read the invocation's full
// authority object - routes, approved capabilities, limits - into a rendered Widget Document, or
// use it to make control-flow decisions no plugin is supposed to be able to introspect. Confirmed
// by directly compiling and running `__grant` against a real budget/env before this fix: it
// evaluated to whatever value the env map held under that key. Fixed by rejecting any
// "__"-prefixed identifier in the same place "$env" was already rejected, closing off every
// internal name at once rather than enumerating today's two (__grant, __ctx).
func TestLoad_RejectsInternalIdentifierAccess(t *testing.T) {
	for _, name := range []string{"__grant", "__ctx", "__d3_charge", "__anything"} {
		t.Run(name, func(t *testing.T) {
			body := strings.Replace(baseManifest, "expr: r.value", "expr: "+name, 1)
			mustReject(t, body, "forbidden")
		})
	}
}

func TestLoad_RejectsCustomFunctionCalls(t *testing.T) {
	body := strings.Replace(baseManifest, "expr: r.value", "expr: 'notARealBuiltin(r.value)'", 1)
	mustReject(t, body, "")
}
