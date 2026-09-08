# Veduta Rust plugin SDK

This Apache-2.0 crate provides typed Widget Document builders and safe wrappers
for Veduta's capability-broker host functions. Use `extism_pdk::plugin_fn` to
export `invoke`, accept `Json<veduta_sdk::Invocation>`, and return
`Json<veduta_sdk::Document>`.

Build with `cargo build --release --target wasm32-unknown-unknown`. The target
needs no WASI imports and is the preferred Veduta plugin toolchain.
