# S2 — Upstream reality check: Immich and Jellyfin

**Status:** specification pass complete; live-server pass outstanding.
**Blocks:** E3 (Immich vertical slice), G4 (Jellyfin WASM plugin).

The spike exists to stop us discovering in E3 that an endpoint does not work the way the manifest
assumed. Two of the findings below would have done exactly that.

## What was checked, and how

Against the authoritative machine-readable contracts, not documentation prose:

| Source | Version | Retrieved |
| --- | --- | --- |
| `immich-app/immich/open-api/immich-openapi-specs.json` | 3.2.0-rc.0, 191 paths | 2026-09-05 |
| `api.jellyfin.org/openapi/jellyfin-openapi-stable.json` | 12.0.0, 294 paths | 2026-09-05 |
| `immich/server/src/controllers/*.controller.ts` | main | 2026-09-05 |

Everything below marked **[spec]** is settled. Everything marked **[live]** cannot be settled from a
specification and needs a real server — see [Outstanding](#outstanding-needs-a-live-server).

---

## Immich

### F1 — `/server/statistics` is admin-only **[spec, high impact]**

```ts
@Authenticated({ permission: Permission.ServerStatistics, admin: true })
getServerStatistics(): Promise<ServerStatsResponseDto>
```

A normal API key gets 403. The manifest previously made this the default operation, so the
flagship declarative integration would have failed for every user who did not hand the dashboard an
**admin** key — the precise opposite of the project's least-authority claim.

**Resolved:** the default `recent-assets` operation now uses `GET /api/assets/statistics`, which
needs only `Permission.AssetStatistics` and is scoped to the key's own user. Its response is
`{images, total, videos}` — no byte usage. Disk usage moved to a separate `server-statistics`
operation that an administrator approves knowingly, and which the example lock deliberately does
**not** approve. That is the approval model working as designed: approving a subset of what a
manifest requests is the normal case.

### F2 — Search and thumbnail contracts confirmed **[spec]**

- `POST /api/search/metadata` — body `size` (default 250, **max 1000**), `page`, `order` (`asc`|`desc`),
  `visibility` (`timeline`|`archive`|`hidden`|`locked`), 45 fields in total. Response envelope is
  `{albums, assets: {items, count, total, nextPage, nextCursor, facets}}`, so the manifest's
  `recent.assets.items` mapping is correct. The manifest now also pins `visibility: timeline`, so
  archived and hidden photos never reach a wall-mounted dashboard.
- `GET /api/assets/{id}/thumbnail` — `size` is `original|fullsize|preview|thumbnail`; the manifest's
  `preview` is right. `AssetResponseDto` carries `id`, `originalFileName`, `fileCreatedAt`, `type`
  and `thumbhash` (a blur placeholder we can use in B4).
- Authentication is `x-api-key` in a header, as configured. Bearer and cookie are also accepted; we
  use neither.

### F3 — The thumbnail endpoint declares `application/octet-stream` **[spec, needs live confirmation]**

The spec says the 200 response of `viewAsset` is `application/octet-stream`, not `image/*`. The
asset-proxy contract ([01-architecture.md §7](../01-architecture.md)) says the response's
`Content-Type` must match `image/(jpeg|png|webp|gif|avif)` or be refused.

If the running server really sends `application/octet-stream`, **every Immich photo would be
rejected by our own proxy.** The contract already says "sniffed, not trusted", and this is why:
the guard must be *content* sniffing with the declared type as a hint, never a header equality
check. Confirm what the server actually sends before E1 hardens that path.

---

## Jellyfin

### F4 — `/Users/{userId}/Items` no longer exists **[spec, high impact]**

Jellyfin 12 removed it. The routes the manifest declared (`/Users`, `/Users/*/Items`) would have
404'd on every call, and the WASM plugin in G4 would have been written against an API that is gone.

**Resolved:** the manifest now declares `/Users/Me` (one request, no admin needed, replacing "list
users then pick one"), `/Items/Latest` (`GetLatestMedia` — purpose-built for recently-added, so no
sort-and-filter dance), `/Items` for counts, and `/Sessions` for active streams.

### F5 — Item images need no authentication **[spec, design input]**

`GET /Items/{itemId}/Images/{imageType}` is the one endpoint in the whole surface with
`security: null`, and it returns `image/*`. Posters are public on any reachable Jellyfin.

This does not remove the asset proxy from the picture, but it changes *why* it is there: for
Jellyfin the proxy buys caching and keeps the server's address out of the browser, not credential
injection. Worth stating plainly rather than implying every asset route hides a secret.

### F6 — The auth header we configured is the legacy one **[spec]**

The spec declares exactly one scheme, `CustomAuthentication`: an API key in the **`Authorization`**
header. `X-Emby-Token` is a fallback that Jellyfin 10.11+ can disable and that is slated for
removal. The example connection now sends `Authorization: MediaBrowser Token="…"`.

### F7 — Jellyfin can answer in PascalCase or camelCase **[spec, affects mapping]**

Responses advertise three media types: `application/json`, `application/json; profile="CamelCase"`
and `application/json; profile="PascalCase"`. `BaseItemDto` is defined with PascalCase properties
(`Id`, `Name`, `ProductionYear`, `ImageTags`, `SeriesName`, `UserData`).

An integration that maps `.name` instead of `.Name` will silently produce empty cards. G4 must pin
the casing by requesting one profile explicitly rather than relying on the server default — and the
golden fixtures must be captured with that same `Accept` header.

---

## Consequences already applied

| Change | Where |
| --- | --- |
| Immich default operation moved off the admin endpoint; `usage` split into an admin-only operation | `plugins/immich/manifest.yaml` |
| `visibility: timeline` pinned so archived/hidden photos never render | `plugins/immich/manifest.yaml` |
| Jellyfin routes rewritten for Jellyfin 12 (`/Users/Me`, `/Items/Latest`, `/Items`, `/Sessions`) | `plugins/jellyfin/manifest.yaml` |
| Jellyfin connection switched to the `Authorization` scheme | `examples/veduta.yaml` |
| Lock approves the non-admin Immich routes only, demonstrating subset approval | `examples/veduta.lock.yaml` |
| `veduta manifest digest` added, so a lock can be regenerated rather than guessed | `cmd/veduta` |

## Outstanding: needs a live server

A specification cannot answer these, and E1/E3/G4 depend on them. Run
[`hack/capture-upstream-fixtures.sh`](../../hack/capture-upstream-fixtures.sh) against a real
instance; it writes raw responses to `testdata/upstream/` for review before they are committed.

1. **The actual `Content-Type` of an Immich thumbnail** (F3). Decides whether the asset proxy's
   guard can compare headers at all, or must sniff exclusively.
2. **Response sizes** — a 250-asset search response, and a thumbnail at `preview` size — against
   the `inputMB` and asset size caps.
3. **Redirect behaviour**, if any, on either service. The connection default is `MaxRedirects: 0`.
4. **Jellyfin's default JSON casing** for a plain `Accept: application/json` (F7), and whether
   requesting a profile explicitly is honoured.
5. **Whether `/Items/Latest` returns `ImageTags.Primary`** for every item, or whether some items
   need a fallback to a series or album image.
6. **Pagination shape in practice** — `nextPage` as a string or a number, `total` versus `count`.
7. **Immich `thumbhash` presence** — if it is always populated, B4's blur-up placeholder is free.

Fixtures must be reviewed before committing: real filenames, album names, user ids and library
contents are personal data, and a golden fixture lives in the repository forever.
