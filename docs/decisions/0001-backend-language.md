# Backend language: Go core, Rust plugins

Status: accepted, 2026-09-08. Revisit after 0.1 only if a trigger below is met.

## Decision

Keep the Veduta backend in Go through 0.1 and use Rust as the supported language
for shipped WASM plugins. Do not start a full Go-to-Rust rewrite.

This is a product and delivery decision, not a claim that Go is generally a
better language than Rust. Rust is the better fit on the guest side because it
produces small WASI-free modules. Go is the better fit for the current host
because its implementation and release properties are already proven in this
repository.

## Evidence from this codebase

At this decision point the core contains 13,678 lines of non-test Go and 10,831
lines of Go tests across 73 test files. Those tests cover the highest-risk
boundaries: secret handling, pinned outbound requests, route grants, approval
digests, asset authorization, reload fencing, persistence, scheduling, and the
WASM sandbox. Rewriting them would recreate already-closed security defects
before adding user-visible capability.

The existing host cross-compiles with `CGO_ENABLED=0` to the four release
targets, including Linux ARMv7. Stripped local builds are 21 MiB for Linux
amd64 and 19 MiB for ARMv7. Its SQLite and WASM engines are pure Go. A Rust
host can target ARMv7, but an equivalent stack would need new cross-build and
runtime proof: common SQLite bindings compile C, and Extism's Rust host uses
Wasmtime. Wasmtime can fall back to its Pulley interpreter on 32-bit targets,
but this repository has no ARMv7 performance or release evidence for that
combination.

Veduta is mostly an I/O-bound HTTP service. The expected runtime benefits of a
rewrite do not address a measured bottleneck. Both languages prevent the class
of memory bugs relevant to ordinary safe code; the stronger ownership model in
Rust does not compensate for reimplementing the capability and credential
boundaries without a concrete defect it would prevent.

The stock Go Extism guest measured during G3 was 4.3 MiB, took about five
seconds to cold-validate on the development host, and imported 17 WASI
functions. The Rust guest is about 250 KiB, cold-validates in under a second,
and imports no WASI. This supports Rust plugins without implying a Rust host.

## Options declined

- **Rewrite now in Rust:** delays the remaining H-L product work and reopens
  mature security-sensitive behavior for no measured user benefit.
- **Incrementally run two backend implementations:** creates duplicate config,
  broker, storage, and lifecycle paths. These boundaries must have one owner.
- **Use Rust only for new core packages:** creates an FFI/process boundary
  inside a small single binary and complicates ARM releases.
- **Enable restricted WASI for Go guests:** widens the guest surface solely to
  accommodate one toolchain. The strict import policy is simpler to audit.
- **Maintain a custom WASI-free Go PDK:** adds long-term ABI/toolchain ownership
  while Rust already meets the size and sandbox requirements.

## Revisit triggers

Re-evaluate the host language after 0.1 if at least one trigger is supported by
measurements and a migration prototype:

- Go GC latency or resident memory breaks a written target on Pi-class hardware.
- A required host capability has no safe, maintained, cgo-free Go implementation.
- ARMv7 is dropped and a Rust Extism/SQLite release prototype passes the full
  contract suite with comparable binary size and operational simplicity.
- Maintaining the Go core becomes materially harder than staffing a rewrite.

Any future proposal must migrate one vertical slice behind the existing HTTP,
schema, manifest, and golden-document contracts before changing this decision.
