// SPDX-License-Identifier: AGPL-3.0-or-later

package manifestload

import "testing"

func FuzzManifestParser(f *testing.F) {
	f.Add([]byte(`apiVersion: veduta.dev/v1
kind: Integration
metadata: {id: fuzz-demo, name: Fuzz demo, version: 1.0.0}
spec:
  runtime: declarative
  capabilities: []
  operations:
    - id: show
      name: Show
      pipeline: []
      output: {schemaVersion: 1, blocks: [{type: text, content: hello}]}
`))
	f.Add([]byte("&a [*a]\n"))
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, raw []byte) {
		manifest, err := loadBytes("fuzz.yaml", raw)
		if err == nil && (manifest == nil || manifest.ID == "" || manifest.Digest == "") {
			t.Fatalf("successful parse returned an incomplete manifest: %+v", manifest)
		}
	})
}
