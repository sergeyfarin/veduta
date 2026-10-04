// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// The responses are captured from a real Prometheus 3.15.0 by hack/capture-prometheus-fixtures.sh
// (testdata/upstream/CAPTURE-NOTES.md), each served for the PromQL that produced it. Expected
// values are read back out of the same files independently of the manifest's expressions, so a
// wrong path through Prometheus's [timestamp, "value"] pairs cannot agree with them by accident.
var promFixtures = map[string]string{
	"sum(up)": "query-vector-one",
	"sum by (handler) (prometheus_http_requests_total)": "query-vector-many",
	"scalar(sum(up))":         "query-scalar",
	"veduta_no_such_metric":   "query-empty",
	"(sum(up) - sum(up)) / 0": "query-nan",
	"sum((":                   "query-bad",
	"sum(rate(prometheus_http_requests_total[1m]))": "query-range",
}

type promUpstream struct {
	mu       sync.Mutex
	requests []*url.URL
}

func (u *promUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.requests = append(u.requests, r.URL)
	u.mu.Unlock()
	name, ok := promFixtures[r.URL.Query().Get("query")]
	if r.URL.Path == "/api/v1/query_range" && name == "query-empty" {
		name = "query-range-empty"
	}
	if !ok {
		http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusNotFound)
		return
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "upstream", "prometheus", name+".json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if name == "query-bad" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_, _ = w.Write(body)
}

// invokePrometheus runs one operation of the shipped manifest through the real broker, with the
// route grant an approval of that manifest would produce. The document comes back as plain JSON
// so the assertions read the same shape the frontend does.
func invokePrometheus(t *testing.T, opID, params string) (map[string]any, *promUpstream, error) {
	t.Helper()
	up := &promUpstream{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)

	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "prometheus", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	op := operation(t, m, opID)
	routes := make([]capabilities.Route, len(op.Routes))
	for i, r := range op.Routes {
		routes[i] = capabilities.Route{Slot: r.Slot, Method: r.Method, Path: r.Path, QueryKeys: r.QueryKeys, Use: capabilities.UseData}
	}
	reg, err := connections.New(map[string]config.Connection{
		"conn1": {Kind: "http", HTTP: &config.HTTPConnection{BaseURL: srv.URL, Auth: config.ConnectionAuth{Type: "none"}}},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := capabilities.NewBroker(reg, capabilities.NewMemCache(), capabilities.NewMemAudit(), nil)
	grant := capabilities.NewGrant(m.ID, m.Version, "inst1", map[string]string{"server": "conn1"},
		capabilities.NewCapSet("http"), routes, routes, nil,
		capabilities.Limits{HTTPRequests: 8, ResponseMB: 8, HostCalls: 100}, capabilities.ExecutionIdentity{})
	inst, err := declarative.New(broker).Load(context.Background(), integrations.Installed{
		Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: opID, Params: json.RawMessage(params), Grant: grant})
	if err != nil {
		return nil, up, err
	}
	body, err := json.Marshal(resp.Document)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, up, nil
}

// promFixture decodes a captured response for computing expectations.
func promFixture(t *testing.T, name string) (data struct {
	ResultType string `json:"resultType"`
	Result     json.RawMessage
}) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "upstream", "prometheus", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	data.ResultType, data.Result = env.Data.ResultType, env.Data.Result
	return data
}

func block(t *testing.T, doc map[string]any, i int) map[string]any {
	t.Helper()
	blocks, _ := doc["blocks"].([]any)
	if len(blocks) <= i {
		t.Fatalf("document has %d blocks, want at least %d: %v", len(blocks), i+1, doc)
	}
	return blocks[i].(map[string]any)
}

func TestPrometheusStat(t *testing.T) {
	doc, up, err := invokePrometheus(t, "stat", `{"queries": [
		{"label": "Targets up", "query": "sum(up)", "format": "count"},
		{"label": "Scalar", "query": "scalar(sum(up))"},
		{"label": "Missing", "query": "veduta_no_such_metric", "unit": "B"},
		{"label": "Undefined", "query": "(sum(up) - sum(up)) / 0", "format": "percent"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(up.requests) != 4 {
		t.Fatalf("made %d requests for 4 queries", len(up.requests))
	}
	var one []struct {
		Value [2]any `json:"value"`
	}
	if err := json.Unmarshal(promFixture(t, "query-vector-one").Result, &one); err != nil {
		t.Fatal(err)
	}
	want, _ := strconv.ParseFloat(one[0].Value[1].(string), 64)
	var scalar [2]any
	if err := json.Unmarshal(promFixture(t, "query-scalar").Result, &scalar); err != nil {
		t.Fatal(err)
	}
	wantScalar, _ := strconv.ParseFloat(scalar[1].(string), 64)

	items, _ := block(t, doc, 0)["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("items = %v", items)
	}
	check := func(i int, label string, value any, format, unit string) {
		t.Helper()
		item := items[i].(map[string]any)
		if item["label"] != label || item["value"] != value || item["format"] != format {
			t.Errorf("items[%d] = %v, want label %q value %v format %q", i, item, label, value, format)
		}
		if got, _ := item["unit"].(string); got != unit {
			t.Errorf("items[%d].unit = %q, want %q", i, got, unit)
		}
	}
	check(0, "Targets up", want, "count", "")
	check(1, "Scalar", wantScalar, "number", "")
	// No samples and a NaN sample both show as no value, not as a failed card.
	check(2, "Missing", nil, "number", "B")
	check(3, "Undefined", nil, "percent", "")

	signal, _ := doc["signals"].(map[string]any)["value"].(map[string]any)
	if signal["value"] != want {
		t.Errorf("value signal = %v, want the first query's %v", signal, want)
	}
}

// With nothing to read, the signal is absent - which a rule reads as unknown - rather than zero.
func TestPrometheusStatSignalAbsentWithoutASample(t *testing.T) {
	for _, q := range []string{"veduta_no_such_metric", "(sum(up) - sum(up)) / 0"} {
		doc, up, err := invokePrometheus(t, "stat", `{"queries": [{"label": "x", "query": "`+q+`"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		if len(up.requests) != 1 {
			t.Errorf("%s: made %d requests for one query", q, len(up.requests))
		}
		if signals, _ := doc["signals"].(map[string]any); signals["value"] != nil {
			t.Errorf("%s: value signal = %v, want it absent", q, signals["value"])
		}
	}
}

func TestPrometheusSeries(t *testing.T) {
	before := time.Now().Unix()
	doc, up, err := invokePrometheus(t, "series", `{"title": "Requests", "window": "1h", "format": "number", "unit": "req/s", "min": 0,
		"lines": [{"label": "All", "query": "sum(rate(prometheus_http_requests_total[1m]))"}, {"label": "None", "query": "veduta_no_such_metric"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(up.requests) != 2 {
		t.Fatalf("made %d requests for 2 lines", len(up.requests))
	}
	for _, r := range up.requests {
		q := r.Query()
		start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
		end, _ := strconv.ParseInt(q.Get("end"), 10, 64)
		if r.Path != "/api/v1/query_range" || q.Get("step") != "15" || end-start != 3600 || end < before || end > time.Now().Unix() {
			t.Errorf("request %s, want query_range over the last hour at a 15s step", r)
		}
	}
	if up.requests[0].Query().Get("end") != up.requests[1].Query().Get("end") {
		t.Errorf("lines were read over different windows: %s and %s", up.requests[0], up.requests[1])
	}

	var matrix []struct {
		Values [][2]any `json:"values"`
	}
	if err := json.Unmarshal(promFixture(t, "query-range").Result, &matrix); err != nil {
		t.Fatal(err)
	}
	b := block(t, doc, 0)
	if b["type"] != "series" || b["title"] != "Requests" || b["unit"] != "req/s" || b["min"] != 0.0 || b["max"] != nil {
		t.Errorf("block = %v", b)
	}
	lines, _ := b["series"].([]any)
	if len(lines) != 2 {
		t.Fatalf("series = %v", lines)
	}
	all := lines[0].(map[string]any)
	points, _ := all["points"].([]any)
	if all["label"] != "All" || len(points) != len(matrix[0].Values) {
		t.Fatalf("first line = %v, want %d points", all, len(matrix[0].Values))
	}
	for i, p := range points {
		pt := p.(map[string]any)
		ts := time.Unix(int64(matrix[0].Values[i][0].(float64)), 0).UTC().Format(time.RFC3339)
		v, _ := strconv.ParseFloat(matrix[0].Values[i][1].(string), 64)
		if pt["t"] != ts || pt["v"] != v {
			t.Errorf("points[%d] = %v, want t %s v %v", i, pt, ts, v)
		}
	}
	if none := lines[1].(map[string]any); none["label"] != "None" || len(none["points"].([]any)) != 0 {
		t.Errorf("a query with no series should draw an empty line, got %v", none)
	}
}

// Every window's step keeps a line inside the series block's 288 points.
func TestPrometheusSeriesStepFitsTheBlock(t *testing.T) {
	for _, window := range []string{"1h", "6h", "24h", "7d"} {
		_, up, err := invokePrometheus(t, "series", `{"window": "`+window+`", "lines": [{"label": "x", "query": "veduta_no_such_metric"}]}`)
		if err != nil {
			t.Fatal(err)
		}
		q := up.requests[0].Query()
		start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
		end, _ := strconv.ParseInt(q.Get("end"), 10, 64)
		step, _ := strconv.ParseInt(q.Get("step"), 10, 64)
		d, _ := time.ParseDuration(strings.Replace(window, "7d", "168h", 1))
		if end-start != int64(d.Seconds()) || step <= 0 || (end-start)/step+1 > 288 {
			t.Errorf("%s: start %d end %d step %d", window, start, end, step)
		}
	}
}

func TestPrometheusTop(t *testing.T) {
	doc, _, err := invokePrometheus(t, "top", `{"title": "Busiest handlers", "query": "sum by (handler) (prometheus_http_requests_total)", "label": "handler", "format": "count", "limit": 3}`)
	if err != nil {
		t.Fatal(err)
	}
	var vector []struct {
		Metric map[string]string `json:"metric"`
		Value  [2]any            `json:"value"`
	}
	if err := json.Unmarshal(promFixture(t, "query-vector-many").Result, &vector); err != nil {
		t.Fatal(err)
	}
	var max float64
	for _, s := range vector {
		if v, _ := strconv.ParseFloat(s.Value[1].(string), 64); v > max {
			max = v
		}
	}
	b := block(t, doc, 0)
	items, _ := b["items"].([]any)
	if b["type"] != "list" || b["title"] != "Busiest handlers" || len(items) != 3 {
		t.Fatalf("block = %v", b)
	}
	prev := max
	for i, it := range items {
		item := it.(map[string]any)
		v, _ := item["value"].(float64)
		if i == 0 && v != max {
			t.Errorf("first row = %v, want the largest value %v", item, max)
		}
		if v > prev || item["format"] != "count" || !strings.HasPrefix(item["title"].(string), "/") {
			t.Errorf("items[%d] = %v, want descending values titled by handler", i, item)
		}
		prev = v
	}
}

func TestPrometheusTopWithoutTheLabel(t *testing.T) {
	doc, _, err := invokePrometheus(t, "top", `{"query": "sum(up)", "label": "instance", "subtitle": "job"}`)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := block(t, doc, 0)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	item := items[0].(map[string]any)
	if item["title"] != "(no instance)" || item["subtitle"] != nil {
		t.Errorf("item = %v, want a placeholder title and no subtitle", item)
	}
}

// A query Prometheus cannot parse fails the card with the status, not an empty card.
func TestPrometheusBadQueryFailsTheCard(t *testing.T) {
	_, _, err := invokePrometheus(t, "top", `{"query": "sum((", "label": "x"}`)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("err = %v, want the upstream's 400", err)
	}
}
