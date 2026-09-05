// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"veduta.dev/veduta/schemas"
)

var (
	schemaOnce sync.Once
	schemaErr  error
	compiled   *jsonschema.Schema
)

// configSchema compiles schemas/config.v1.schema.json once. Same pattern as
// internal/widgets.widgetSchema - one compiled schema, embedded, never read from disk at runtime.
func configSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		body, err := schemas.FS.ReadFile("config.v1.schema.json")
		if err != nil {
			schemaErr = fmt.Errorf("config: reading embedded schema: %w", err)
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			schemaErr = fmt.Errorf("config: parsing embedded schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		const id = "https://veduta.dev/schemas/config.v1.schema.json"
		if err := c.AddResource(id, doc); err != nil {
			schemaErr = fmt.Errorf("config: loading embedded schema: %w", err)
			return
		}
		compiled, schemaErr = c.Compile(id)
	})
	return compiled, schemaErr
}
