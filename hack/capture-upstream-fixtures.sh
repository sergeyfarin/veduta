#!/usr/bin/env bash
# Capture real upstream responses as test fixtures (spike S2).
#
# Nothing in the contract suite needs a running Immich or Jellyfin - fixtures are what make that
# true. This script produces them, and answers the questions a specification cannot: the actual
# Content-Type of a thumbnail, real response sizes, redirect behaviour, JSON casing.
#
#   IMMICH_URL=http://immich:2283 IMMICH_KEY=... \
#   JELLYFIN_URL=http://jellyfin:8096 JELLYFIN_KEY=... \
#     hack/capture-upstream-fixtures.sh
#
# Output goes to testdata/upstream/. REVIEW BEFORE COMMITTING: responses contain real filenames,
# album names, user ids and library contents. A golden fixture lives in the repository forever.
set -euo pipefail

out="testdata/upstream"
mkdir -p "$out/immich" "$out/jellyfin"
notes="$out/CAPTURE-NOTES.md"
: > "$notes"

log() { printf '%s\n' "$*" | tee -a "$notes"; }

# probe METHOD URL OUTFILE [curl args...]
# Records status, content type, byte size and redirect count next to the body, because those are
# the answers the spike is actually after.
probe() {
  local method="$1" url="$2" file="$3"; shift 3
  local meta
  meta=$(curl -sS -X "$method" \
    -o "$file" \
    -w '%{http_code}\t%{content_type}\t%{size_download}\t%{num_redirects}\t%{redirect_url}' \
    --max-time 30 "$@" "$url" || echo "ERR")
  IFS=$'\t' read -r code ctype size redirects redirect_url <<<"$meta"
  log "| \`$method $(printf '%s' "$url" | sed 's#https\?://[^/]*##')\` | $code | \`${ctype:-none}\` | ${size} B | ${redirects:-0}${redirect_url:+ -> $redirect_url} |"
}

if [[ -n "${IMMICH_URL:-}" && -n "${IMMICH_KEY:-}" ]]; then
  log "## Immich"
  log ""
  log "| Request | Status | Content-Type | Size | Redirects |"
  log "| --- | --- | --- | --- | --- |"
  auth=(-H "x-api-key: ${IMMICH_KEY}")

  probe GET  "${IMMICH_URL}/api/assets/statistics" "$out/immich/assets-statistics.json" "${auth[@]}"
  # Admin-only: a 403 here is the expected result for a non-admin key, and confirms finding F1.
  probe GET  "${IMMICH_URL}/api/server/statistics" "$out/immich/server-statistics.json" "${auth[@]}"
  probe POST "${IMMICH_URL}/api/search/metadata" "$out/immich/search-metadata.json" \
    "${auth[@]}" -H 'content-type: application/json' \
    -d '{"size":6,"order":"desc","visibility":"timeline"}'

  # F3: does a thumbnail really arrive as application/octet-stream, as the spec declares? The
  # asset proxy's guard depends on the answer.
  id=$(sed -n 's/.*"id":"\([0-9a-f-]\{36\}\)".*/\1/p' "$out/immich/search-metadata.json" | head -1)
  if [[ -n "$id" ]]; then
    probe GET "${IMMICH_URL}/api/assets/${id}/thumbnail?size=preview" \
      "$out/immich/thumbnail-preview.bin" "${auth[@]}"
    log ""
    log "First bytes of the thumbnail (magic number tells you the real format regardless of header):"
    log '```'
    log "$(od -An -tx1 -N 16 "$out/immich/thumbnail-preview.bin" | tr -s ' ')"
    log '```'
  else
    log ""
    log "No asset id found in the search response - skipped the thumbnail probe."
  fi
  log ""
fi

if [[ -n "${JELLYFIN_URL:-}" && -n "${JELLYFIN_KEY:-}" ]]; then
  log "## Jellyfin"
  log ""
  log "| Request | Status | Content-Type | Size | Redirects |"
  log "| --- | --- | --- | --- | --- |"
  # The plugin pins the CamelCase JSON profile; capture goldens with the same header.
  auth=(-H "Authorization: MediaBrowser Token=\"${JELLYFIN_KEY}\"" \
        -H 'Accept: application/json; profile="CamelCase"')

  # A Jellyfin API key has no associated user, so /Users/Me and a userId-less
  # /Items/Latest both 400 - recently-added is one sorted /Items query. See
  # docs/spikes/s2-upstream-reality-check.md finding F4.
  recent='recursive=true&includeItemTypes=Movie,Series&sortBy=DateCreated&sortOrder=Descending'
  recent="${recent}&limit=6&fields=ProductionYear&imageTypeLimit=1&enableImageTypes=Primary&enableImages=true"
  probe GET "${JELLYFIN_URL}/Items?${recent}" "$out/jellyfin/items-recent.json" "${auth[@]}"
  probe GET "${JELLYFIN_URL}/Items?recursive=true&includeItemTypes=Movie&limit=1" \
    "$out/jellyfin/items-count-movies.json" "${auth[@]}"
  probe GET "${JELLYFIN_URL}/Items?recursive=true&includeItemTypes=Series&limit=1" \
    "$out/jellyfin/items-count-series.json" "${auth[@]}"
  probe GET "${JELLYFIN_URL}/Sessions" "$out/jellyfin/sessions.json" "${auth[@]}"

  # F7: which casing does the server use by default, and is an explicit profile honoured?
  jf_auth=(-H "Authorization: MediaBrowser Token=\"${JELLYFIN_KEY}\"")
  probe GET "${JELLYFIN_URL}/Items?recursive=true&includeItemTypes=Movie&limit=1" \
    "$out/jellyfin/casing-default.json" "${jf_auth[@]}"
  probe GET "${JELLYFIN_URL}/Items?recursive=true&includeItemTypes=Movie&limit=1" \
    "$out/jellyfin/casing-camel.json" \
    "${jf_auth[@]}" -H 'Accept: application/json; profile="CamelCase"'

  # F5: item images are declared unauthenticated. Verify with NO credential at all.
  item=$(sed -n 's/.*"\(Id\|id\)":"\([0-9a-f]\{32\}\)".*/\2/p' "$out/jellyfin/items-recent.json" | head -1)
  if [[ -n "$item" ]]; then
    probe GET "${JELLYFIN_URL}/Items/${item}/Images/Primary?maxWidth=400" \
      "$out/jellyfin/poster-noauth.bin"
    log ""
    log "The poster probe above sent **no credential**: a 200 confirms finding F5."
  else
    log ""
    log "No item id found - skipped the poster probe."
  fi
  log ""
fi

log "Captured $(find "$out" -type f ! -name CAPTURE-NOTES.md | wc -l) files into $out/."
log ""
log "Review every file before committing: they contain real filenames, album names, user ids and"
log "library contents. Redact or synthesise anything personal - a fixture is permanent."
