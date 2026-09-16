// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
	"veduta.dev/veduta/internal/widgets"
)

type memoriesBroker struct {
	fixtureBroker
	t      *testing.T
	body   string
	status int
	query  map[string]string
	paths  []string
}

func (b *memoriesBroker) HTTP(_ context.Context, _ capabilities.Grant, r capabilities.HTTPRequest) (capabilities.HTTPResponse, error) {
	b.t.Helper()
	if r.Method != "GET" || r.Path != "/api/memories" || r.Slot != "server" {
		b.t.Fatalf("unexpected request: %#v", r)
	}
	b.query = r.Query
	return capabilities.HTTPResponse{StatusCode: b.status, Body: []byte(b.body)}, nil
}

func (b *memoriesBroker) AssetRef(_ context.Context, _ capabilities.Grant, slot, path string, q url.Values, _ capabilities.Transform) (string, error) {
	if slot != "server" || q.Get("size") != "thumbnail" {
		b.t.Errorf("unexpected asset: %s %v", slot, q)
	}
	b.paths = append(b.paths, path)
	return "v1.a.b", nil
}

// Hand-written from Immich v3.1.0 memory.dto.ts, not live captures.
// Deliberately unordered: limits must apply AFTER year deduplication and sorting.
const memoryFixture = `[
 {"id":"old","type":"on_this_day","data":{"year":2020},"assets":[{"id":"old-photo"}]},
 {"id":"new","type":"on_this_day","data":{"year":2025},"assets":[{"id":"new-photo"},{"id":"video","type":"VIDEO"}]},
 {"id":"duplicate","type":"on_this_day","data":{"year":2025},"assets":[{"id":"other-photo"}]},
 {"id":"fallback","type":"on_this_day","memoryAt":"2024-01-01T00:00:00Z","assets":[{"id":"fallback-photo"}]},
 {"id":"empty","type":"on_this_day","data":{"year":2023},"assets":[]},
 {"id":"unknown","type":"on_this_day","assets":[{"id":"unknown-photo"}]},
 {"id":"future","type":"on_this_day","data":{"year":2027},"assets":[{"id":"future-photo"}]},
 {"id":"current","type":"on_this_day","data":{"year":2026},"assets":[{"id":"current-photo"}]},
 {"id":"other","type":"other","data":{"year":2022},"assets":[{"id":"other-type"}]}
]`

func TestImmichMemories(t *testing.T) {
	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "immich", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, params, day, errContains string
		wantErr                              bool
		status                               int
		titles, subtitles, paths             []string
		link                                 string
	}{
		{name: "sorted unique years with fallback and mixed media", body: memoryFixture, params: `{"date":"2026-01-01","publicUrl":"https://photos.example/immich/"}`, day: "2026-01-01", status: 200,
			titles: []string{"1 year ago", "2 years ago", "6 years ago"}, subtitles: []string{"2 items", "1 item", "1 item"}, paths: []string{"/api/assets/new-photo/thumbnail", "/api/assets/fallback-photo/thumbnail", "/api/assets/old-photo/thumbnail"}, link: "https://photos.example/immich/photos/new-photo"},
		{name: "limit after deduplication", body: memoryFixture, params: `{"date":"2026-01-01","limit":2}`, day: "2026-01-01", status: 200, titles: []string{"1 year ago", "2 years ago"}, subtitles: []string{"2 items", "1 item"}, paths: []string{"/api/assets/new-photo/thumbnail", "/api/assets/fallback-photo/thumbnail"}},
		{name: "new year uses requested year", body: memoryFixture, params: `{"date":"2025-12-31","limit":1}`, day: "2025-12-31", status: 200, titles: []string{"1 year ago"}, subtitles: []string{"1 item"}, paths: []string{"/api/assets/fallback-photo/thumbnail"}},
		{name: "empty today defaults", body: "[]", params: "{}", status: 200},
		{name: "upstream permission error", body: `{"error":"denied"}`, params: `{"date":"2026-01-01"}`, status: 403, wantErr: true, errContains: "403"},
		{name: "invalid calendar date", body: "[]", params: `{"date":"2026-02-30"}`, status: 200, wantErr: true},
		{name: "malformed fallback fails visibly", body: `[{"id":"bad","type":"on_this_day","memoryAt":"not-a-date","assets":[{"id":"a"}]}]`, params: `{"date":"2026-01-01"}`, status: 200, wantErr: true},
		{name: "reject oversized limit", body: "[]", params: `{"limit":10}`, status: 200, wantErr: true},
		{name: "reject credentials in public link", body: "[]", params: `{"publicUrl":"https://user:secret@photos.example"}`, status: 200, wantErr: true},
		{name: "reject unsafe link", body: "[]", params: `{"publicUrl":"javascript:alert(1)"}`, status: 200, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &memoriesBroker{t: t, body: tc.body, status: tc.status}
			inst, err := New(b).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
			if err != nil {
				t.Fatal(err)
			}
			before := time.Now().UTC().Format("2006-01-02")
			resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "memories", Params: json.RawMessage(tc.params)})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
				}
				if len(b.paths) > 0 {
					t.Fatal("minted assets on failed invocation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			day := tc.day
			if day == "" {
				day = b.query["for"]
				if day != before && day != time.Now().UTC().Format("2006-01-02") {
					t.Fatalf("default day = %q", day)
				}
			}
			if !reflect.DeepEqual(b.query, map[string]string{"for": day, "type": "on_this_day", "order": "desc"}) {
				t.Fatalf("query = %v", b.query)
			}
			grid := resp.Document.Blocks[0].(widgets.BlockMedia)
			if grid.Columns != 3 || grid.Empty != "No memories on this day" {
				t.Fatalf("grid = %#v", grid)
			}
			if len(grid.Items) != len(tc.titles) {
				t.Fatalf("items = %#v", grid.Items)
			}
			for i, item := range grid.Items {
				if item.Title != tc.titles[i] || item.Subtitle != tc.subtitles[i] {
					t.Errorf("item %d = %#v", i, item)
				}
				if item.Image.Ref != "v1.a.b" || item.Image.Alt == "" {
					t.Errorf("image = %#v", item.Image)
				}
				if i == 0 && item.Link != tc.link {
					t.Errorf("link = %q", item.Link)
				}
			}
			if !reflect.DeepEqual(b.paths, tc.paths) {
				t.Errorf("assets = %v, want %v", b.paths, tc.paths)
			}
		})
	}
}

// A realistic large collection stays within the manifest's ordinary iteration
// budget, while defaults and the maximum limit still select the newest years.
func TestImmichMemoriesLimitsAndBudget(t *testing.T) {
	m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", "immich", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	memories := make([]any, 60)
	for i := range memories {
		memories[i] = map[string]any{"id": fmt.Sprint(i), "type": "on_this_day", "data": map[string]any{"year": 1966 + i}, "assets": []any{map[string]any{"id": fmt.Sprint(i)}}}
	}
	body, err := json.Marshal(memories)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		params string
		count  int
	}{
		{params: `{"date":"2026-01-01"}`, count: 6},
		{params: `{"date":"2026-01-01","limit":9}`, count: 9},
	} {
		b := &memoriesBroker{t: t, body: string(body), status: 200}
		inst, err := New(b).Load(context.Background(), integrations.Installed{Manifest: m, Lock: &integrations.LockEntry{ManifestSHA256: m.Digest}})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{Operation: "memories", Params: json.RawMessage(tc.params)})
		if err != nil {
			t.Fatal(err)
		}
		grid := resp.Document.Blocks[0].(widgets.BlockMedia)
		if len(grid.Items) != tc.count || grid.Items[0].Title != "1 year ago" || grid.Items[tc.count-1].Title != fmt.Sprintf("%d years ago", tc.count) {
			t.Fatalf("unexpected selection: %#v", grid.Items)
		}
	}
}
