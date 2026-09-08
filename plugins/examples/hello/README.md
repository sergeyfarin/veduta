# Hello WASM plugin

From the repository root:

```sh
make -C plugins hello
go run ./cmd/veduta plugin validate plugins/examples/hello/hello.wasm
```

Copy this directory, change the package and manifest metadata, and implement
`invoke`. The SDK's host functions can only use capabilities and routes declared
in the manifest and approved in `veduta.lock.yaml`.
