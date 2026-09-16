# Jellyfin remains the WASM proof case for 0.1

Status: accepted for retaining Jellyfin in 0.1, 2026-09-16. Further WASM
development is parked; its product requirements and value are under review.

## Decision

Ship the existing first-party Rust/WASM Jellyfin integration in 0.1. Close the
proof-case question without requiring a declarative rewrite or a replacement
integration before release.

Jellyfin demonstrates a real integration running through the WASM runtime, Rust
SDK, capability-mediated HTTP calls and broker-owned poster references. It does
not need to demonstrate behavior that is impossible in the declarative DSL.

## Evidence and tradeoff

The [S2 live pass](../spikes/s2-upstream-reality-check.md) removed the original
user-resolution premise. The implementation now makes four requests: a sorted
recent-items query, two typed counts and a session list. The declarative DSL can
express that flow, and its asset metadata, conditional omission and non-2xx
failure gaps have since been closed.

Keeping WASM preserves the tested CamelCase/PascalCase compatibility and avoids
changing a live-validated integration for release without a measured user
benefit. The cost is retaining the Rust build and WASM runtime overhead for this
integration. Native ARM64 CI already measures the real Jellyfin module's runtime
budgets; ARMv7 performance remains a separate deferred item.

The golden and scenario tests in `internal/integrations/wasm/jellyfin*_test.go`
cover document output, request shape, missing posters, missing production years,
PascalCase responses and failed upstream requests. The runtime budget tests use
the shipped module. These are concrete reasons to retain it as a proof case.

## Revisit conditions

Consider a declarative migration after 0.1 when it offers a measured maintenance
or runtime benefit. Validate it against the existing golden and scenarios,
explicitly decide whether PascalCase tolerance is preserved, and retain a real
WASM integration workload for runtime and hardware-budget coverage. No migration
or new proof-case project is scheduled by this decision.

## Scope clarification: declarative first

The Immich memories operation is implemented declaratively. Do not create a WASM
example solely to exercise WASM. Further WASM development is parked while we
review whether it adds enough user value to justify its complexity and cost.
No replacement proof case or new WASM feature is required for 0.1 or automatically
scheduled for 0.2. The existing Jellyfin implementation and its security/regression
coverage remain in place.

A review after 0.1 must start with a concrete need, compare declarative and upstream
alternatives, and account for maintenance, resource and permission costs. Keeping
WASM parked or reducing its role are valid outcomes. Candidate integrations and
review criteria are recorded in the
[open requirements review](../03-backlog.md#does-wasm-add-enough-value-to-justify-further-development).
Visualization work is independent of this decision.
