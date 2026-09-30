# Veduta Rust plugin SDK

> **Frozen.** The WebAssembly path is maintained but not extended: no new host functions, ABI
> changes or integrations, and it is removed at 1.0 unless a real integration needs it. Prefer a
> declarative manifest; see [decision 0005](../../docs/decisions/0005-wasm-frozen.md).

This Apache-2.0 crate provides typed Widget Document builders and safe wrappers
for Veduta's capability-broker host functions. Use `extism_pdk::plugin_fn` to
export `invoke`, accept `Json<veduta_sdk::Invocation>`, and return
`Json<veduta_sdk::Document>`.

Return `Document::validation()` when `Invocation::is_validation()` is true.
This reserved operation must not call host functions; the CLI uses it to prove
that the module can instantiate and return a bounded v1 document.

Build with `cargo build --release --target wasm32-unknown-unknown`. The target
needs no WASI imports and is the preferred Veduta plugin toolchain.
