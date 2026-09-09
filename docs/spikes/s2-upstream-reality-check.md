# S2 — Upstream reality check: Immich and Jellyfin

**Status:** specification pass complete; live-server pass run 2026-09-09 — Immich confirmed, Jellyfin
findings force a manifest/G4 rewrite (see [Live-server pass](#live-server-pass-2026-09-09) and
`docs/03-backlog.md`).
**Blocks:** E3 (Immich vertical slice) — cleared, cold-latency AC met. G4 (Jellyfin WASM plugin) —
still blocked pending the rewrite.

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

## Live-server pass (2026-09-09)

`hack/capture-upstream-fixtures.sh` was run against a live **Immich 3.1.0** and a live **Jellyfin
12.0.0** (`info.version` of the server's own `/api-docs/openapi.json`, 294 paths — the same
contract this spec pass used). Scrubbed Immich fixtures and the capture log are in
`testdata/upstream/`; see its `CAPTURE-NOTES.md`.

### The seven questions, answered

1. **Immich thumbnail `Content-Type` (F3):** `image/jpeg`, real JFIF/EXIF magic bytes — **not** the
   `application/octet-stream` the OpenAPI declares. E1's decision (sniff bytes with
   `image.DecodeConfig`, never trust the header) is right regardless; the header just isn't hostile
   on this server.
2. **Response sizes:** 6-asset `search/metadata` = 4.9 KB (so ~250 assets ≈ 200 KB, inside
   `inputMB: 4`); `preview` thumbnail = 167 KB. No problem for the caps.
3. **Redirects:** none observed on either service. `MaxRedirects: 0` is safe.
4. **Jellyfin JSON casing (F7):** default `Accept` → **PascalCase**; `Accept: application/json;
   profile="CamelCase"` → **camelCase**, and the explicit profile **is** honoured. G4 must pin one
   profile in the request `Accept` header and capture goldens with the same header.
5. **`ImageTags.Primary` coverage:** present on every movie item — from both `/Items/Latest` and
   the `/Items` query. No series/album-image fallback needed for a movie card. (But `/Items/Latest`
   is not the right endpoint — see below.)
6. **Immich pagination shape:** `nextPage` is the **string** `"2"`; `total` and `count` both
   present (equal here); envelope is `{assets:{total,count,items,facets,nextPage}}`, so the
   manifest's `recent.assets.items` mapping is correct.
7. **Immich `thumbhash`:** populated on every asset. B4's blur-up placeholder is free.

### F3 — resolved

The live server sends `image/jpeg`. The asset proxy sniffs regardless (architecture §7); no code
change. E3's cold-latency AC was then measured end-to-end (fresh process, empty disk cache, LAN
Immich): `GET /api/v1/cards` 31 ms + six thumbnails through the signed proxy in parallel 58 ms =
**~90 ms cold**, against the 2 s budget. The Immich API key appeared in no page HTML, no card JSON
and no log line.

### F4 — the "Resolved" fix was wrong; the endpoint IS gone, but the replacements don't work

`/Users/{userId}/Items` is genuinely absent from the 12.0.0 OpenAPI (a legacy route still answers
200, undocumented, not to be relied on). But the replacement this spike chose —  `/Users/Me` then
`/Items/Latest` — fails on a real server:

- **`GET /Users/Me` → 400** with an API key (`Authorization: MediaBrowser Token=` or
  `X-Emby-Token`). A Jellyfin API key is app-scoped and carries **no user identity**, so `Me`
  resolves to nothing. A user-scoped token would require `POST /Users/AuthenticateByName` with a
  username + password — worse for a dashboard.
- **`GET /Items/Latest` is the wrong endpoint.** Without `parentId` it ignores `includeItemTypes`
  (asked for `Movie`, returned `MusicAlbum`); with `parentId` it returned `400 Error processing
  request.` on this build.
- **The right endpoint is `GET /Items`** (generic query; `userId` optional). `GET
  /Items?recursive=true&includeItemTypes=Movie&sortBy=DateCreated&sortOrder=Descending&limit=N&enableImages=true&enableImageTypes=Primary&imageTypeLimit=1`
  with only the API key → 200, movies only, newest first, `ImageTags.Primary` on every row, plus
  `TotalRecordCount` for the count signal. **No user-resolution step is needed** — the multi-step
  premise behind putting Jellyfin in WASM (G4) largely evaporates.

The consequent rewrite of `plugins/jellyfin/manifest.yaml`, this file, the capture script and the
G4 module is tracked in `docs/03-backlog.md`.

### F5 — confirmed `[live]`

`GET /Items/{id}/Images/Primary` with **no credential** → 200 `image/jpeg` (~190 KB), no redirect.
A 404 there means the item has no Primary image, not an auth failure.

### F6 — confirmed `[live]`

The 12.0.0 OpenAPI declares exactly one scheme, `CustomAuthentication` = API key in the
`Authorization` header. `X-Emby-Token` is gone from the spec. The example connection's
`Authorization: MediaBrowser Token="…"` is correct.

### F7 — confirmed `[live]`

As in the spec pass: PascalCase by default, camelCase when the profile is requested explicitly, and
the explicit request is honoured. Pin it in G4.

Fixtures are scrubbed before committing: real filenames, album names, user ids and library
contents are personal data, and a golden fixture lives in the repository forever. The committed
Immich `thumbnail-preview.bin` is a synthetic JPEG, not a photo from the source library.
