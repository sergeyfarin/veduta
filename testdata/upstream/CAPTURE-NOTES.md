# S2 upstream capture — evidence record

Produced by `hack/capture-upstream-fixtures.sh` on 2026-09-09 against live servers, then
**scrubbed by hand** before committing. See `docs/spikes/s2-upstream-reality-check.md` for the
findings these files support and `docs/03-backlog.md` for follow-up work.

## Immich 3.1.0

| Request | Status | Content-Type | Size (real) | Redirects |
| --- | --- | --- | --- | --- |
| `GET /api/assets/statistics` | 200 | `application/json; charset=utf-8` | 44 B | 0 |
| `GET /api/server/statistics` | 403 | `application/json; charset=utf-8` | 60 B | 0 |
| `POST /api/search/metadata` (`size:6`) | 200 | `application/json; charset=utf-8` | 4875 B | 0 |
| `GET /api/assets/{id}/thumbnail?size=preview` | 200 | `image/jpeg` | 166688 B | 0 |

- Thumbnail first bytes on the wire: `ff d8 ff e2 ...` — a real JPEG (JFIF/EXIF), **not** the
  `application/octet-stream` the OpenAPI 3.x `viewAsset` response declares (finding F3).
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
  synthetic.
- `server-statistics.json` — the 403 error body, no personal data, kept verbatim.
- `thumbnail-preview.bin` — the real capture was a photo from the owner's library. Replaced with a
  **synthetic 2160×1440 baseline JPEG**. The wire facts that matter — `Content-Type: image/jpeg`,
  JFIF magic bytes, ~160 KB at `preview` size — are in the table above.

## Jellyfin 12.0.0

Approach A (see the spike): a Jellyfin API key has no associated user, so recently-added is one
sorted `GET /Items` query — no `/Users/Me`, no user-scoped `/Items/Latest`.

| Request | Status | Content-Type | Size (real) | Redirects |
| --- | --- | --- | --- | --- |
| `GET /Items?recursive=true&includeItemTypes=Movie,Series&sortBy=DateCreated&sortOrder=Descending&limit=6&…` | 200 | `application/json; profile="CamelCase"; charset=utf-8` | 3990 B | 0 |
| `GET /Items?recursive=true&includeItemTypes=Movie&limit=1` | 200 | `application/json; profile="CamelCase"; charset=utf-8` | 1185 B | 0 |
| `GET /Items?recursive=true&includeItemTypes=Series&limit=1` | 200 | `application/json; profile="CamelCase"; charset=utf-8` | 823 B | 0 |
| `GET /Sessions` | 200 | `application/json; profile="CamelCase"; charset=utf-8` | 1921 B | 0 |
| `GET /Items?…&limit=1` — default `Accept` (casing probe) | 200 | `application/json; charset=utf-8` | 1185 B | 0 |
| `GET /Items?…&limit=1` — `Accept: …profile="CamelCase"` (casing probe) | 200 | `application/json; profile="CamelCase"; charset=utf-8` | 1185 B | 0 |
| `GET /Items/{id}/Images/Primary?maxWidth=400` — **no credential** | 200 | `image/jpeg` | 153254 B | 0 |

- **F7:** default `Accept` → PascalCase body (`{"Items":[{"Name":…}]}`); `Accept: application/json;
  profile="CamelCase"` → camelCase body (`{"items":[{"name":…}]}`) and the server echoes the
  profile in the response `Content-Type`. The explicit profile is honoured. The plugin pins it.
- **F5:** the poster request carried **no `Authorization` header** and still returned `200
  image/jpeg`.
- `/Items` list envelope is `{items, totalRecordCount, startIndex}`; every one of the six newest
  items had an `imageTags.Primary`. `totalRecordCount` on the type-filtered `limit:1` queries is
  the whole-library count (movies 288, series 25 on the source server).
- No redirects anywhere.

### Scrubbing applied to the committed files
- `items-recent.json`, `items-count-movies.json`, `items-count-series.json` — synthetic item
  names, ids, `serverId`, `imageTags`/`imageBlurHashes` values and years; real ratings, container
  detail and provider ids dropped. Envelope, field casing and `totalRecordCount` shape unchanged.
- `casing-default.json` / `casing-camel.json` — trimmed to the one thing they demonstrate, the key
  casing; PascalCase vs camelCase preserved exactly.
- `sessions.json` — one idle session, shape only; device id, real user name and client details
  removed. No `NowPlayingItem` (nobody was streaming at capture time).
- `poster-noauth.bin` — replaced with a **synthetic 400×600 baseline JPEG**. Real capture was
  `image/jpeg`, 153 KB.
