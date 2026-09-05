# Third-party licences

Every Go module in `go.mod` must appear here with its licence, and a test in
`internal/contracts` fails the build if one is missing. Adding a dependency is therefore a
deliberate, recorded act rather than a side effect of an import.

Veduta's core is AGPL-3.0-or-later. Apache-2.0, BSD and MIT code is one-way compatible into it.
**GPL-2.0-only, SSPL, BUSL and unlicensed code are not, and must not be added.**

## Direct

| Module | Licence | Why |
| --- | --- | --- |
| `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | One JSON Schema implementation validates configuration, manifests, lock records and widget documents — the same schema files the editor and the frontend use |
| `gopkg.in/yaml.v3` | Apache-2.0 / MIT | YAML parsing with node positions, so configuration errors can carry `file:line:col` |

## Indirect

| Module | Licence |
| --- | --- |
| `golang.org/x/text` | BSD-3-Clause |

## Frontend

Build-time only; nothing here is shipped inside the Go binary except the compiled bundle.

| Package | Licence |
| --- | --- |
| `svelte`, `@sveltejs/vite-plugin-svelte` | MIT |
| `vite` | MIT |
| `typescript` | Apache-2.0 |
| `svelte-check` | MIT |
| `@tsconfig/svelte` | MIT |
| `concurrently` | MIT |

A generated `THIRD-PARTY-NOTICES.md` covering the shipped frontend bundle is a release task (L4).
