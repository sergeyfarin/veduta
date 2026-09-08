.PHONY: all hello verify clean

all: hello

hello:
	cargo build --manifest-path examples/hello/Cargo.toml --release --target wasm32-unknown-unknown
	cp examples/hello/target/wasm32-unknown-unknown/release/veduta_hello.wasm examples/hello/hello.wasm

verify: hello
	@test "$$(sha256sum examples/hello/hello.wasm | awk '{print $$1}')" = "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' examples/hello/manifest.yaml)"

clean:
	cargo clean --manifest-path examples/hello/Cargo.toml
	rm -f examples/hello/hello.wasm
