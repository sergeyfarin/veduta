#!/usr/bin/env bash
# Stage the first-party integrations that ship with a release.
#
# One list, two consumers: the release workflow copies it into each archive, and the Dockerfile
# copies it into /usr/share/veduta/plugins. Listing a file here is what makes it shipped -
# everything else under plugins/ (Rust sources, Cargo manifests and locks, target/, READMEs, the
# hello example) is build material and must never reach a user's disk.
#
# Plugins ship *beside* the binary rather than inside it, deliberately: a side-loaded manifest is
# lock-governed like any third-party integration, so a first-party one crosses the same approval
# boundary instead of quietly inheriting the binary's trust. See docs/01-architecture.md section 6.
#
#   hack/stage-plugins.sh <destdir>   stage the distributed set into destdir
#   hack/stage-plugins.sh --list      print the distributed set, one relative path per line
set -euo pipefail

# The distributed set, as paths under plugins/. A wasm module is listed next to the manifest whose
# spec.sha256 pins it.
files=(
  arcane/manifest.yaml
  beszel/manifest.yaml
  dockhand/manifest.yaml
  glances/manifest.yaml
  homeassistant/manifest.yaml
  immich/manifest.yaml
  jellyfin/manifest.yaml
  jellyfin/jellyfin.wasm
  proxmox/manifest.yaml
)

if [ "${1:-}" = "--list" ]; then
  printf '%s\n' "${files[@]}"
  exit 0
fi

dest="${1:?usage: hack/stage-plugins.sh <destdir> | --list}"
src="$(cd "$(dirname "$0")/.." && pwd)/plugins"

for file in "${files[@]}"; do
  install -D -m 0644 "$src/$file" "$dest/$file"
done

# A module whose bytes do not match the digest its own manifest pins is refused at load - which is
# the right behaviour, but discovering it after publishing an archive is not. Check while staging.
for file in "${files[@]}"; do
  case "$file" in
  *.wasm) ;;
  *) continue ;;
  esac
  manifest="$src/$(dirname "$file")/manifest.yaml"
  want="$(sed -n 's/^[[:space:]]*sha256:[[:space:]]*"\{0,1\}\([0-9a-f]\{64\}\)"\{0,1\}.*/\1/p' "$manifest")"
  got="$(sha256sum "$dest/$file" | cut -d' ' -f1)"
  if [ -z "$want" ]; then
    echo "$file: $manifest pins no sha256" >&2
    exit 1
  fi
  if [ "$want" != "$got" ]; then
    echo "$file: manifest pins $want but the staged module is $got" >&2
    exit 1
  fi
done

printf 'staged %d files into %s\n' "${#files[@]}" "$dest"
