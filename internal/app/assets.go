// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"veduta.dev/veduta/internal/capabilities"
	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/manifestload"
)

// AssetAuthorized re-evaluates an already-signed asset reference against the current manifest,
// approval, card slot binding, and connection path policy. Revoking approval takes effect even
// while the token remains cryptographically valid.
func AssetAuthorized(_ context.Context, snap *config.Snapshot, configPath string, reg connections.Registry, p assettokens.Payload) (bool, error) {
	pluginID, version, ok := strings.Cut(p.Plugin, "@")
	if !ok {
		return false, nil
	}
	declared, ok := snap.IntegrationByID(pluginID)
	if !ok {
		return false, nil
	}
	src, err := integrations.ResolveSource(filepath.Dir(configPath), declared.Source)
	if err != nil || src.Builtin {
		return false, err
	}
	manifestPath, err := integrations.ManifestFile(src.Dir)
	if err != nil {
		return false, err
	}
	m, err := manifestload.Load(manifestPath)
	if err != nil {
		return false, err
	}
	if m.Version != version {
		return false, nil
	}
	lock, err := integrations.ReadLock(filepath.Join(filepath.Dir(configPath), integrations.LockFileName))
	if err != nil {
		return false, err
	}
	entry := lock.Integrations[pluginID]
	if entry == nil || entry.ManifestSHA256 != m.Digest {
		return false, nil
	}
	query, err := url.ParseQuery(p.Query)
	if err != nil {
		return false, err
	}
	for _, section := range snap.Config.Sections {
		for _, card := range section.Cards {
			if card.Integration != pluginID {
				continue
			}
			for slot, connectionID := range card.Slots {
				if connectionID != p.Connection {
					continue
				}
				g := grant(m, entry, card, reg, nil, 0, "", "")
				req := capabilities.HTTPRequest{Slot: slot, Method: "GET", Path: p.Path, Query: queryFirstValues(query)}
				if g.Authorize(req, capabilities.UseAsset) == nil {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func queryFirstValues(q url.Values) map[string]string {
	out := make(map[string]string, len(q))
	for key, values := range q {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}
