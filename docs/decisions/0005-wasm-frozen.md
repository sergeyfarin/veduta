# WebAssembly integrations are frozen; formats go to the core

Status: accepted and **frozen**, 2026-09-30. It closes the requirements review opened on 2026-09-16
and is not reopened except under [Reopening](#reopening) below. It narrows
[decision 0003](0003-jellyfin-wasm-proof-case.md), which stands for what it decided about Jellyfin.

## Context

The WASM path was built in Phase G as the escape hatch for integration logic a declarative manifest
cannot express: a sandboxed guest (wazero via Extism, no WASI) reaching the world only through the
same capability broker as a manifest. It works, and it is safe by the tests that exist for it. The
review asked a different question: whether it earns what it costs.

It has not been used. Every integration written since 0.1 is declarative - Arcane, Home Assistant,
Proxmox, Dockhand, Beszel, Immich memories - and Phase M2 closed the last gap that made the manifest
language look insufficient, by letting a card parameter select an upstream object in a path. The one
WASM integration, Jellyfin, is WASM because it was the proof case, not because it needs to be;
decision 0003 says so. No user has asked for WASM.

It is not free. Measured on 2026-09-30:

| | |
|---|---|
| Runtime | 769 lines of Go and 1,283 of tests |
| Guest side | about 800 lines of Rust: the SDK, Jellyfin and the example |
| Binary | about 1.2 MB of a 23 MB binary is wazero and Extism |
| Dependencies | Extism brings OpenTelemetry, protobuf and testify into the module graph |
| Build and CI | a pinned Rust toolchain, an 83-second CI job, Rust crates in the notices generator |
| Security | an in-process compiler turning untrusted bytecode into machine code - the largest attack surface in the project, however well sandboxed |

Runtime performance is not the problem: about 2 ms and 1 MB per instance on arm64.

The candidate needs recorded during the review - a calendar agenda, RSS/Atom feeds, Prometheus
metrics, text weather feeds - share a shape. Each is a **format** a manifest cannot read, not logic a
manifest cannot express.

## Decision

**1. WASM is a frozen, experimental escape hatch.** What exists stays and stays correct: the runtime,
the Rust SDK, the conformance suite, the ARM64 budget test, `plugins/jellyfin` and the `hello`
example, and security fixes to any of them. Nothing is added: no new host functions, no ABI changes
or extensions, no new SDKs, no new WASM integrations, first-party or example, and no runtime
features. The guest ABI keeps its "experimental, will change" status, and a change to it needs this
decision reopened first.

**2. Formats a manifest cannot read are added to the core, as decoders.** A pipeline step already
decodes JSON inside node, depth and size budgets. A step that declares another format - ICS, XML for
RSS/Atom, Prometheus text - is decoded the same way, by one audited, fuzzed Go decoder under the same
budgets, into the tree a manifest's expressions already read. That serves every manifest author with
no toolchain, adds no untrusted code, and follows the rule the renderer already follows
([decision 0004](0004-card-expressiveness-and-the-presentation-contract.md)): a closed vocabulary of
vetted capabilities, grown deliberately, rather than an open door. Each decoder is its own change,
made when a concrete integration needs it; none is committed by this decision.

**3. Jellyfin stays WASM.** It is the only real workload for the sandbox and budget tests, and
rewriting it gains users nothing. Decision 0003's conditions for migrating it are unchanged.

**4. The Go SDK will not be built.** It existed only for a WASM path that grows; see
[the resolved entry](../archive/03-backlog-resolved.md#go-plugin-sdk-would-have-required-a-maintained-wasi-free-toolchain).

**5. armv7 is disclosed, not measured.** `docs/docker.md` says the armv7 image is not
performance-tested for WASM integrations. Buying hardware to measure a frozen path is not justified.

## Reopening

Only when **true value surfaces**: a concrete integration someone actually wants, that a declarative
manifest cannot express **and** that a core decoder or an upstream service cannot reasonably serve.
Interest in WASM as a technology, a wish for a showcase, or a need a decoder would meet are not
triggers.

Even then, reopening is a weighing, not an approval. The case has to set the value it delivers - who
it serves, and why nothing cheaper serves them - against the investment to build it and the
maintenance it adds: interface stability, SDK upkeep, the toolchain, CI time, the security surface,
and each supported architecture. Continuing the freeze is a valid outcome of that weighing.

**The 1.0 release is a checkpoint.** 1.0 promises stability, and that promise cannot honestly cover
an interface nobody depends on. If by then nothing has reopened this decision and no integration
beyond Jellyfin needs WASM, the path is removed rather than stabilised: Jellyfin moves to a
declarative manifest under decision 0003's conditions, and the runtime, SDK and dependencies go. If
something did, the interface is stabilised on the evidence of that use.

## Alternatives considered

- **Commit to WASM as a supported path** (stabilise the ABI, add a Go SDK, measure armv7, build an
  integration on it). The strongest form of the sandboxed-integrations pitch, and the largest
  ongoing cost - a stability promise, SDK upkeep and the compiler's attack surface - for a path with
  no demonstrated user. Rejected until value surfaces.
- **Remove it now.** Cheapest to maintain, and it may yet be the outcome at 1.0. Rejected for now
  because it reverses a core architectural choice on the evidence of two weeks, and adding it back
  would cost more than keeping it frozen until the checkpoint.
- **Keep it "parked".** What the project had since 2026-09-16: paying the running cost while
  promising nothing. This decision exists to end that state.
