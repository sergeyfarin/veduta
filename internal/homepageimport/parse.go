// SPDX-License-Identifier: AGPL-3.0-or-later

package homepageimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var knownWidgetTypes = map[string]struct{}{
	"adguard": {}, "authentik": {}, "bazarr": {}, "beszel": {}, "calibreweb": {},
	"changedetectionio": {}, "cloudflared": {}, "coinmarketcap": {}, "deluge": {},
	"docker": {}, "emby": {}, "frigate": {}, "glances": {}, "homeassistant": {},
	"immich": {}, "jellyfin": {}, "komga": {}, "lidarr": {}, "nextcloud": {},
	"nginxproxymanager": {}, "omada": {}, "openweathermap": {}, "overseerr": {},
	"pihole": {}, "plex": {}, "portainer": {}, "prowlarr": {}, "proxmox": {},
	"qbittorrent": {}, "radarr": {}, "sabnzbd": {}, "sonarr": {}, "speedtest": {},
	"tautulli": {}, "traefik": {}, "transmission": {}, "truenas": {}, "uptimekuma": {},
	"watchtower": {}, "resources": {}, "search": {}, "datetime": {}, "greeting": {},
	"kubernetes": {}, "openmeteo": {}, "logo": {}, "longhorn": {},
}

// ParseDir reads the four conventional Homepage YAML files. Missing individual files are valid;
// Homepage installations commonly use only services.yaml.
func ParseDir(dir string) (Model, []Warning, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return Model{}, nil, err
	}
	if !info.IsDir() {
		return Model{}, nil, fmt.Errorf("%s: not a directory", dir)
	}

	model := Model{Settings: map[string]any{}}
	var warnings []Warning
	if err = readOptionalYAML(filepath.Join(dir, "services.yaml"), func(value any) error {
		var parseWarnings []Warning
		model.Groups, parseWarnings, err = parseGroups(value, "services.yaml")
		warnings = append(warnings, parseWarnings...)
		return err
	}); err != nil {
		return Model{}, warnings, err
	}
	if err = readOptionalYAML(filepath.Join(dir, "bookmarks.yaml"), func(value any) error {
		model.Bookmarks, err = parseBookmarks(value)
		return err
	}); err != nil {
		return Model{}, warnings, err
	}
	if err = readOptionalYAML(filepath.Join(dir, "settings.yaml"), func(value any) error {
		var ok bool
		model.Settings, ok = value.(map[string]any)
		if !ok {
			return errors.New("settings.yaml: top level must be a mapping")
		}
		return nil
	}); err != nil {
		return Model{}, warnings, err
	}
	if err = readOptionalYAML(filepath.Join(dir, "widgets.yaml"), func(value any) error {
		var parseWarnings []Warning
		model.Widgets, parseWarnings, err = parseGlobalWidgets(value)
		warnings = append(warnings, parseWarnings...)
		return err
	}); err != nil {
		return Model{}, warnings, err
	}
	return model, warnings, nil
}

func readOptionalYAML(path string, consume func(any) error) error {
	// #nosec G304 -- the directory is explicitly supplied by the CLI caller and filenames are fixed.
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var value any
	if err = yaml.Unmarshal(body, &value); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return consume(value)
}

func parseGroups(value any, source string) ([]Group, []Warning, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s: top level must be a sequence", source)
	}
	groups := make([]Group, 0, len(items))
	var warnings []Warning
	for index, item := range items {
		name, children, err := namedEntry(item)
		if err != nil {
			return nil, warnings, fmt.Errorf("%s[%d]: %w", source, index, err)
		}
		group, childWarnings, err := parseGroup(name, children, source, name)
		warnings = append(warnings, childWarnings...)
		if err != nil {
			return nil, warnings, err
		}
		groups = append(groups, group)
	}
	return groups, warnings, nil
}

func parseGroup(name string, value any, source, path string) (Group, []Warning, error) {
	items, ok := value.([]any)
	if !ok {
		return Group{}, nil, fmt.Errorf("%s:%s: group contents must be a sequence", source, path)
	}
	group := Group{Name: name}
	var warnings []Warning
	for index, item := range items {
		childName, childValue, err := namedEntry(item)
		if err != nil {
			return Group{}, warnings, fmt.Errorf("%s:%s[%d]: %w", source, path, index, err)
		}
		childPath := path + "." + childName
		switch typed := childValue.(type) {
		case []any:
			child, childWarnings, childErr := parseGroup(childName, typed, source, childPath)
			warnings = append(warnings, childWarnings...)
			if childErr != nil {
				return Group{}, warnings, childErr
			}
			group.Groups = append(group.Groups, child)
		case map[string]any:
			service, serviceWarnings := parseService(childName, typed, source, childPath)
			warnings = append(warnings, serviceWarnings...)
			group.Services = append(group.Services, service)
		default:
			return Group{}, warnings, fmt.Errorf("%s:%s: service must be a mapping or nested group", source, childPath)
		}
	}
	return group, warnings, nil
}

func namedEntry(value any) (string, any, error) {
	mapping, ok := value.(map[string]any)
	if !ok || len(mapping) != 1 {
		return "", nil, errors.New("entry must be a single-key mapping")
	}
	for name, child := range mapping {
		if strings.TrimSpace(name) == "" {
			return "", nil, errors.New("entry name must not be empty")
		}
		return name, child, nil
	}
	panic("single-key mapping had no key")
}

func parseService(name string, raw map[string]any, source, path string) (Service, []Warning) {
	service := Service{Name: name, Extra: map[string]any{}}
	var warnings []Warning
	for key, value := range raw {
		switch key {
		case "href":
			service.Href = scalarString(value)
		case "description":
			service.Description = scalarString(value)
		case "icon":
			service.Icon = scalarString(value)
		case "ping":
			service.Ping = scalarString(value)
		case "siteMonitor":
			service.SiteMonitor = scalarString(value)
		case "server":
			service.Server = scalarString(value)
		case "container":
			service.Container = scalarString(value)
		case "widget":
			if widgetMap, ok := value.(map[string]any); ok {
				widget, widgetWarnings := parseWidget(widgetMap, source, path+".widget")
				warnings = append(warnings, widgetWarnings...)
				service.Widgets = append(service.Widgets, widget)
			} else {
				warnings = append(warnings, Warning{Source: source, Path: path + ".widget", Message: "widget must be a mapping; ignored"})
			}
		case "widgets":
			widgets, widgetWarnings := parseWidgetList(value, source, path+".widgets")
			warnings = append(warnings, widgetWarnings...)
			service.Widgets = append(service.Widgets, widgets...)
		default:
			service.Extra[key] = value
		}
	}
	return service, warnings
}

func parseWidgetList(value any, source, path string) ([]Widget, []Warning) {
	items, ok := value.([]any)
	if !ok {
		return nil, []Warning{{Source: source, Path: path, Message: "widgets must be a sequence; ignored"}}
	}
	widgets := make([]Widget, 0, len(items))
	var warnings []Warning
	for index, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			warnings = append(warnings, Warning{Source: source, Path: fmt.Sprintf("%s[%d]", path, index), Message: "widget must be a mapping; ignored"})
			continue
		}
		widget, itemWarnings := parseWidget(mapping, source, fmt.Sprintf("%s[%d]", path, index))
		warnings = append(warnings, itemWarnings...)
		widgets = append(widgets, widget)
	}
	return widgets, warnings
}

func parseWidget(raw map[string]any, source, path string) (Widget, []Warning) {
	widget := Widget{Options: map[string]any{}}
	for key, value := range raw {
		switch key {
		case "type":
			widget.Type = scalarString(value)
		case "url":
			widget.URL = scalarString(value)
		case "key":
			widget.Key = scalarString(value)
		case "fields":
			widget.Fields = stringSlice(value)
		default:
			widget.Options[key] = value
		}
	}
	if widget.Type == "" {
		return widget, []Warning{{Source: source, Path: path, Message: "widget has no type"}}
	}
	if _, ok := knownWidgetTypes[strings.ToLower(widget.Type)]; !ok {
		return widget, []Warning{{Source: source, Path: path, Message: fmt.Sprintf("unknown widget type %q; retained for manual mapping", widget.Type)}}
	}
	return widget, nil
}

func parseGlobalWidgets(value any) ([]Widget, []Warning, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, nil, errors.New("widgets.yaml: top level must be a sequence")
	}
	widgets := make([]Widget, 0, len(items))
	var warnings []Warning
	for index, item := range items {
		name, config, err := namedEntry(item)
		if err != nil {
			return nil, warnings, fmt.Errorf("widgets.yaml[%d]: %w", index, err)
		}
		raw, ok := config.(map[string]any)
		if !ok {
			raw = map[string]any{"value": config}
		}
		raw["type"] = name
		widget, itemWarnings := parseWidget(raw, "widgets.yaml", fmt.Sprintf("[%d].%s", index, name))
		warnings = append(warnings, itemWarnings...)
		widgets = append(widgets, widget)
	}
	return widgets, warnings, nil
}

func parseBookmarks(value any) ([]BookmarkGroup, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("bookmarks.yaml: top level must be a sequence")
	}
	groups := make([]BookmarkGroup, 0, len(items))
	for groupIndex, item := range items {
		name, value, err := namedEntry(item)
		if err != nil {
			return nil, fmt.Errorf("bookmarks.yaml[%d]: %w", groupIndex, err)
		}
		entries, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("bookmarks.yaml:%s: group contents must be a sequence", name)
		}
		group := BookmarkGroup{Name: name}
		for bookmarkIndex, entry := range entries {
			bookmarkName, bookmarkValue, entryErr := namedEntry(entry)
			if entryErr != nil {
				return nil, fmt.Errorf("bookmarks.yaml:%s[%d]: %w", name, bookmarkIndex, entryErr)
			}
			fields := collapseBookmarkFields(bookmarkValue)
			group.Bookmarks = append(group.Bookmarks, Bookmark{Name: bookmarkName, Href: scalarString(fields["href"]), Description: scalarString(fields["description"]), Icon: scalarString(fields["icon"]), Abbr: scalarString(fields["abbr"])})
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func collapseBookmarkFields(value any) map[string]any {
	if mapping, ok := value.(map[string]any); ok {
		return mapping
	}
	result := map[string]any{}
	if items, ok := value.([]any); ok {
		for _, item := range items {
			if mapping, itemOK := item.(map[string]any); itemOK {
				for key, field := range mapping {
					result[key] = field
				}
			}
		}
	}
	return result
}

func scalarString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, scalarString(item))
	}
	return out
}

// ParseDockerLabels parses the homepage.* labels from one container. The bool is false when the
// label map does not opt the container into Homepage.
func ParseDockerLabels(labels map[string]string) (DockerService, []Warning, bool) {
	name := labels["homepage.name"]
	if name == "" {
		return DockerService{}, nil, false
	}
	raw := map[string]any{}
	var warnings []Warning
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := labels[key]
		switch {
		case key == "homepage.name" || key == "homepage.group":
		case strings.HasPrefix(key, "homepage.widget."):
			widget, _ := raw["widget"].(map[string]any)
			if widget == nil {
				widget = map[string]any{}
				raw["widget"] = widget
			}
			setWidgetLabel(widget, strings.TrimPrefix(key, "homepage.widget."), value)
		case strings.HasPrefix(key, "homepage.widgets["):
			index, path, ok := indexedWidgetLabel(key)
			if !ok {
				warnings = append(warnings, Warning{Source: "docker labels", Path: key, Message: "malformed indexed widget label; ignored"})
				continue
			}
			widgets, _ := raw["widgets"].([]any)
			for len(widgets) <= index {
				widgets = append(widgets, map[string]any{})
			}
			widget, _ := widgets[index].(map[string]any)
			setWidgetLabel(widget, path, value)
			raw["widgets"] = widgets
		case strings.HasPrefix(key, "homepage."):
			field := strings.TrimPrefix(key, "homepage.")
			switch field {
			case "href", "description", "icon", "ping", "siteMonitor", "server", "container":
				raw[field] = value
			default:
				warnings = append(warnings, Warning{Source: "docker labels", Path: key, Message: "unsupported Homepage label; retained nowhere"})
			}
		}
	}
	service, serviceWarnings := parseService(name, raw, "docker labels", name)
	warnings = append(warnings, serviceWarnings...)
	return DockerService{Group: labels["homepage.group"], Service: service}, warnings, true
}

func indexedWidgetLabel(key string) (int, string, bool) {
	rest := strings.TrimPrefix(key, "homepage.widgets[")
	closeAt := strings.IndexByte(rest, ']')
	if closeAt <= 0 || len(rest) <= closeAt+2 || rest[closeAt+1] != '.' {
		return 0, "", false
	}
	index, err := strconv.Atoi(rest[:closeAt])
	if err != nil || index < 0 || index > 63 {
		return 0, "", false
	}
	return index, rest[closeAt+2:], true
}

func setWidgetLabel(target map[string]any, path, value string) {
	parts := strings.Split(path, ".")
	if len(parts) >= 2 && parts[0] == "headers" {
		headers, _ := target["headers"].(map[string]any)
		if headers == nil {
			headers = map[string]any{}
			target["headers"] = headers
		}
		headers[strings.Join(parts[1:], ".")] = value
		return
	}
	current := target
	for _, part := range parts[:len(parts)-1] {
		next, _ := current[part].(map[string]any)
		if next == nil {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}
