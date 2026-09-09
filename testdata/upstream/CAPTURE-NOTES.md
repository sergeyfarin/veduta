# S2 upstream capture — evidence record

Produced by `hack/capture-upstream-fixtures.sh` on 2026-09-09 against live servers, then
**scrubbed by hand** before committing. See `docs/spikes/s2-upstream-reality-check.md` for the
findings these files support and `docs/03-backlog.md` for the follow-up work.

## Immich 3.1.0

| Request | Status | Content-Type | Size (real) | Redirects |
| --- | --- | --- | --- | --- |
| `GET /api/assets/statistics` | 200 | `application/json; charset=utf-8` | 44 B | 0 |
| `GET /api/server/statistics` | 403 | `application/json; charset=utf-8` | 60 B | 0 |
| `POST /api/search/metadata` (`size:6`) | 200 | `application/json; charset=utf-8` | 4875 B | 0 |
| `GET /api/assets/{id}/thumbnail?size=preview` | 200 | `image/jpeg` | 166688 B | 0 |

- Thumbnail first bytes on the wire: `ff d8 ff e2 ...` — a real JPEG (JFIF/EXIF), **not** the
  `application/octet-stream` the OpenAPI 3.x `viewAsset` response declares (spike finding F3).
- `search/metadata` envelope is `{albums, assets:{total,count,items,facets,nextPage}}`;
  `nextPage` is the **string** `"2"`. Every asset carries a populated `thumbhash` and
  `visibility: "timeline"`.
- `/api/server/statistics` returns `{"message":"Missing required permission: server.statistics"}`
  for a non-admin key — confirms F1.

### Scrubbing applied to the committed files
- `search-metadata.json` — asset `id`s, `ownerId`, `originalPath`, `originalFileName`, `checksum`,
  `thumbhash` and all timestamps replaced with deterministic synthetic values. Response
  **structure, field set, counts and `nextPage` shape are unchanged.**
- `assets-statistics.json` — real body shape `{"images","videos","total"}` kept; the counts are
  synthetic (the live instance holds a real personal library).
- `server-statistics.json` — the 403 error body, no personal data, kept verbatim.
- `thumbnail-preview.bin` — the real capture was a photo from the owner's library. Replaced with a
  **synthetic 2160×1440 baseline JPEG** (`go run hack/gen-fixture-images.go` style, deterministic
  seed). The wire facts that matter — `Content-Type: image/jpeg`, JFIF magic bytes, ~160 KB at
  `preview` size — are recorded in the table above.

## Jellyfin 12.0.0 — no committable fixtures from this run

The capture script's Jellyfin half still probes `/Users/Me` and `/Items/Latest`, which the live
pass showed to be the wrong endpoints (`/Users/Me` → 400 with an API key; `/Items/Latest` ignores
`includeItemTypes`). Every Jellyfin response from this run was an error and none is a useful
golden. The Jellyfin capture will be redone once the manifest and `hack/capture-upstream-fixtures.sh`
are repointed at `GET /Items` — see `docs/03-backlog.md`, "Jellyfin manifest, spike S2, capture
script and G4 need a coordinated rewrite".

What the live Jellyfin 12 calls did establish (recorded in the spike doc, not as fixtures here):
default JSON casing is PascalCase and an explicit `profile="CamelCase"` is honoured (F7); item
`Images/Primary` is served with no credential (F5); the only auth scheme in the 12.0.0 OpenAPI is
an API key in the `Authorization` header (F6).
