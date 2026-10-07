# S2 upstream capture — evidence record

Produced by `hack/capture-upstream-fixtures.sh` on 2026-09-09 against live servers, then
**scrubbed by hand** before committing. See `docs/spikes/s2-upstream-reality-check.md` for the
findings these files support and `docs/archive/03-backlog-resolved.md` for the follow-up work.

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

## Prometheus 3.15.0

Captured 2026-10-04 by `hack/capture-prometheus-fixtures.sh` from a stock `prom/prometheus`
container scraping only itself, a few minutes after it started. Every file is the body exactly as
served; nothing was scrubbed, because a self-scraping Prometheus holds nothing private.

| File | Request | Status |
| --- | --- | --- |
| `query-vector-one.json` | `GET /api/v1/query` `sum(up)` | 200 |
| `query-vector-many.json` | `GET /api/v1/query` `sum by (handler) (prometheus_http_requests_total)` | 200 |
| `query-scalar.json` | `GET /api/v1/query` `scalar(sum(up))` | 200 |
| `query-empty.json` | `GET /api/v1/query` `veduta_no_such_metric` | 200 |
| `query-nan.json` | `GET /api/v1/query` `(sum(up) - sum(up)) / 0` | 200 |
| `query-bad.json` | `GET /api/v1/query` `sum((` | 400 |
| `query-range.json` | `GET /api/v1/query_range` `sum(rate(prometheus_http_requests_total[1m]))`, 1 h at 15 s | 200 |
| `query-range-empty.json` | `GET /api/v1/query_range` `veduta_no_such_metric`, 1 h at 15 s | 200 |
| `query-table-total.json` | `GET /api/v1/query` `sum by (handler, code) (prometheus_http_requests_total)` | 200 |
| `query-table-rate.json` | `GET /api/v1/query` `sum by (handler, code) (rate(prometheus_http_requests_total[5m]))` | 200 |
| `query-table-size.json` | `GET /api/v1/query` `sum by (handler) (prometheus_http_response_size_bytes_sum)` | 200 |

- Every sample value is a **string**, `"NaN"` included; timestamps are JSON numbers of Unix
  seconds (whole here, because the capture passed whole-second `time`/`start`).
- A scalar result is the bare pair `[t, "v"]`, not a list; a vector is a list of
  `{metric, value}`, a matrix a list of `{metric, values}`.
- No samples is `"result": []` with status 200, not an error. A query that does not parse is
  400 with `{"status":"error","errorType":"bad_data","error":…}`.
- The three `query-table-*` files were captured 2026-10-07 from a fresh `prom/prometheus:v3.15.0`
  after a few requests, three of them failing with 400, so `/api/v1/query` appears under two
  `code` values with different counts.
- The range result only covers the minutes the server had been running: a matrix has no points
  where there was no data, rather than nulls.
