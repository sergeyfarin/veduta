# Third-party licences

Every Go module in `go.mod` must appear here with its licence, and a test in
`internal/contracts` fails the build if one is missing. Adding a dependency is therefore a
deliberate, recorded act rather than a side effect of an import.

Veduta's core is AGPL-3.0-or-later. Apache-2.0, BSD and MIT code is one-way compatible into it.
**GPL-2.0-only, SSPL, BUSL and unlicensed code are not, and must not be added.**

## Direct

| Module | Licence | Why |
| --- | --- | --- |
| `github.com/extism/go-sdk` | BSD-3-Clause | Extism JSON I/O ABI and per-call guest lifecycle for the G1 sandbox |
| `github.com/tetratelabs/wazero` | Apache-2.0 | Pure-Go WebAssembly execution, compilation cache, memory ceilings and deadline interruption |
| `github.com/tetratelabs/wabin` | Apache-2.0 | Builds small, auditable Wasm conformance fixtures directly in Go tests |
| `github.com/fsnotify/fsnotify` | BSD-3-Clause | Portable filesystem notifications for atomic, debounced configuration reloads |
| `github.com/expr-lang/expr` | MIT | Parses and evaluates the deliberately constrained declarative integration expression language; D3 supplies resource accounting |
| `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | One JSON Schema implementation validates configuration, manifests, lock records and widget documents — the same schema files the editor and the frontend use |
| `golang.org/x/time` | BSD-3-Clause | Token-bucket rate limiting per connection (`internal/connections`) — the canonical implementation, not one hand-rolled here |
| `golang.org/x/crypto` | BSD-3-Clause | Argon2id verification for administrator passwords without implementing password hashing primitives locally |
| `gopkg.in/yaml.v3` | Apache-2.0 / MIT | YAML parsing with node positions, so configuration errors can carry `file:line:col` |
| `modernc.org/sqlite` | BSD-3-Clause | Pure-Go SQLite driver for persistent card state, scheduling, sessions, events, and caches |

## Indirect

| Module | Licence |
| --- | --- |
| `github.com/dylibso/observe-sdk/go` | Apache-2.0 |
| `github.com/gobwas/glob` | MIT |
| `github.com/ianlancetaylor/demangle` | BSD-3-Clause |
| `go.opentelemetry.io/proto/otlp` | Apache-2.0 |
| `google.golang.org/protobuf` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |
| `golang.org/x/text` | BSD-3-Clause |
| `github.com/dustin/go-humanize` | MIT |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/ncruces/go-strftime` | MIT |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause |
| `golang.org/x/exp` | BSD-3-Clause |
| `modernc.org/libc` | BSD-3-Clause |
| `modernc.org/mathutil` | BSD-3-Clause |
| `modernc.org/memory` | BSD-3-Clause |

## Frontend

Build-time only; nothing here is shipped inside the Go binary except the compiled bundle.

| Package | Licence |
| --- | --- |
| `svelte`, `@sveltejs/vite-plugin-svelte` | MIT |
| `vite` | MIT |
| `typescript` | Apache-2.0 |
| `svelte-check` | MIT |
| `json-schema-to-typescript` | MIT |
| `vitest` | MIT |
| `@testing-library/svelte` | MIT |
| `jsdom` | MIT |
| `@testing-library/jest-dom` | MIT |
| `@tsconfig/svelte` | MIT |
| `concurrently` | MIT |
| `@playwright/test` | Apache-2.0 |

## Rust plugin SDK and first-party plugins

These dependencies are compiled into the Apache-2.0 WASM examples and
first-party plugins, not into the Go host.

| Crate | Licence |
| --- | --- |
| `extism-pdk` | BSD-3-Clause |
| `serde` | MIT OR Apache-2.0 |
| `serde_json` | MIT OR Apache-2.0 |

## Embedded offline icon pack

| Source | Version | Licence |
| --- | --- | --- |
| Homarr Labs Dashboard Icons | commit `c1f7e6b91c9e23317ac72b81f0ee3f9a4ac14aec` | Apache-2.0 |
| Material Design Icons (`@mdi/svg`) | 7.4.47 | Apache-2.0 |
| Simple Icons | 16.29.0 | CC0-1.0 (trademark rights are not waived) |

[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) is the generated companion to this file: it
covers the shipped frontend bundle (read from Rollup's real module graph, so it lists what the
browser receives rather than the build tooling above) and the complete locked Rust dependency
graph rather than only the direct crates. This file stays the curated policy record — it is where
a dependency's justification lives, and where the compatibility rules above are enforced.
