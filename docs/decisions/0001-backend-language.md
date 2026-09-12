# Backend language: Go core; optional WASM plugins with an initially Rust-only SDK

Status: accepted, 2026-09-08. Amended 2026-09-12 (scope of the guest-language half; see the
amendment at the end). Revisit after 0.1 only if a trigger below is met.

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

## Amendment, 2026-09-12: what "Rust plugins" does and does not mean

The original title, "Go core, Rust plugins", overstated Rust's role, and this document is the right
place to correct it because it is the document that decided both halves.

**Rust is the supported SDK for compiled WASM plugins. It is not a requirement for writing a Veduta
integration, and the runtime is not Rust-aware.** Most integrations are YAML and always were:
`docs/01-architecture.md` D12 makes declarative the primary extension mechanism for 0.1, and three
of the four shipped integrations use it. Nobody connecting a service should need a Rust toolchain,
and the authoring guide now leads with that ladder — an `http-json` card for a single endpoint, a
declarative manifest for most integrations, and WASM only where real programming logic is needed.

The runtime cannot tell which language produced a module. It enforces the required exports, the
import allowlist (no WASI), ABI-compatible input and output, the resource limits, and the approved
module hash. Any toolchain that satisfies those could be supported. Rust is supported today because
this repository ships a typed SDK for it and has measured its output — about 250 KiB, sub-second
cold validation, no WASI imports — not because the sandbox requires it.

Another guest language becomes supported when a prototype demonstrates, with measurements:

- `wasm32-unknown-unknown` (or equivalent) output with no forbidden imports,
- module size and cold-validation time in the same class as the Rust guest, within the 32 MiB cap,
- a pass through the G1 conformance suite and the S1a ARM budgets,
- and a maintained PDK, so the ABI does not become this project's to own.

Go is the obvious candidate and is already tracked in `docs/03-backlog.md` ("Go plugin SDK requires
a maintained WASI-free toolchain"); it fails the first two criteria today. Python is further away:
the official Extism Python PDK packages an interpreter into the module and requires WASI even when
the plugin needs no system access, which conflicts directly with the no-WASI sandbox and the
Pi-class size and latency targets. Running Python as a subprocess or sidecar would mean owning
interpreter installation, process lifecycle, credential-safe IPC, filesystem and network
sandboxing, and ARM packaging — and would abandon the single-binary promise (D1) for a language
preference. Neither is planned; both are welcome as prototypes meeting the criteria above.

Nothing about the host decision changes: the core stays Go, and the revisit triggers above stand.
