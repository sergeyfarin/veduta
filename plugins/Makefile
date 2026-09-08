.PHONY: all hello jellyfin verify clean

# Rust records source paths in panic metadata. Normalise the runner-specific
# home directory so checked-in plugin hashes are reproducible across machines.
export RUSTFLAGS := --remap-path-prefix=$(HOME)=/build

all: hello jellyfin

hello:
	cargo build --manifest-path examples/hello/Cargo.toml --release --target wasm32-unknown-unknown
	cp examples/hello/target/wasm32-unknown-unknown/release/veduta_hello.wasm examples/hello/hello.wasm

jellyfin:
	cargo build --manifest-path jellyfin/Cargo.toml --release --target wasm32-unknown-unknown
	cp jellyfin/target/wasm32-unknown-unknown/release/veduta_jellyfin.wasm jellyfin/jellyfin.wasm

verify: hello jellyfin
	@echo "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' examples/hello/manifest.yaml)  examples/hello/hello.wasm" | sha256sum --check
	@echo "$$(sed -n 's/.*sha256: "\([0-9a-f]*\)"/\1/p' jellyfin/manifest.yaml)  jellyfin/jellyfin.wasm" | sha256sum --check

clean:
	cargo clean --manifest-path examples/hello/Cargo.toml
	cargo clean --manifest-path jellyfin/Cargo.toml
	rm -f examples/hello/hello.wasm
	rm -f jellyfin/jellyfin.wasm
