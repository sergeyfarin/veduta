// SPDX-License-Identifier: AGPL-3.0-or-later

package homepageimport

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"veduta.dev/veduta/internal/config"
)

type mappingSpec struct {
	integration string
	operation   string
	header      string
}

// The common Homepage widget surface is classified explicitly. Empty integration means its
// service/link is imported intact while widget-specific metrics await a Veduta integration.
var widgetMappings = map[string]mappingSpec{
	"adguard": {}, "authentik": {}, "bazarr": {header: "X-Api-Key"},
	"beszel": {integration: "beszel", operation: "overview"}, "calibreweb": {},
	"changedetectionio": {}, "cloudflared": {}, "coinmarketcap": {}, "deluge": {},
	"docker": {}, "emby": {}, "frigate": {},
	"glances": {integration: "glances", operation: "overview"}, "homeassistant": {},
	"immich":   {integration: "immich", operation: "recent-assets", header: "x-api-key"},
	"jellyfin": {integration: "jellyfin", operation: "recently-added", header: "X-Emby-Token"},
	"komga":    {}, "lidarr": {header: "X-Api-Key"}, "nextcloud": {},
	"nginxproxymanager": {}, "omada": {}, "openweathermap": {}, "overseerr": {},
	"pihole": {}, "plex": {header: "X-Plex-Token"}, "portainer": {},
	"prowlarr": {header: "X-Api-Key"}, "proxmox": {}, "qbittorrent": {},
	"radarr": {header: "X-Api-Key"}, "sabnzbd": {}, "sonarr": {header: "X-Api-Key"}, "speedtest": {},
	"tautulli": {}, "traefik": {}, "transmission": {}, "truenas": {}, "uptimekuma": {},
	"watchtower": {},
}

// MapOptions controls paths written for integrations that ship as separate plugins.
type MapOptions struct {
	PluginDir string
}

// Report is the deterministic summary printed after an import.
type Report struct {
	Services             int
	Complete             int
	WithoutWidgets       int
	NeedManual           int
	EnvironmentVariables []string
}

// Map converts parsed Homepage input into an additive Veduta configuration patch. Every imported
// connection and plugin declaration is disabled until an operator reviews it.
func Map(model Model, existing *config.Snapshot, options MapOptions) (config.ApplyPatch, Report, []Warning) {
	if options.PluginDir == "" {
		options.PluginDir = "plugins"
	}
	mapper := newMapper(existing, options)
	for _, group := range model.Groups {
		mapper.mapGroup(group, "")
	}
	for _, group := range model.Bookmarks {
		cards := make([]any, 0, len(group.Bookmarks))
		for _, bookmark := range group.Bookmarks {
			cards = append(cards, map[string]any{"id": mapper.uniqueCardID(bookmark.Name), "title": bookmark.Name, "href": bookmark.Href, "icon": bookmark.Icon})
		}
		if len(cards) > 0 {
			mapper.patch.Sections = append(mapper.patch.Sections, map[string]any{"title": group.Name, "cards": cards})
		}
	}
	sort.Strings(mapper.report.EnvironmentVariables)
	return mapper.patch, mapper.report, mapper.warnings
}

type mapper struct {
	options           MapOptions
	patch             config.ApplyPatch
	report            Report
	warnings          []Warning
	cardIDs           map[string]bool
	connectionIDs     map[string]bool
	integrationIDs    map[string]bool
	connectionsByURL  map[string]string
	declaredInPatch   map[string]bool
	environmentValues map[string]bool
}

func newMapper(existing *config.Snapshot, options MapOptions) *mapper {
	m := &mapper{options: options, patch: config.ApplyPatch{Connections: map[string]map[string]any{}}, cardIDs: map[string]bool{}, connectionIDs: map[string]bool{}, integrationIDs: map[string]bool{}, connectionsByURL: map[string]string{}, declaredInPatch: map[string]bool{}, environmentValues: map[string]bool{}}
	if existing == nil {
		return m
	}
	for id, connection := range existing.Config.Connections {
		m.connectionIDs[id] = true
		if connection.HTTP != nil {
			if normal, ok := normalBaseURL(connection.HTTP.BaseURL); ok {
				m.connectionsByURL[normal] = id
			}
		}
	}
	for _, integration := range existing.Config.Integrations {
		m.integrationIDs[integration.ID] = true
	}
	for _, section := range existing.Config.Sections {
		for _, card := range section.Cards {
			m.cardIDs[card.ID] = true
		}
	}
	return m
}

func (m *mapper) mapGroup(group Group, parent string) {
	title := group.Name
	if parent != "" {
		title = parent + " / " + group.Name
	}
	cards := make([]any, 0, len(group.Services))
	for _, service := range group.Services {
		cards = append(cards, m.mapService(service, title))
	}
	if len(cards) > 0 {
		m.patch.Sections = append(m.patch.Sections, map[string]any{"title": title, "cards": cards})
	}
	for _, child := range group.Groups {
		m.mapGroup(child, title)
	}
}

func (m *mapper) mapService(service Service, sourcePath string) map[string]any {
	m.report.Services++
	card := map[string]any{"id": m.uniqueCardID(service.Name), "title": service.Name}
	setNonEmpty(card, "href", service.Href)
	setNonEmpty(card, "icon", service.Icon)
	if len(service.Widgets) == 0 {
		m.report.WithoutWidgets++
		return card
	}
	widget := service.Widgets[0]
	widgetType := strings.ToLower(widget.Type)
	spec, known := widgetMappings[widgetType]
	if !known {
		m.report.NeedManual++
		m.warnings = append(m.warnings, Warning{Source: "mapping", Path: sourcePath + "." + service.Name, Message: fmt.Sprintf("widget type %q needs manual mapping; imported as a link-only card", widget.Type)})
		return card
	}
	connectionID := ""
	if widget.URL != "" {
		connectionID = m.connectionFor(service.Name, widget, spec)
	}
	if spec.integration == "" {
		m.report.Complete++
		m.warnings = append(m.warnings, Warning{Source: "mapping", Path: sourcePath + "." + service.Name, Message: fmt.Sprintf("%s widget has no Veduta metrics integration; imported as a link-only card", widget.Type)})
		return card
	}
	if connectionID == "" {
		m.report.NeedManual++
		m.warnings = append(m.warnings, Warning{Source: "mapping", Path: sourcePath + "." + service.Name, Message: fmt.Sprintf("%s widget has no usable URL; imported as a link-only card", widget.Type)})
		return card
	}
	m.declarePlugin(spec.integration)
	card["integration"] = spec.integration
	card["operation"] = spec.operation
	card["slots"] = map[string]any{"server": connectionID}
	card["refresh"] = "5m"
	m.report.Complete++
	if len(service.Widgets) > 1 {
		m.warnings = append(m.warnings, Warning{Source: "mapping", Path: sourcePath + "." + service.Name, Message: "additional widgets retained by K1 but only the first widget maps to a Veduta card"})
	}
	return card
}

func (m *mapper) connectionFor(serviceName string, widget Widget, spec mappingSpec) string {
	baseURL, ok := normalBaseURL(widget.URL)
	if !ok {
		m.warnings = append(m.warnings, Warning{Source: "mapping", Path: serviceName + ".widget.url", Message: "invalid HTTP URL; no connection created"})
		return ""
	}
	if id := m.connectionsByURL[baseURL]; id != "" {
		return id
	}
	id := m.uniqueConnectionID(serviceName)
	connection := map[string]any{"kind": "http", "enabled": false, "baseUrl": baseURL}
	if widget.Key != "" {
		environmentName := "HOMEPAGE_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_KEY"
		auth := map[string]any{"type": "bearer", "value": "${secret:" + environmentName + "}"}
		if spec.header != "" {
			auth = map[string]any{"type": "header", "name": spec.header, "value": "${secret:" + environmentName + "}"}
		}
		connection["auth"] = auth
		if !m.environmentValues[environmentName] {
			m.environmentValues[environmentName] = true
			m.report.EnvironmentVariables = append(m.report.EnvironmentVariables, environmentName)
		}
	}
	m.patch.Connections[id] = connection
	m.connectionsByURL[baseURL] = id
	return id
}

func (m *mapper) declarePlugin(id string) {
	if m.integrationIDs[id] || m.declaredInPatch[id] {
		return
	}
	m.declaredInPatch[id] = true
	source := strings.TrimRight(m.options.PluginDir, "/\\") + "/" + id
	m.patch.Integrations = append(m.patch.Integrations, map[string]any{"id": id, "source": "path:" + source, "enabled": false, "description": "Imported from Homepage; review source, connection and permissions before enabling"})
}

func (m *mapper) uniqueCardID(name string) string {
	return uniqueID(slug(name), m.cardIDs, 64)
}

func (m *mapper) uniqueConnectionID(name string) string {
	return uniqueID(slug(name), m.connectionIDs, 64)
}

func uniqueID(base string, used map[string]bool, limit int) string {
	if base == "" {
		base = "imported"
	}
	if len(base) > limit {
		base = strings.Trim(base[:limit], "-")
	}
	candidate := base
	for suffix := 2; used[candidate]; suffix++ {
		ending := fmt.Sprintf("-%d", suffix)
		prefix := base
		if len(prefix)+len(ending) > limit {
			prefix = strings.Trim(prefix[:limit-len(ending)], "-")
		}
		candidate = prefix + ending
	}
	used[candidate] = true
	return candidate
}

func slug(value string) string {
	var out strings.Builder
	dash := false
	for _, char := range strings.ToLower(value) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			out.WriteRune(char)
			dash = false
		} else if !dash && out.Len() > 0 && (unicode.IsSpace(char) || unicode.IsPunct(char) || unicode.IsSymbol(char)) {
			out.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func normalBaseURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), true
}

func setNonEmpty(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}
