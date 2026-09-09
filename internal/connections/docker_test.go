// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/config"
)

func TestDockerConnectionUsesFixedReadOnlySurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "1" {
			t.Errorf("request %s %s", r.Method, r.URL.String())
		}
		_, _ = io.WriteString(w, `[{"Id":"abc"}]`)
	}))
	defer server.Close()
	endpoint := "tcp://" + strings.TrimPrefix(server.URL, "http://")
	registry, err := New(map[string]config.Connection{"local": {Kind: "docker", Docker: &config.DockerConnection{Endpoint: endpoint}}}, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	_, doErr := registry.Do(context.Background(), "local", Request{Method: http.MethodGet, Path: "/containers/json"})
	if !errors.Is(doErr, ErrNotHTTP) {
		t.Fatalf("plugin-facing HTTP surface reached Docker: %v", doErr)
	}
	docker := registry.(DockerRegistry)
	response, err := docker.DockerGET(context.Background(), "local", "/containers/json?all=1")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if _, err := docker.DockerGET(context.Background(), "local", "/containers/abc/stop"); !errors.Is(err, ErrDockerPath) {
		t.Fatalf("write endpoint was not refused: %v", err)
	}
}
