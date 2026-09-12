# Integration authoring

An integration declares what it needs; it does not grant itself authority. Veduta loads `manifest.yaml`, validates it, computes its canonical digest, and intersects its requests with the administrator's digest-bound entry in `veduta.lock.yaml`. The core owns connections and credentials. Integrations refer only to abstract slots.

## Choosing a mechanism

Three rungs, in order. Start at the lowest one that fits — **most integrations never leave the first two, and neither requires a compiler, a toolchain, or Rust.**

1. **An `http-json` card, for a single endpoint.** No manifest, no new files: name a connection, a path, and the values to show. See the `adguard` card in [`examples/veduta.yaml`](../examples/veduta.yaml). It gets the same expression and resource budgets as any declarative integration.
2. **A declarative manifest, for most integrations.** Up to eight requests, later requests using earlier results, conditional steps, structured bodies, filtering and mapping over JSON, iteration into cards and lists, broker-minted image references, and declared signals for history and alerts. All three of `plugins/immich`, `plugins/glances` and `plugins/beszel` are this, and `plugins/glances` alone makes six requests. Nothing here is compiled; a manifest is data.
3. **A WASM plugin, only when logic genuinely demands it.** Loops with per-item requests, non-JSON parsing, error recovery, or branching the bounded template grammar cannot express. Rust is the supported SDK today; it is not a requirement for the rungs above, and the runtime itself is language-neutral — see [decisions/0001-backend-language.md](decisions/0001-backend-language.md).

All three use the same capability broker, credential boundary, limits, output validation, and approval flow. Moving up a rung buys expressiveness, not authority: a WASM plugin can do nothing a manifest could not ask for.

## Manifest structure

Start with a first-party example under [`plugins/`](../plugins/). A manifest contains:

- `metadata`: stable ID, display name, SemVer version, and optional project metadata.
- `spec.runtime`: `declarative` or `wasm` for an external integration.
- `spec.slots`: abstract HTTP or Docker connections that an administrator binds on each card.
- `spec.capabilities`: an explicit request for `http`, `assets`, `cache`, `events`, or `log` access.
- `spec.routes`: each allowed method and canonical path, bound to a slot and use. Query keys, content type, body size, and reason are part of the approved route identity.
- `spec.limits`: bounded requests, response bytes, expression work, output size, memory, and fuel.
- `spec.operations`: operation IDs, declared signals, and runtime-specific implementation.

The authoritative shape and loader-only checks are in [`schemas/plugin-manifest.v1.schema.json`](../schemas/plugin-manifest.v1.schema.json). Keep routes narrow and explain each one. A manifest change changes its digest and disables the integration until the new permission diff is approved.

Validate trust state while authoring:

```sh
./veduta manifest digest plugins/my-integration/manifest.yaml
./veduta integration list --config veduta.yaml
./veduta integration diff my-integration --config veduta.yaml
```

## Declarative integrations

A declarative operation supplies a bounded `pipeline` and `output`. Each pipeline request names a declared slot and a route already present in the manifest. Expressions select and transform response data. Output templates produce a Widget Document, and declared signals expose typed values for history and rules.

An upstream field that is sometimes missing needs `{ if: …, then: … }`, which omits the key or list element entirely rather than emitting `null`:

```yaml
subtitle:
  if:   { expr: "item.productionYear != nil" }
  then: { expr: "string(item.productionYear)" }
```

Reaching for `{ expr: "string(item.productionYear)" }` alone is the trap: it renders the literal text `<nil>` when the field is absent, and it validates, so nothing catches it before a user sees it. A bare `{ expr: item.productionYear }` is at least loud — `null` fails document validation — but it fails the whole card, not just that field. The condition must be a boolean; a truthy value is refused rather than coerced.

[`plugins/immich/manifest.yaml`](../plugins/immich/manifest.yaml) demonstrates asset routes and image output. [`plugins/glances/manifest.yaml`](../plugins/glances/manifest.yaml) demonstrates metrics and signals. [`plugins/beszel/manifest.yaml`](../plugins/beszel/manifest.yaml) demonstrates an authorised record selected from a collection.

Run the focused loader and runtime tests while editing a manifest:

```sh
go test ./internal/integrations/manifestload ./internal/integrations/declarative
./veduta --check-config --config veduta.yaml
```

The loader rejects duplicate YAML keys, excessive size or nesting, undeclared slots and signals, uncovered requests, invalid templates, and budgets over the hard caps before the integration can run.

## WASM integrations with Rust

Rust is the supported compiled guest language — the one this repository ships a typed SDK for and has measured. It is not what the runtime enforces: the sandbox checks exports, the import allowlist, the ABI, the limits and the approved module hash, none of which are Rust-specific. [decisions/0001-backend-language.md](decisions/0001-backend-language.md) records what another toolchain would have to demonstrate.

The SDK targets `wasm32-unknown-unknown` and produces modules without WASI imports, so filesystem, environment, clocks, sockets, and process APIs are unavailable unless Veduta exposes a specific broker operation.

Build the starter module:

```sh
rustup target add wasm32-unknown-unknown
make -C plugins hello
./veduta plugin validate plugins/examples/hello/hello.wasm
```

Copy [`plugins/examples/hello`](../plugins/examples/hello), implement the exported `invoke` function using [`sdk/rust`](../sdk/rust), and declare every operation and permission in the manifest. An operation-strict plugin must return the SDK validation document for a validation invocation; the CLI uses that path without granting broker capabilities.

For a release build, calculate the module's SHA-256 digest, write it to `spec.sha256`, and validate again:

```sh
sha256sum plugins/my-integration/my-integration.wasm
./veduta plugin validate plugins/my-integration/my-integration.wasm
```

The validator applies production import restrictions and resource limits, instantiates the module without WASI, and smoke-invokes the ABI. [`plugins/jellyfin`](../plugins/jellyfin) is the complete example for multiple broker requests, typed mapping, asset references, signals, and partial-data notices.

Before publishing an integration, test error and partial-data paths with captured, reviewed fixtures; verify all output through the Widget Document validator; and review the exact `integration diff` an administrator will see.

## The ABI is experimental

Veduta is pre-1.0, and the WebAssembly guest ABI, the host functions exposed by [`sdk/rust`](../sdk/rust), and the manifest schema **will change between releases**. This is stated plainly rather than discovered: a WASM integration you publish today should be expected to need a rebuild, a new module hash, and a fresh approval on upgrade.

That is a deliberate consequence of the trust model rather than an accident of it. `veduta.lock.yaml` pins the manifest digest and the module SHA-256, so a module built against an older ABI is refused at load with a printed permission diff — it fails closed instead of running against host functions whose meaning has shifted. Declarative integrations are the more stable surface: they are data, not compiled code, and are the recommended starting point unless an integration genuinely needs cross-request logic.
