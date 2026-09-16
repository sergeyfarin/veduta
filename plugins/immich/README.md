# Immich integration

This declarative integration offers recent photos, library statistics, and
on-this-day memories. Connections own the API key; thumbnails go through Veduta's
asset broker. No Python service, custom HTML, or WASM module is needed.

## Memories

Add a card using your existing Immich connection:

```yaml
- id: immich-memories
  integration: immich
  operation: memories
  slots: { server: immich }
  params:
    limit: 6
    publicUrl: https://photos.example.com
  refresh: 1h
  span: { columns: 2, rows: 3 }
```

The API key needs `memory.read` and `asset.view`. Review and re-approve the
updated manifest using your configuration path:

```sh
veduta integration diff --config veduta.yaml immich
veduta integration approve --config veduta.yaml immich
```

Review the route selection: memories needs `GET /api/memories` and
`GET /api/assets/*/thumbnail`. It does not need the admin-only
`/api/server/statistics` route declared by the separate statistics operation.
The checked-in example lock keeps that admin route unapproved.

- `limit` defaults to 6 and accepts 1–9. It applies after year deduplication.
- The request explicitly filters `type=on_this_day` and `for=YYYY-MM-DD`.
  The default day and relative-year labels both use the invocation's UTC date.
  Optional `date: "2026-01-01"` selects a fixed day for inspection; unavailable
  historical memories produce an empty grid. This is not a historical photo search.
- Empty memories and other memory types are excluded. The original year comes
  from `data.year`, falling back to the parsed `memoryAt` year. Missing years,
  the selected year itself, and future years are excluded. Malformed dates or
  nonnumeric years fail visibly instead of producing misleading labels.
- When multiple nonempty memories have the same year, the first in Immich's
  descending response order wins. Its first asset is the thumbnail and its
  asset count is shown; groups are not merged. Counts say “items” because assets
  may include videos. Selected years are sorted newest first.
- `publicUrl` is optional. Set it to your browser-facing Immich URL, including
  any deployment subpath, without credentials, query, or fragment. Leaving it
  unset omits photo links. The integration never reads the private connection URL.
- The existing image-grid renderer provides three columns, lazy thumbnails and
  captions on hover/focus. An empty result reads “No memories on this day”.
  HTTP failures use Veduta's normal integration error handling.

The response and date-filter contract were checked against Immich **3.1.0**
([DTO](https://github.com/immich-app/immich/blob/v3.1.0/server/src/dtos/memory.dto.ts),
[query](https://github.com/immich-app/immich/blob/v3.1.0/server/src/repositories/memory.repository.ts)).
The memories scenarios are hand-written fixtures, not a live-server validation.
The existing recent-photos operation was validated live during
[S2](../../docs/spikes/s2-upstream-reality-check.md).
