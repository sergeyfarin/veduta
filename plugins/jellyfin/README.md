# Jellyfin integration

This first-party Rust/WASM plugin fetches library-wide recent items, counts
movies and shows, counts active playback sessions, and
mints broker-owned poster references. It never receives the connection URL or
API key.

Build and validate it from the repository root:

```sh
make -C plugins jellyfin
go run ./cmd/veduta plugin validate plugins/jellyfin/jellyfin.wasm
```

The plugin requests Jellyfin's explicit camelCase JSON profile and still accepts
PascalCase fields for compatibility. Its checked-in test responses are shaped
from the Jellyfin 12 OpenAPI contract and the live-server pass recorded in
[S2](../../docs/spikes/s2-upstream-reality-check.md), completed on 2026-09-09.
Requests use the connection’s API key without resolving a user.

Jellyfin remains the shipped WASM proof case for 0.1; see
[decision 0003](../../docs/decisions/0003-jellyfin-wasm-proof-case.md).
