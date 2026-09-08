# S1 / G1 — WASM sandbox decision

The G1 implementation adopts Extism Go SDK v1.7.1 on wazero v1.9.0 (D6),
with compiled code shared through wazero's digest/version-keyed cache and a fresh
Extism instance for every invocation. Pooling is unnecessary for correctness and
would require a separate proof that guest globals, memory and runtime state reset.
The concurrent isolation test deliberately traps on a second use of a guest.

`wasm.New(ctx, cacheDir)` owns the cache and loaded compiled instances. An empty
cache directory selects memory-only caching. `Runtime.Close` waits for bounded
invocations, closes compiled modules, then the cache. `Instance.Close` is idempotent;
closed instances reject further calls. Callers must close the runtime at shutdown.

The executable manifest loader retains `spec.module` and `spec.sha256`. Load checks
both manifest and module approval pins, version, runtime and resource ceilings. It
reads the module once (32 MiB ceiling) through `os.Root`, rejecting paths and symlinks
that escape the installed manifest directory. It hashes those exact bytes before
any compilation, including a cache hit. A preflight compile validates the import
surface and `invoke() -> i32` export without executing guest code.

The ABI is Extism JSON in / JSON out: `invoke` receives
`{"operation":"stats","params":{},"now":"<UTC timestamp>"}` and returns a Widget
Document through `output_set`; non-zero status fails the invocation. Parameters
receive schema defaults and validation; documents pass the existing widget validator
and operation signal declarations. Grants and credentials are never included in
input JSON. G2 will bind broker functions using the invocation context.

G1 permits only Extism allocation, memory access, input, output and error functions, plus the five
G2 `extism:host/user` broker imports once present in a module.
Native HTTP (both `http_request` and the legacy `extism_http_request` spelling),
variables, configuration and native logging imports are rejected before instantiation.
`allowed_hosts` is also explicitly empty. WASI is disabled: filesystem, environment,
arguments, sockets and stdout/stderr imports cannot resolve. This is stricter than
mounting an empty WASI filesystem or capturing its output, and avoids the SDK's
`EXTISM_ENABLE_WASI_OUTPUT` environment override. Plugins must currently target a
WASI-free Extism build. G2's `veduta_log` provides bounded broker logging.

Memory limits apply to each linear memory, including the Extism kernel and a guest's
own memory; they are not an aggregate process RSS limit. The invocation context
covers instantiation (including Wasm start sections), execution and output retrieval.
Caller deadlines and cancellation can only shorten the approved timeout. A wazero
function listener checks the full i64 `output_set` length before the SDK narrows it
or allocates a Go copy; the widget validator checks the output limit again. The
listener reads the invocation's limit, so cached code cannot retain another
integration's output allowance.

`internal/integrations/wasm/runtime_test.go` builds executable Wasm fixtures in Go
using wabin. CI's existing `go test -race ./...` includes the conformance suite without
a guest compiler or downloaded binaries. The tests cover forbidden imports, pin
mismatches, corrupted/oversized modules, path escapes, deadline kills, memory and
output ceilings, document validation, isolated concurrent calls and lifecycle.

Remaining phase boundaries: plugin SDKs and build recipes belong to G3; Jellyfin and application
runtime selection/lifecycle wiring belong to
the G4 vertical slice. This decision does **not** claim the original S1a hardware
acceptance: ARM cold/warm timings, Pi RSS measurements and guest toolchain size
comparisons still require the target hardware. Cross-compilation alone cannot
establish the <50 ms warm-call / <20 MiB RSS criteria.

Validation for this implementation: `go vet ./...` and `go test -race ./...`
passed, as did golangci-lint for the changed WASM and manifest-loader packages.
The WASM test executable cross-compiled with CGO disabled for linux/arm64 and
linux/arm (GOARM=7). Repository-wide golangci-lint still reports existing issues in
other packages; those are outside G1's scope.
