// SPDX-License-Identifier: AGPL-3.0-or-later

// Package docker implements the core-owned, read-only Docker dashboard integration.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/widgets"
)

// Containers returns a Widget Document from Docker's recorded container-list shape.
func Containers(ctx context.Context, registry connections.DockerRegistry, connectionID string) (widgets.Document, error) {
	response, err := registry.DockerGET(ctx, connectionID, "/containers/json?all=1")
	if err != nil {
		return widgets.Document{}, fmt.Errorf("docker: list containers: %w", err)
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return widgets.Document{
			Title:   "Containers",
			Status:  &widgets.Status{Level: widgets.LevelWarn, Text: "Docker API access restricted"},
			Blocks:  []widgets.Block{widgets.BlockList{Empty: "Container listing is not allowed by the socket proxy", Items: []widgets.ListItem{}}},
			Notices: []widgets.Notice{{Level: "warn", Message: "The Docker socket proxy denied container listing"}},
		}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return widgets.Document{}, fmt.Errorf("docker: list containers returned HTTP %d", response.StatusCode)
	}
	var containers []container
	if err := json.Unmarshal(response.Body, &containers); err != nil {
		return widgets.Document{}, fmt.Errorf("docker: decode container list: %w", err)
	}
	if len(containers) > 100 {
		containers = containers[:100]
	}
	items := make([]widgets.ListItem, 0, len(containers))
	running, unhealthy := 0, 0
	for _, item := range containers {
		level := levelFor(item.State, item.Status)
		if item.State == "running" {
			running++
		}
		if level == widgets.LevelWarn || level == widgets.LevelError {
			unhealthy++
		}
		name := strings.TrimPrefix(first(item.Names), "/")
		if name == "" {
			name = shortID(item.ID)
		}
		timestamp := ""
		if item.Created > 0 {
			timestamp = time.Unix(item.Created, 0).UTC().Format(time.RFC3339)
		}
		items = append(items, widgets.ListItem{ID: item.ID, Title: name, Subtitle: item.Image, Value: item.Status, Level: level, Timestamp: timestamp})
	}
	statusLevel := widgets.LevelOK
	if unhealthy > 0 {
		statusLevel = widgets.LevelWarn
	}
	return widgets.Document{
		Title:   "Containers",
		Status:  &widgets.Status{Level: statusLevel, Text: fmt.Sprintf("%d running · %d total", running, len(containers))},
		Blocks:  []widgets.Block{widgets.BlockList{Empty: "No containers", Items: items}},
		Signals: map[string]widgets.Signal{"containers.running": {Value: float64(running), Unit: widgets.UnitCount}, "containers.total": {Value: float64(len(containers)), Unit: widgets.UnitCount}},
	}, nil
}

type container struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	State   string   `json:"State"`
	Status  string   `json:"Status"`
	Created int64    `json:"Created"`
}

func levelFor(state, status string) widgets.Level {
	switch {
	case strings.Contains(strings.ToLower(status), "unhealthy"):
		return widgets.LevelWarn
	case state == "running":
		return widgets.LevelOK
	case state == "exited" || state == "dead":
		return widgets.LevelError
	default:
		return widgets.LevelMuted
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func shortID(id string) string {
	return id[:min(12, len(id))]
}
