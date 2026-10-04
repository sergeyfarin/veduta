#!/usr/bin/env bash
# Capture the Prometheus query API responses plugins/prometheus is tested against.
#
#   docker run -d --rm --name veduta-prom -p 127.0.0.1:19090:9090 prom/prometheus:latest
#   # wait a few minutes, so range queries have samples to return
#   hack/capture-prometheus-fixtures.sh http://127.0.0.1:19090
#
# A stock Prometheus scrapes only itself, so nothing captured is private. Each file is the
# response body exactly as served; the status line is recorded in CAPTURE-NOTES.md.
set -euo pipefail

base=${1:?usage: $0 <prometheus base URL>}
out=testdata/upstream/prometheus
mkdir -p "$out"
now=$(date +%s)

instant() { # name query
	curl -sS -G -o "$out/$1.json" -w "%{http_code} $1\n" "$base/api/v1/query" --data-urlencode "query=$2" --data-urlencode "time=$now"
}
range() { # name query
	curl -sS -G -o "$out/$1.json" -w "%{http_code} $1\n" "$base/api/v1/query_range" --data-urlencode "query=$2" \
		--data-urlencode "start=$((now - 3600))" --data-urlencode "end=$now" --data-urlencode "step=15"
}

instant query-vector-one 'sum(up)'
instant query-vector-many 'sum by (handler) (prometheus_http_requests_total)'
instant query-scalar 'scalar(sum(up))'
instant query-empty 'veduta_no_such_metric'
instant query-nan '(sum(up) - sum(up)) / 0'
instant query-bad 'sum(('
range query-range 'sum(rate(prometheus_http_requests_total[1m]))'
range query-range-empty 'veduta_no_such_metric'
echo "start=$((now - 3600)) end=$now"
