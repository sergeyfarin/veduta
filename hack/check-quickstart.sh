#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Runs the README's quick start, exactly as written, against an image you name:
#
#   hack/check-quickstart.sh veduta:ci
#
# The quick start is the first thing a new operator does, and it shipped broken once: it landed
# on main describing a `veduta init` that the published 0.1.0 image did not have, and the first
# outside install failed with an exit code and nothing else. The commit that introduced it had
# been verified against a locally built image, so it passed. Nothing then compared the documented
# commands with the artefact an operator would actually pull, and this script is that comparison.
#
# The compose file and the commands are extracted from README.md rather than copied here, so the
# README cannot drift from what is tested. Only the image reference is replaced, with the one
# under test - which is the point: the same instructions, run against a different build.
#
# It needs docker with the compose plugin, curl, and port 8099 free on the loopback interface.
# It leaves nothing behind: the containers, and the scratch directory holding config and data.
set -euo pipefail

image="${1:?usage: hack/check-quickstart.sh IMAGE}"
root="$(cd "$(dirname "$0")/.." && pwd)"

# The first fenced block of the given language after the "Quick start" heading. It stops at the
# heading's own section, so a later example in the README can never be mistaken for the quick start.
quickstart_block() {
  awk -v lang="$1" '
    /^## /                        { in_section = ($0 == "## Quick start"); next }
    in_section && !open && $0 == "```" lang { open = 1; next }
    open && $0 == "```"           { exit }
    open                          { print }
  ' "$root/README.md"
}

compose="$(quickstart_block yaml)"
commands="$(quickstart_block sh)"
[ -n "$compose" ]  || { echo "no yaml block under '## Quick start' in README.md" >&2; exit 1; }
[ -n "$commands" ] || { echo "no sh block under '## Quick start' in README.md" >&2; exit 1; }

# Both services must name the published image; if the README stops doing that this substitutes
# nothing, and testing the wrong image would pass silently.
references="$(grep -c 'image: ghcr.io/sergeyfarin/veduta:' <<<"$compose" || true)"
if [ "$references" -ne 2 ]; then
  echo "expected the quick start to name ghcr.io/sergeyfarin/veduta twice, found $references" >&2
  exit 1
fi

work="$(mktemp -d)"
cleanup() {
  (cd "$work" && docker compose down --volumes --remove-orphans >/dev/null 2>&1) || true
  rm -rf "$work"
}
trap cleanup EXIT
cd "$work"

sed -E "s#(image: )ghcr\.io/sergeyfarin/veduta:[^[:space:]]+#\1${image}#" <<<"$compose" > compose.yaml
echo "== compose.yaml under test =="
cat compose.yaml
echo "== README commands =="
echo "$commands"

# Verbatim. The README joins its last two commands with `;` so the init logs print even when
# `up` fails, which also means this block's own exit status says nothing about `up` - so what it
# left running is asserted below, not what it returned.
echo "== running them =="
output="$(bash -c "$commands" 2>&1)" || true
echo "$output"

init_container="$(docker compose ps --all --quiet veduta-init)"
[ -n "$init_container" ] || { echo "FAIL: veduta-init was never created" >&2; exit 1; }
init_exit="$(docker inspect --format '{{.State.ExitCode}}' "$init_container")"
if [ "$init_exit" != 0 ]; then
  echo "FAIL: veduta-init exited $init_exit; its logs, which the README tells the reader to read:" >&2
  docker compose logs veduta-init >&2 || true
  exit 1
fi

# The README promises the password is printed. Take it from the output the reader would see, where
# compose prefixes every log line with the service name and a pipe.
password="$(sed -nE 's/^veduta-init-[0-9]+ +\| +([A-Za-z0-9]{16,}) *$/\1/p' <<<"$output" | head -n1)"
[ -n "$password" ] || { echo "FAIL: the init logs did not print a password" >&2; exit 1; }

# The README says Veduta is on http://127.0.0.1:8099. Give it a minute: the server starts only
# after init completes, and a cold start on a small runner is not instant.
healthy=""
for _ in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:8099/api/v1/health >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 1
done
if [ -z "$healthy" ]; then
  echo "FAIL: nothing healthy on http://127.0.0.1:8099 after 60s; the server's logs:" >&2
  docker compose logs veduta >&2 || true
  exit 1
fi

# Health proves a process is listening. Signing in with the printed password proves the whole
# promise: init wrote a configuration, hashed that password into it, and the server read it back.
status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8099/api/v1/auth/session \
  -H 'Content-Type: application/json' \
  --data "{\"username\":\"admin\",\"password\":\"${password}\"}")"
if [ "$status" != 201 ]; then
  echo "FAIL: signing in as admin with the printed password returned HTTP $status, want 201" >&2
  exit 1
fi

# The dashboard itself, not just the API: the SPA is embedded at build time and a binary built
# without it serves a 404 here while every API check above still passes.
curl -fsS http://127.0.0.1:8099/ | grep -qi '<html' \
  || { echo "FAIL: GET / did not return the dashboard page" >&2; exit 1; }

echo "quick start OK against ${image}"
