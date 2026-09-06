// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"veduta.dev/veduta/schemas"
)

const (
	lockSchemaID     = "https://veduta.dev/schemas/integration-lock.v1.schema.json"
	manifestSchemaID = "https://veduta.dev/schemas/plugin-manifest.v1.schema.json"
)

var (
	schemaOnce sync.Once
	schemaErr  error
	compiled   *jsonschema.Schema
)

// lockSchema compiles schemas/integration-lock.v1.schema.json once, together with
// plugin-manifest.v1.schema.json - the lock schema's effectiveLimits/limits fields $ref the
// manifest schema's own limits $def, so both must be registered with the same compiler before
// either can resolve. Same pattern as internal/config.configSchema and internal/widgets'
// equivalent: one compiled schema, embedded, never read from disk at runtime.
func lockSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		c := jsonschema.NewCompiler()
		for _, file := range []struct{ name, id string }{
			{"plugin-manifest.v1.schema.json", manifestSchemaID},
			{"integration-lock.v1.schema.json", lockSchemaID},
		} {
			body, err := schemas.FS.ReadFile(file.name)
			if err != nil {
				schemaErr = fmt.Errorf("integrations: reading embedded schema %s: %w", file.name, err)
				return
			}
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
			if err != nil {
				schemaErr = fmt.Errorf("integrations: parsing embedded schema %s: %w", file.name, err)
				return
			}
			if err := c.AddResource(file.id, doc); err != nil {
				schemaErr = fmt.Errorf("integrations: loading embedded schema %s: %w", file.name, err)
				return
			}
		}
		compiled, schemaErr = c.Compile(lockSchemaID)
	})
	return compiled, schemaErr
}
