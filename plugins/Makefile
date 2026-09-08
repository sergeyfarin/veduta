.PHONY: all hello jellyfin verify clean

all: hello jellyfin

hello:
	cargo build --manifest-path examples/hello/Cargo.toml --release --target wasm32-unknown-unknown
	cp examples/hello/target/wasm32-unknown-unknown/release/veduta_hello.wasm examples/hello/hello.wasm

jellyfin:
	cargo build --manifest-path jellyfin/Cargo.toml --release --target wasm32-unknown-unknown
	cp jellyfin/target/wasm32-unknown-unknown/release/veduta_jellyfin.wasm jellyfin/jellyfin.wasm

verify: hello jellyfin
	@test "$$(sha256sum examples/hello/hello.wasm | awk '{print $$1}')" = "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' examples/hello/manifest.yaml)"
	@test "$$(sha256sum jellyfin/jellyfin.wasm | awk '{print $$1}')" = "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' jellyfin/manifest.yaml)"

clean:
	cargo clean --manifest-path examples/hello/Cargo.toml
	cargo clean --manifest-path jellyfin/Cargo.toml
	rm -f examples/hello/hello.wasm
	rm -f jellyfin/jellyfin.wasm
