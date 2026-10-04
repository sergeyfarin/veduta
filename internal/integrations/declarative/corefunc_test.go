// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// The loader admits exactly manifestload.CoreFunctions; compile must register every one, or a
// manifest that loads fails only when a card runs.
func TestEveryCoreFunctionIsRegistered(t *testing.T) {
	calls := map[string]string{"fromUnix": `fromUnix(0)`, "unix": `unix(now())`}
	if len(calls) != len(manifestload.CoreFunctions) {
		t.Fatalf("manifestload.CoreFunctions = %v; this test covers %d", manifestload.CoreFunctions, len(calls))
	}
	for name := range manifestload.CoreFunctions {
		src, ok := calls[name]
		if !ok {
			t.Fatalf("core function %q has no case here", name)
		}
		p, err := compile(src, 64)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if _, err := run(context.Background(), p, map[string]any{}, &budget{deadline: time.Now().Add(time.Second)}); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
}

func TestFromUnix(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{0, "1970-01-01T00:00:00Z"},
		{int64(1759600000), "2025-10-04T17:46:40Z"},
		{1759600000.0, "2025-10-04T17:46:40Z"},
		{1759600000.123, "2025-10-04T17:46:40.123Z"},
		// Float noise below a millisecond is not a distinct instant.
		{1759600000.1234999, "2025-10-04T17:46:40.123Z"},
		// Rounded to the nearest millisecond, not truncated.
		{1759600000.1236, "2025-10-04T17:46:40.124Z"},
		{json.Number("1759600000.5"), "2025-10-04T17:46:40.5Z"},
		{float64(maxUnixSeconds), "9999-12-31T23:59:59Z"},
	} {
		got, err := fromUnix(c.in)
		if err != nil || got != c.want {
			t.Errorf("fromUnix(%v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []any{-1, float64(maxUnixSeconds + 1), math.NaN(), math.Inf(1), "1759600000", json.Number("x"), nil} {
		if got, err := fromUnix(in); err == nil {
			t.Errorf("fromUnix(%v) = %q, want an error", in, got)
		}
	}
}

func TestUnix(t *testing.T) {
	p, err := compile(`unix(now() - duration("24h"))`, 64)
	if err != nil {
		t.Fatal(err)
	}
	got, err := run(context.Background(), p, map[string]any{}, &budget{deadline: time.Now().Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().Add(-24 * time.Hour).Unix(); math.Abs(float64(got.(int)-int(want))) > 2 {
		t.Fatalf("unix(now() - 24h) = %v, want about %d", got, want)
	}
	// Bare now is the invocation's env value, not the builtin; immich's manifest reads it as one.
	p, err = compile(`unix(now)`, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := run(context.Background(), p, map[string]any{"now": time.Unix(1000, 0)}, &budget{deadline: time.Now().Add(time.Second)}); err != nil || got != 1000 {
		t.Fatalf("unix(now) = %v, %v; want the env's 1000", got, err)
	}
	if n, err := unix("2025-10-04T17:46:40.9+02:00"); err != nil || n != 1759592800 {
		t.Fatalf(`unix("2025-10-04T17:46:40.9+02:00") = %d, %v`, n, err)
	}
	for _, in := range []any{"yesterday", 1759600000, nil} {
		if _, err := unix(in); err == nil {
			t.Errorf("unix(%v): want an error", in)
		}
	}
}

type queryCapturingBroker struct {
	fixtureBroker
	query map[string]string
}

func (b *queryCapturingBroker) HTTP(_ context.Context, _ capabilities.Grant, r capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	b.query = r.Query
	return capabilities.HTTPResponse{StatusCode: 200, Body: []byte(`{"values":[[1759600000,"1.5"],[1759600060.25,"2"]]}`)}, nil
}

// A manifest reading a Unix-timestamped upstream (Prometheus' query_range is the case that asked
// for this) computes its window from now() and emits points a series block accepts.
func TestUnixTimestampedSeriesEndToEnd(t *testing.T) {
	const manifest = `apiVersion: veduta.dev/v1
kind: Integration
metadata: { id: unixseries, name: Unix series, version: 0.1.0 }
spec:
  runtime: declarative
  slots: [{ name: server, kind: http }]
  capabilities: [http]
  operations:
    - id: op
      routes:
        - { slot: server, method: GET, path: /range, queryKeys: [start, end] }
      pipeline:
        - as: r
          request:
            slot: server
            method: GET
            path: /range
            query:
              start: { expr: 'string(unix(now - duration("1h")))' }
              end:   { expr: 'string(unix(now))' }
      output:
        title: Series
        blocks:
          - type: series
            series:
              - label: Rate
                points:
                  each: { expr: r.values }
                  as: p
                  item: { t: { expr: 'fromUnix(p[0])' }, v: { expr: 'float(p[1])' } }
`
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifestload.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	b := &queryCapturingBroker{}
	inst, err := New(b).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "op", Params: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	start, _ := strconv.ParseInt(b.query["start"], 10, 64)
	end, _ := strconv.ParseInt(b.query["end"], 10, 64)
	if end-start != 3600 || math.Abs(float64(end-time.Now().Unix())) > 2 {
		t.Fatalf("query start=%q end=%q, want the last hour in Unix seconds", b.query["start"], b.query["end"])
	}
	body, _ := json.Marshal(resp.Document.Blocks)
	var blocks []struct {
		Series []struct {
			Points []struct {
				T string  `json:"t"`
				V float64 `json:"v"`
			} `json:"points"`
		} `json:"series"`
	}
	if err := json.Unmarshal(body, &blocks); err != nil || len(blocks) != 1 || len(blocks[0].Series) != 1 {
		t.Fatalf("blocks = %s", body)
	}
	pts := blocks[0].Series[0].Points
	if len(pts) != 2 || pts[0].T != "2025-10-04T17:46:40Z" || pts[0].V != 1.5 || pts[1].T != "2025-10-04T17:47:40.25Z" || pts[1].V != 2 {
		t.Fatalf("points = %+v", pts)
	}
}
