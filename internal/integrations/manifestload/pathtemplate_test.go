// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import (
	"strings"
	"testing"
)

// templated swaps baseManifest's single route, params and pipeline path for the given ones, so
// each case below differs from a manifest that loads in exactly the way it names.
func templated(route, params, path string) string {
	body := strings.Replace(baseManifest, "        - { slot: server, method: GET, path: /a }\n",
		"        - { slot: server, method: GET, path: "+route+" }\n", 1)
	body = strings.Replace(body, "      signals:\n        - { name: sig", params+"      signals:\n        - { name: sig", 1)
	return strings.Replace(body, "request: { slot: server, method: GET, path: /a }",
		"request: { slot: server, method: GET, path: "+path+" }", 1)
}

const requiredID = `      params:
        type: object
        required: [id]
        properties: { id: { type: string } }
`

func TestLoad_AcceptsCoveredPlaceholder(t *testing.T) {
	m, err := Load(writeManifest(t, templated("/items/*/detail", requiredID, `"/items/{id}/detail"`)))
	if err != nil {
		t.Fatal(err)
	}
	req := m.Operations[0].Pipeline[0].Request
	if req.PathTemplate == nil || strings.Join(req.PathTemplate.Placeholders(), ",") != "id" {
		t.Fatalf("PathTemplate = %+v, want one placeholder id", req.PathTemplate)
	}
}

// Each of these is a way a manifest could make its reachable paths undecidable at approval, or
// fail only when a card runs. All must be refused at load, before anyone is asked to approve. The
// schema refuses the purely syntactic ones first; validateRequest repeats every rule anyway,
// because http-json builds its request without passing through the schema.
func TestLoad_RejectsUnprovablePipelinePaths(t *testing.T) {
	cases := map[string]struct{ route, params, path, want string }{
		"expression path":                {"/items/*/detail", requiredID, `{ expr: '"/items/" + params.id + "/detail"' }`, "/request/path': got object, want string"},
		"placeholder on a narrower glob": {"/items/v*/detail", requiredID, `"/items/{id}/detail"`, "each {param} segment needs a route segment that is exactly *"},
		"placeholder on a literal route": {"/items/1/detail", requiredID, `"/items/{id}/detail"`, "no declared route covers"},
		"undeclared parameter":           {"/items/*/detail", "", `"/items/{id}/detail"`, "names no parameter"},
		"optional parameter": {"/items/*/detail", `      params:
        type: object
        properties: { id: { type: string } }
`, `"/items/{id}/detail"`, "must be required or have a default"},
		"object parameter": {"/items/*/detail", `      params:
        type: object
        required: [id]
        properties: { id: { type: object } }
`, `"/items/{id}/detail"`, "must be string or integer"},
		"brace inside a segment": {"/items/*/detail", requiredID, `"/items/x{id}/detail"`, "/request/path': '/items/x{id}/detail' does not match pattern"},
		"query in the template":  {"/items/*/detail", requiredID, `"/items/{id}/detail?x=1"`, "/request/path': '/items/{id}/detail?x=1' does not match pattern"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mustReject(t, templated(c.route, c.params, c.path), c.want)
		})
	}
}
