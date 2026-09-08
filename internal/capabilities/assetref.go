// SPDX-License-Identifier: AGPL-3.0-or-later

package capabilities

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/connections/routepath"
)

// defaultAssetRefTTL matches docs/01-architecture.md section 7's default token expiry.
const defaultAssetRefTTL = 24 * time.Hour

// transformPattern is docs/01-architecture.md section 7's own grammar for the "t" payload field.
var transformPattern = regexp.MustCompile(`^(w=(160|320|640|1280))?(,f=(webp|jpeg))?$`)

// AssetRef implements Broker. Preamble: capability, slot, route authorisation against UseAsset
// specifically (never UseData - see the type's own doc comment: without this split, a route
// granted for image thumbnails could mint refs to any path a data grant forbids), budget.
func (b *broker) AssetRef(ctx context.Context, g Grant, slot, path string, query url.Values, t Transform) (string, error) {
	var connID string
	if err := b.authorize(g, "AssetRef", func() error {
		if !g.Caps.Has("assets") {
			return ErrCapDenied
		}
		id, ok := g.Slots[slot]
		if !ok {
			return ErrSlotDenied
		}
		connID = id
		req := HTTPRequest{Slot: slot, Method: "GET", Path: path, Query: firstValues(query)}
		if err := g.Authorize(req, UseAsset); err != nil {
			return err
		}
		return g.consumeHostCall()
	}); err != nil {
		return "", err
	}

	canonicalPath, err := routepath.Canonicalise(path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRouteDenied, err)
	}
	if err := validateTransform(t); err != nil {
		return "", err
	}

	revision := g.Ident.SlotRevisions[slot]
	if revision == "" {
		revision = hex.EncodeToString(b.fallbackRevision[:])
	}
	payload := assettokens.Payload{
		V: 1, Connection: connID, ConnectionRevision: revision, Path: canonicalPath,
		Query: canonicalQuery(query), Transform: transformString(t),
		Plugin: g.PluginID + "@" + g.Version, Expires: time.Now().Add(defaultAssetRefTTL).Unix(),
	}
	return b.assets.Mint(payload)
}

func firstValues(q url.Values) map[string]string {
	out := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// canonicalQuery sorts keys and percent-normalises, so ?x=1&y=2 and ?y=2&x=1 sign identically -
// docs/01-architecture.md section 7: "Path and query are signed separately and canonically."
func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(q.Get(k)))
	}
	return b.String()
}

func transformString(t Transform) string {
	var b strings.Builder
	if t.Width > 0 {
		fmt.Fprintf(&b, "w=%d", t.Width)
	}
	if t.Format != "" {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "f=%s", t.Format)
	}
	return b.String()
}

func validateTransform(t Transform) error {
	if !transformPattern.MatchString(transformString(t)) {
		return fmt.Errorf("capabilities: transform %+v is not in the allowlist (widths 160/320/640/1280, formats webp/jpeg)", t)
	}
	return nil
}
