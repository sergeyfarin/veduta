// SPDX-License-Identifier: AGPL-3.0-or-later

// Package homepageimport parses Homepage YAML and Docker labels into a format-neutral import
// model. Mapping that model into Veduta configuration is deliberately a separate step.
package homepageimport

// Model is the complete Homepage input understood by the importer.
type Model struct {
	Groups    []Group
	Bookmarks []BookmarkGroup
	Settings  map[string]any
	Widgets   []Widget
}

// Group is a Homepage service group. Homepage permits groups nested inside groups.
type Group struct {
	Name     string
	Services []Service
	Groups   []Group
}

// Service is one Homepage service entry.
type Service struct {
	Name        string
	Href        string
	Description string
	Icon        string
	Ping        string
	SiteMonitor string
	Server      string
	Container   string
	Widgets     []Widget
	Extra       map[string]any
}

// Widget is either a service widget or one entry from widgets.yaml. Options retains fields the
// parser does not interpret so K2 can map a widget without losing source information.
type Widget struct {
	Type    string
	URL     string
	Key     string
	Fields  []string
	Options map[string]any
}

// BookmarkGroup holds the flat bookmark list under one Homepage bookmark heading.
type BookmarkGroup struct {
	Name      string
	Bookmarks []Bookmark
}

// Bookmark is one link from bookmarks.yaml.
type Bookmark struct {
	Name        string
	Href        string
	Description string
	Icon        string
	Abbr        string
}

// DockerService is the service and group recovered from one container's Homepage labels.
type DockerService struct {
	Group   string
	Service Service
}

// Warning describes source data that was retained or skipped but does not stop an import.
type Warning struct {
	Source  string
	Path    string
	Message string
}
