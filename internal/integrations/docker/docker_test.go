// SPDX-License-Identifier: AGPL-3.0-or-later

package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/widgets"
)

type recordedDocker struct {
	status int
	body   string
}

func (r recordedDocker) DockerGET(context.Context, string, string) (*connections.Response, error) {
	return &connections.Response{StatusCode: r.status, Body: []byte(r.body)}, nil
}

func TestContainersRendersRecordedEngineResponse(t *testing.T) {
	document, err := Containers(context.Background(), recordedDocker{status: http.StatusOK, body: `[
  {"Id":"abc123","Names":["/api"],"Image":"ghcr.io/acme/api:1","State":"running","Status":"Up 2 hours (healthy)","Created":1788890400},
  {"Id":"def456","Names":["/worker"],"Image":"worker:1","State":"exited","Status":"Exited (1) 5 minutes ago","Created":1788880000}
]`}, "local")
	if err != nil {
		t.Fatal(err)
	}
	if document.Status == nil || document.Status.Level != widgets.LevelWarn || document.Signals["containers.running"].Value != float64(1) {
		t.Fatalf("document=%+v", document)
	}
	list, ok := document.Blocks[0].(widgets.BlockList)
	if !ok || len(list.Items) != 2 || list.Items[0].Title != "api" || list.Items[0].Level != widgets.LevelOK || list.Items[1].Level != widgets.LevelError {
		t.Fatalf("list=%+v", document.Blocks[0])
	}
	if err := validateDocument(document); err != nil {
		t.Fatalf("invalid widget document: %v", err)
	}
}

func TestContainersDegradesWhenSocketProxyForbidsListing(t *testing.T) {
	document, err := Containers(context.Background(), recordedDocker{status: http.StatusForbidden}, "local")
	if err != nil {
		t.Fatal(err)
	}
	if document.Status == nil || document.Status.Level != widgets.LevelWarn || len(document.Notices) != 1 {
		t.Fatalf("document=%+v", document)
	}
	if err := validateDocument(document); err != nil {
		t.Fatalf("invalid degraded document: %v", err)
	}
}

func validateDocument(document widgets.Document) error {
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = widgets.Validate(raw)
	return err
}
