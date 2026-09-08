# Jellyfin integration

This first-party Rust plugin resolves the authenticated Jellyfin user, fetches
recent items, counts movies and shows, counts active playback sessions, and
mints broker-owned poster references. It never receives the connection URL or
API key.

Build and validate it from the repository root:

```sh
make -C plugins jellyfin
go run ./cmd/veduta plugin validate plugins/jellyfin/jellyfin.wasm
```

The plugin requests Jellyfin's explicit camelCase JSON profile and still accepts
PascalCase fields for compatibility. Its checked-in test responses are shaped
from the Jellyfin 12 OpenAPI contract. Capturing and reviewing responses from a
real server remains the S2 deployment-validation backlog item.
