# Building Veduta plugins

Rust is the supported compiled plugin language for Veduta 0.1. Its SDK produces
small `wasm32-unknown-unknown` modules with no WASI imports, preserving the
sandbox rule that all external effects pass through Veduta's capability broker.

Install a current Rust toolchain and the WebAssembly target, then build and
validate the starter plugin:

```sh
rustup target add wasm32-unknown-unknown
make -C plugins hello
go run ./cmd/veduta plugin validate plugins/examples/hello/hello.wasm
```

To start a plugin, copy `plugins/examples/hello`, change the Cargo package and
manifest metadata, and implement its exported `invoke` function. Declare every
slot, route, capability, and signal in `manifest.yaml`; an administrator must
approve the resulting manifest and module digests before Veduta loads it.

After a release build, calculate `sha256sum your-plugin.wasm` (or
`shasum -a 256` on macOS), put that digest in `spec.sha256`, and run the
validator again. The validator applies the production import policy and
resource limits, instantiates the module without WASI, and smoke-invokes its
ABI.

The Go core remains the supported Veduta host. The measured Go plugin toolchain
requires WASI and does not meet the strict guest profile; its revisit criteria
are recorded in [`docs/decisions/0001-backend-language.md`](../docs/decisions/0001-backend-language.md).
