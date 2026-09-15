// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	"veduta.dev/veduta/internal/integrations/manifestload"
	"veduta.dev/veduta/internal/widgets"
)

// The responses below are HAND-WRITTEN from each upstream's own source of truth, not captured from
// a live server the way testdata/upstream is - every field name is cited where it came from. That
// is weaker evidence than an S2-style capture and is deliberately kept distinguishable from one:
// it proves the manifest's expressions are consistent with the documented response shape, not that
// the shape is what a particular release actually emits.
//
// What the assertions are for: a declarative manifest's real failure mode is an expression that
// compiles and then reads a field that is spelled differently, nested one level deeper, or typed
// as a string upstream. Loading the manifest cannot catch any of those. Running it against a
// response shaped like the real one can, and the expected signal values are arithmetic done by
// hand so a wrong aggregation cannot agree with them by accident.
func TestFirstPartyManifests_AgainstDocumentedResponseShapes(t *testing.T) {
	cases := []struct {
		plugin    string
		operation string
		params    string
		responses map[string]string
		// wantSignals is exact: every declared signal the operation should emit, and its value.
		wantSignals map[string]float64
		wantStrings map[string]string
		// wantNoSignals names signals that must be ABSENT, which is the only way a rule can tell
		// "we could not read this" apart from a stale number.
		wantNoSignals []string
		wantStatus    string
		// wantFirstListItem guards the string munging each manifest does for display, which is
		// where upstream naming conventions (Docker's leading slash, friendly_name) actually bite.
		wantFirstListItem string
	}{{
		// Fields per the Proxmox VE API viewer for GET /cluster/resources: type, status, node,
		// name, cpu (a 0..1 fraction of maxcpu), maxcpu, mem/maxmem in bytes, and for storage
		// entries storage/disk/maxdisk. pve3 is deliberately "unknown" - an unreachable node is
		// still listed, which is exactly the case the status line exists to report.
		plugin:    "proxmox",
		operation: "cluster-overview",
		params:    `{}`,
		responses: map[string]string{"/api2/json/cluster/resources": `{"data":[
			{"id":"node/pve1","type":"node","node":"pve1","status":"online","cpu":0.25,"maxcpu":16,"mem":8589934592,"maxmem":34359738368,"uptime":864000},
			{"id":"node/pve2","type":"node","node":"pve2","status":"online","cpu":0.5,"maxcpu":4,"mem":2147483648,"maxmem":8589934592,"uptime":432000},
			{"id":"node/pve3","type":"node","node":"pve3","status":"unknown","cpu":0,"maxcpu":8,"mem":0,"maxmem":0,"uptime":0},
			{"id":"qemu/100","type":"qemu","node":"pve1","status":"running","name":"vaultwarden","vmid":100,"cpu":0.02,"maxcpu":2,"mem":536870912,"maxmem":2147483648},
			{"id":"qemu/101","type":"qemu","node":"pve1","status":"stopped","name":"windows","vmid":101,"cpu":0,"maxcpu":4,"mem":0,"maxmem":8589934592},
			{"id":"lxc/200","type":"lxc","node":"pve2","status":"running","name":"adguard","vmid":200,"cpu":0.01,"maxcpu":1,"mem":134217728,"maxmem":536870912},
			{"id":"storage/pve1/local","type":"storage","node":"pve1","storage":"local","status":"available","disk":53687091200,"maxdisk":107374182400}
		]}`},
		wantSignals: map[string]float64{
			"nodes.online": 2, "nodes.total": 3,
			"vms.running": 1, "vms.total": 2,
			"lxc.running": 1, "lxc.total": 1,
			// Core-weighted across the two ONLINE nodes only: (0.25*16 + 0.5*4) / (16+4) = 30%.
			// A plain mean of the three nodes' cpu fields would be 25%, and of the two online
			// ones 37.5% - neither of which this assertion accepts.
			"cpu.percent": 30,
			// (8 GiB + 2 GiB) / (32 GiB + 8 GiB) = 25%. pve3 contributes to neither side.
			"mem.percent": 25,
		},
		wantStatus:        "2 of 3 nodes online",
		wantFirstListItem: "adguard", // sorted by name: adguard, vaultwarden, windows
	}, {
		// Home Assistant's GET /api/states returns a bare array of state objects, each with
		// entity_id, state, attributes and last_changed - the shape documented in its REST API
		// reference. "unavailable" is an integration that is not answering; "unknown" is an
		// entity that simply has no reading yet, and must NOT be counted as a fault.
		plugin:    "homeassistant",
		operation: "overview",
		params:    `{}`,
		responses: map[string]string{"/api/states": `[
			{"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen"},"last_changed":"2026-09-15T08:00:00+00:00"},
			{"entity_id":"light.hall","state":"off","attributes":{"friendly_name":"Hall"},"last_changed":"2026-09-15T07:00:00+00:00"},
			{"entity_id":"switch.pump","state":"on","attributes":{"friendly_name":"Pump"},"last_changed":"2026-09-15T06:00:00+00:00"},
			{"entity_id":"person.sam","state":"home","attributes":{"friendly_name":"Sam"},"last_changed":"2026-09-15T05:00:00+00:00"},
			{"entity_id":"binary_sensor.door","state":"unknown","attributes":{},"last_changed":"2026-09-15T04:00:00+00:00"},
			{"entity_id":"sensor.outside_temperature","state":"unavailable","attributes":{"friendly_name":"Outside","unit_of_measurement":"°C"},"last_changed":"2026-09-15T03:00:00+00:00"}
		]`},
		wantSignals: map[string]float64{
			"entities": 6, "unavailable": 1,
			"lights.on": 1, "switches.on": 1, "persons.home": 1,
		},
		wantStatus:        "6 entities",
		wantFirstListItem: "Outside",
	}, {
		// One sensor, selected out of the full state array because a v1 manifest cannot build
		// /api/states/<entity_id> from a parameter. The numeric signal exists only because this
		// entity carries a unit_of_measurement and is currently reporting.
		plugin:    "homeassistant",
		operation: "sensor",
		params:    `{"entityId":"sensor.outside_temperature"}`,
		responses: map[string]string{"/api/states": `[
			{"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen"},"last_changed":"2026-09-15T08:00:00+00:00"},
			{"entity_id":"sensor.outside_temperature","state":"21.5","attributes":{"friendly_name":"Outside","unit_of_measurement":"\u00b0C"},"last_changed":"2026-09-15T08:00:00+00:00"}
		]`},
		wantSignals:       map[string]float64{"value": 21.5},
		wantStrings:       map[string]string{"state": "21.5"},
		wantStatus:        "21.5",
		wantFirstListItem: "21.5 \u00b0C",
	}, {
		// The same card after someone renamed the entity in Home Assistant, which is a two-click
		// operation that tells the dashboard nothing. The card must report the entity as missing
		// rather than render an empty panel that looks healthy - and must emit NEITHER signal, so
		// that a rule over it reads unknown instead of holding its last value forever.
		plugin:    "homeassistant",
		operation: "sensor",
		params:    `{"entityId":"sensor.renamed_away"}`,
		responses: map[string]string{"/api/states": `[
			{"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen"},"last_changed":"2026-09-15T08:00:00+00:00"}
		]`},
		wantNoSignals: []string{"state", "value"},
		wantStatus:    "no such entity",
	}, {
		// Arcane's list endpoint returns base.PaginatedWithCounts: success, data (the page),
		// counts (the WHOLE environment, not the page) and pagination. Field names are from
		// types/base/response.go and types/container/container.go in getarcaneapp/arcane; the
		// counts struct is runningContainers/stoppedContainers/totalContainers.
		plugin:    "arcane",
		operation: "containers",
		params:    `{}`,
		responses: map[string]string{"/api/environments/0/containers": `{"success":true,
			"data":[
				{"id":"a1","names":["/immich_server"],"image":"immich:v3","state":"running","status":"Up 3 days"},
				{"id":"b2","names":["/old_thing"],"image":"busybox","state":"exited","status":"Exited (0) 2 days ago"}],
			"counts":{"runningContainers":11,"stoppedContainers":2,"totalContainers":13},
			"pagination":{"totalPages":2,"totalItems":13,"currentPage":1,"itemsPerPage":8}}`},
		wantSignals: map[string]float64{
			"containers.running": 11, "containers.stopped": 2, "containers.total": 13,
		},
		wantStatus: "11 of 13 running",
		// Docker keeps the leading slash on every name; the card must not.
		wantFirstListItem: "immich_server",
	}, {
		// Dockhand's GET /api/dashboard/stats returns one EnvironmentStats object per environment
		// when ?env is omitted - the interface in src/routes/api/dashboard/stats/+server.ts. The
		// second environment is offline, which is what makes the aggregation worth asserting:
		// counts must still sum across it rather than being skipped or double-counted.
		plugin:    "dockhand",
		operation: "overview",
		params:    `{}`,
		responses: map[string]string{"/api/dashboard/stats": `[
			{"id":1,"name":"local","online":true,
			 "containers":{"total":13,"running":11,"stopped":2,"paused":0,"restarting":0,"unhealthy":1,"pendingUpdates":3},
			 "images":{"total":24,"totalSize":0},"volumes":{"total":9,"totalSize":0},"networks":{"total":4},
			 "stacks":{"total":5,"running":4,"partial":1,"stopped":0},
			 "metrics":{"cpuPercent":12,"memoryPercent":40,"memoryUsed":0,"memoryTotal":0},
			 "events":{"total":0,"today":0}},
			{"id":2,"name":"nas","online":false,
			 "containers":{"total":6,"running":0,"stopped":6,"paused":0,"restarting":0,"unhealthy":0,"pendingUpdates":0},
			 "images":{"total":8,"totalSize":0},"volumes":{"total":2,"totalSize":0},"networks":{"total":1},
			 "stacks":{"total":2,"running":0,"partial":0,"stopped":2},
			 "metrics":null,"events":{"total":0,"today":0}}
		]`},
		wantSignals: map[string]float64{
			"environments.online": 1, "environments.total": 2,
			"containers.running": 11, "containers.total": 19,
			"containers.unhealthy": 1, "containers.pending_updates": 3,
			"stacks.running": 4, "images.total": 32,
		},
		wantStatus:        "1 of 2 environments online",
		wantFirstListItem: "local",
	}}

	for _, tc := range cases {
		t.Run(tc.plugin+"/"+tc.operation, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, ok := tc.responses[r.URL.Path]
				if !ok {
					t.Errorf("unexpected upstream request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", tc.plugin, "manifest.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			op := operation(t, m, tc.operation)

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
			resp, err := inst.Invoke(context.Background(), integrations.InvokeRequest{
				Operation: tc.operation, Params: json.RawMessage(tc.params), Grant: grant,
			})
			if err != nil {
				t.Fatalf("invoke %s/%s: %v", tc.plugin, tc.operation, err)
			}

			for name, want := range tc.wantSignals {
				signal, ok := resp.Document.Signals[name]
				if !ok {
					t.Errorf("signal %q was not emitted", name)
					continue
				}
				got, ok := signal.Value.(float64)
				if !ok {
					t.Errorf("signal %q is %T (%v), want a number", name, signal.Value, signal.Value)
					continue
				}
				if got != want {
					t.Errorf("signal %q = %v, want %v", name, got, want)
				}
			}
			for name, want := range tc.wantStrings {
				signal, ok := resp.Document.Signals[name]
				if !ok {
					t.Errorf("signal %q was not emitted", name)
					continue
				}
				if got, _ := signal.Value.(string); got != want {
					t.Errorf("signal %q = %v, want %q", name, signal.Value, want)
				}
			}
			for _, name := range tc.wantNoSignals {
				if signal, present := resp.Document.Signals[name]; present {
					t.Errorf("signal %q was emitted as %v, but there was nothing to read", name, signal.Value)
				}
			}
			// Every signal the operation declares must be either emitted or deliberately
			// conditional; an undeclared one cannot be referenced by a rule at all.
			declared := map[string]bool{}
			for _, s := range op.Signals {
				declared[s.Name] = true
			}
			for name := range resp.Document.Signals {
				if !declared[name] {
					t.Errorf("document emits signal %q, which the manifest does not declare", name)
				}
			}
			if tc.wantStatus != "" {
				if resp.Document.Status == nil {
					t.Fatalf("no status on the document")
				}
				if resp.Document.Status.Text != tc.wantStatus {
					t.Errorf("status text = %q, want %q", resp.Document.Status.Text, tc.wantStatus)
				}
			}
			if tc.wantFirstListItem != "" {
				if got := firstListTitle(resp.Document.Blocks); got != tc.wantFirstListItem {
					t.Errorf("first list item = %q, want %q", got, tc.wantFirstListItem)
				}
			}
		})
	}
}

// TestFirstPartyManifests_DeclareOnlyReadRoutes is the claim each new manifest's header comment
// makes, checked rather than trusted: these integrations watch infrastructure that can be
// destroyed through the very APIs they talk to, and a route grant is the only thing standing
// between an approved manifest and a POST .../stop.
func TestFirstPartyManifests_DeclareOnlyReadRoutes(t *testing.T) {
	for _, plugin := range []string{"proxmox", "homeassistant", "arcane", "dockhand"} {
		t.Run(plugin, func(t *testing.T) {
			m, err := manifestload.Load(filepath.Join("..", "..", "..", "plugins", plugin, "manifest.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range m.Operations {
				for _, r := range op.Routes {
					if r.Method != http.MethodGet && r.Method != http.MethodHead {
						t.Errorf("operation %q declares %s %s; these integrations must be read-only", op.ID, r.Method, r.Path)
					}
				}
			}
		})
	}
}

func operation(t *testing.T, m *manifestload.Manifest, id string) manifestload.OperationDef {
	t.Helper()
	for _, op := range m.Operations {
		if op.ID == id {
			return op
		}
	}
	t.Fatalf("manifest %s has no operation %q", m.ID, id)
	return manifestload.OperationDef{}
}

func firstListTitle(blocks []widgets.Block) string {
	for _, b := range blocks {
		list, ok := b.(widgets.BlockList)
		if ok && len(list.Items) > 0 {
			return list.Items[0].Title
		}
	}
	return ""
}
