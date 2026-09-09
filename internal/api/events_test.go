// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"veduta.dev/veduta/internal/storage"
)

func TestEventsEndpointReturnsNewestPersistentEvents(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err = store.AppendEvent(context.Background(), storage.Event{Type: "rule.fired", Severity: "warn", CardID: "cpu", Message: "CPU high"}); err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{Listen: "127.0.0.1:0", EventStore: store, Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=10", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"type":"rule.fired"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
