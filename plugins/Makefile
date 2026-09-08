.PHONY: all hello hello-build jellyfin jellyfin-build verify clean

# Rust records source paths in panic metadata. Remove runner-specific paths
# from distributable builds even though LLVM section ordering is not stable.
export RUSTFLAGS := --remap-path-prefix=$(abspath ..)=/src --remap-path-prefix=$(HOME)=/build

all: hello jellyfin

hello: hello-build
	cp examples/hello/target/wasm32-unknown-unknown/release/veduta_hello.wasm examples/hello/hello.wasm

hello-build:
	cargo build --manifest-path examples/hello/Cargo.toml --release --target wasm32-unknown-unknown

jellyfin: jellyfin-build
	cp jellyfin/target/wasm32-unknown-unknown/release/veduta_jellyfin.wasm jellyfin/jellyfin.wasm

jellyfin-build:
	cargo build --manifest-path jellyfin/Cargo.toml --release --target wasm32-unknown-unknown

verify:
	@echo "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' examples/hello/manifest.yaml)  examples/hello/hello.wasm" | sha256sum --check
	@echo "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' jellyfin/manifest.yaml)  jellyfin/jellyfin.wasm" | sha256sum --check
	$(MAKE) hello-build jellyfin-build

clean:
	cargo clean --manifest-path examples/hello/Cargo.toml
	cargo clean --manifest-path jellyfin/Cargo.toml
	rm -f examples/hello/hello.wasm
	rm -f jellyfin/jellyfin.wasm
